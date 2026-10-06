package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shiroimon/gomposer/internal/model"
)

// Airflow REST API JSON response structures.

type airflowDAGsResponse struct {
	DAGs         []airflowDAG `json:"dags"`
	TotalEntries int          `json:"total_entries"`
}

type airflowDAG struct {
	DagID            string          `json:"dag_id"`
	IsPaused         bool            `json:"is_paused"`
	ScheduleInterval json.RawMessage `json:"schedule_interval"`
	NextDagRun       *string         `json:"next_dagrun"`
}

type airflowDAGRunsResponse struct {
	DAGRuns      []airflowDAGRun `json:"dag_runs"`
	TotalEntries int             `json:"total_entries"`
}

type airflowDAGRun struct {
	DagID       string          `json:"dag_id"`
	DagRunID    string          `json:"dag_run_id"`
	State       string          `json:"state"`
	StartDate   *string         `json:"start_date"`
	EndDate     *string         `json:"end_date"`
	LogicalDate *string         `json:"logical_date"`
	RunType     string          `json:"run_type"`
	Conf        json.RawMessage `json:"conf"`
	Note        *string         `json:"note"`
}

func (r airflowDAGRun) toModel() model.DAGRun {
	run := model.DAGRun{
		DagID:       r.DagID,
		RunID:       r.DagRunID,
		State:       r.State,
		StartDate:   parseTime(r.StartDate),
		EndDate:     parseTime(r.EndDate),
		LogicalDate: parseTime(r.LogicalDate),
		RunType:     r.RunType,
	}
	if c := strings.TrimSpace(string(r.Conf)); c != "" && c != "null" && c != "{}" {
		run.Conf = c
	}
	if r.Note != nil {
		run.Note = *r.Note
	}
	return run
}

type airflowTaskInstancesResponse struct {
	TaskInstances []airflowTaskInstance `json:"task_instances"`
	TotalEntries  int                   `json:"total_entries"`
}

type airflowTaskInstance struct {
	TaskID    string  `json:"task_id"`
	DagID     string  `json:"dag_id"`
	DagRunID  string  `json:"dag_run_id"`
	State     string  `json:"state"`
	Duration  float64 `json:"duration"`
	StartDate *string `json:"start_date"`
	TryNumber int     `json:"try_number"`
}

type airflowPatchDAG struct {
	IsPaused bool `json:"is_paused"`
}

type airflowTriggerRequest struct {
	Conf map[string]interface{} `json:"conf,omitempty"`
}

// AirflowClient implements DataSource using the Airflow REST API.
type AirflowClient struct {
	baseURL    string
	httpClient *http.Client
	getToken   func() (string, error)
	dropToken  func() // discards a cached token after a 401

	lastRunMu      sync.Mutex
	lastRuns       map[string]airflowDAGRun // dagID → newest run seen; empty run when the DAG has none
	lastRunsPolled time.Time
}

// NewAirflowClient creates a new API client for the given Composer webserver URL.
func NewAirflowClient(webserverURL string) *AirflowClient {
	return &AirflowClient{
		baseURL:    webserverURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		getToken:   sharedToken.Get,
		dropToken:  sharedToken.Invalidate,
	}
}

func (c *AirflowClient) doRequest(method, path string, body io.Reader) ([]byte, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, err
	}

	u := strings.TrimRight(c.baseURL, "/") + "/" + strings.TrimLeft(path, "/")

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized && c.dropToken != nil {
		c.dropToken()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *AirflowClient) ListDAGs() ([]model.DAG, error) {
	var dags []model.DAG
	err := c.eachPage("/api/v1/dags", nil, func(data []byte) (int, int, error) {
		var resp airflowDAGsResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return 0, 0, fmt.Errorf("parsing DAGs response: %w", err)
		}
		for _, d := range resp.DAGs {
			dags = append(dags, model.DAG{
				ID:               d.DagID,
				IsPaused:         d.IsPaused,
				ScheduleInterval: parseScheduleInterval(d.ScheduleInterval),
				NextDagRun:       parseTime(d.NextDagRun),
			})
		}
		return len(resp.DAGs), resp.TotalEntries, nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing DAGs: %w", err)
	}
	c.fillLastRuns(dags)
	return dags, nil
}

// pageSize is the largest page the Airflow API returns by default (maximum_page_limit = 100).
const pageSize = 100

// eachPage walks a list endpoint with limit/offset until total_entries is reached.
// decode returns the number of items on the page and the reported total.
func (c *AirflowClient) eachPage(path string, query url.Values, decode func([]byte) (n, total int, err error)) error {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("limit", fmt.Sprint(pageSize))
	for offset := 0; ; {
		q.Set("offset", fmt.Sprint(offset))
		data, err := c.doRequest("GET", path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		n, total, err := decode(data)
		if err != nil {
			return err
		}
		offset += n
		if n == 0 || offset >= total {
			return nil
		}
	}
}

// lastRunConcurrency bounds the per-DAG lookups so a refresh does not burst the webserver.
const lastRunConcurrency = 8

// lastRunWindow is how far back the first cross-DAG scan looks; DAGs not seen in it are looked up one by one.
const lastRunWindow = 7 * 24 * time.Hour

// lastRunOverlap re-reads a little before the previous poll so a run updated during that request is not missed.
const lastRunOverlap = 2 * time.Minute

// fillLastRuns sets each DAG's latest run. The /dags endpoint does not return it.
// One request per DAG is slow with many DAGs, so after the first load only runs updated since
// the previous poll are read across all DAGs (dag_id "~") and merged into a per-client cache.
// A DAG whose latest run changes always has that run's updated_at bumped, so the cache stays current.
func (c *AirflowClient) fillLastRuns(dags []model.DAG) {
	c.lastRunMu.Lock()
	defer c.lastRunMu.Unlock()
	if c.lastRuns == nil {
		c.lastRuns = map[string]airflowDAGRun{}
	}

	polledAt := time.Now()
	since := c.lastRunsPolled.Add(-lastRunOverlap)
	if c.lastRunsPolled.IsZero() {
		since = polledAt.Add(-lastRunWindow)
	}
	recent, err := c.listRunsUpdatedSince(since)
	if err != nil {
		// The cross-DAG query may be unsupported; fall back to asking every DAG, without trusting the cache.
		c.lastRuns = map[string]airflowDAGRun{}
		c.lastRunsPolled = time.Time{}
	} else {
		for _, r := range recent {
			if prev, ok := c.lastRuns[r.DagID]; !ok || !parseTime(r.LogicalDate).Before(parseTime(prev.LogicalDate)) {
				c.lastRuns[r.DagID] = r
			}
		}
		c.lastRunsPolled = polledAt
	}

	var missing []string
	for _, d := range dags {
		if _, ok := c.lastRuns[d.ID]; !ok {
			missing = append(missing, d.ID)
		}
	}
	for id, r := range c.fetchLatestRuns(missing) {
		c.lastRuns[id] = r
	}

	for i := range dags {
		r := c.lastRuns[dags[i].ID]
		dags[i].LastRunState = r.State
		dags[i].LastRunDate = parseTime(r.StartDate)
	}
}

func (c *AirflowClient) listRunsUpdatedSince(since time.Time) ([]airflowDAGRun, error) {
	var runs []airflowDAGRun
	q := url.Values{"updated_at_gte": {since.UTC().Format(time.RFC3339)}, "order_by": {"-execution_date"}}
	err := c.eachPage("/api/v1/dags/~/dagRuns", q, func(data []byte) (int, int, error) {
		var resp airflowDAGRunsResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return 0, 0, err
		}
		runs = append(runs, resp.DAGRuns...)
		return len(resp.DAGRuns), resp.TotalEntries, nil
	})
	return runs, err
}

// fetchLatestRuns asks each DAG for its newest run. A DAG with no runs maps to an empty run so it is not asked again;
// a failed lookup is left out so the next refresh retries it.
func (c *AirflowClient) fetchLatestRuns(dagIDs []string) map[string]airflowDAGRun {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, lastRunConcurrency)
		out = map[string]airflowDAGRun{}
	)
	for _, id := range dagIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			path := fmt.Sprintf("/api/v1/dags/%s/dagRuns?order_by=-execution_date&limit=1", url.PathEscape(id))
			data, err := c.doRequest("GET", path, nil)
			if err != nil {
				return
			}
			var resp airflowDAGRunsResponse
			if json.Unmarshal(data, &resp) != nil {
				return
			}
			r := airflowDAGRun{DagID: id}
			if len(resp.DAGRuns) > 0 {
				r = resp.DAGRuns[0]
			}
			mu.Lock()
			out[id] = r
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return out
}

// dagRunLimit caps the Runs tab; older runs are counted in the total but not fetched.
const dagRunLimit = 25

func (c *AirflowClient) ListDAGRuns(dagID string) ([]model.DAGRun, int) {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns?order_by=-execution_date&limit=%d", url.PathEscape(dagID), dagRunLimit)
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, 0
	}

	var resp airflowDAGRunsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, 0
	}

	runs := make([]model.DAGRun, len(resp.DAGRuns))
	for i, r := range resp.DAGRuns {
		runs[i] = r.toModel()
	}
	return runs, resp.TotalEntries
}

func (c *AirflowClient) ListTaskInstances(dagID, runID string) []model.TaskInstance {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s/taskInstances",
		url.PathEscape(dagID), url.PathEscape(runID))
	tasks, err := c.listTaskInstances(path, nil)
	if err != nil {
		return nil
	}
	return tasks
}

func (c *AirflowClient) listTaskInstances(path string, query url.Values) ([]model.TaskInstance, error) {
	var tasks []model.TaskInstance
	err := c.eachPage(path, query, func(data []byte) (int, int, error) {
		var resp airflowTaskInstancesResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return 0, 0, err
		}
		for _, t := range resp.TaskInstances {
			tasks = append(tasks, model.TaskInstance{
				TaskID:    t.TaskID,
				DagID:     t.DagID,
				RunID:     t.DagRunID,
				State:     mapTaskState(t.State),
				Duration:  time.Duration(t.Duration * float64(time.Second)),
				StartDate: parseTime(t.StartDate),
				TryNumber: t.TryNumber,
			})
		}
		return len(resp.TaskInstances), resp.TotalEntries, nil
	})
	return tasks, err
}

func (c *AirflowClient) ListProblemTaskInstances(since time.Time) ([]model.TaskInstance, error) {
	q := url.Values{"state": model.ProblemTaskStates, "start_date_gte": {since.UTC().Format(time.RFC3339)}}
	tasks, err := c.listTaskInstances("/api/v1/dags/~/dagRuns/~/taskInstances", q)
	if err != nil {
		return nil, fmt.Errorf("listing problem tasks: %w", err)
	}
	return tasks, nil
}

func (c *AirflowClient) GetTaskLog(dagID, runID, taskID string, tryNumber int) string {
	if tryNumber < 1 {
		tryNumber = 1
	}
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s/taskInstances/%s/logs/%d",
		url.PathEscape(dagID), url.PathEscape(runID), url.PathEscape(taskID), tryNumber)
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return fmt.Sprintf("(error fetching log: %v)", err)
	}
	return string(data)
}

func (c *AirflowClient) TriggerDAG(dagID string, conf map[string]interface{}) (model.DAGRun, error) {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns", url.PathEscape(dagID))
	body, _ := json.Marshal(airflowTriggerRequest{Conf: conf})

	data, err := c.doRequest("POST", path, bytes.NewReader(body))
	if err != nil {
		return model.DAGRun{}, fmt.Errorf("trigger failed: %w", err)
	}

	var r airflowDAGRun
	if err := json.Unmarshal(data, &r); err != nil {
		return model.DAGRun{}, fmt.Errorf("parsing trigger response: %w", err)
	}

	return r.toModel(), nil
}

func (c *AirflowClient) TogglePause(dagID string) (bool, error) {
	// First get current state
	path := fmt.Sprintf("/api/v1/dags/%s", url.PathEscape(dagID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return false, fmt.Errorf("getting DAG state: %w", err)
	}

	var current airflowDAG
	if err := json.Unmarshal(data, &current); err != nil {
		return false, fmt.Errorf("parsing DAG: %w", err)
	}

	// Toggle
	newState := !current.IsPaused
	patchBody, _ := json.Marshal(airflowPatchDAG{IsPaused: newState})

	_, err = c.doRequest("PATCH", path, bytes.NewReader(patchBody))
	if err != nil {
		return false, fmt.Errorf("updating DAG: %w", err)
	}

	return newState, nil
}

type airflowClearRequest struct {
	DryRun            bool     `json:"dry_run"`
	TaskIDs           []string `json:"task_ids,omitempty"`
	OnlyFailed        bool     `json:"only_failed"`
	ResetDagRuns      bool     `json:"reset_dag_runs"`
	IncludeSubdags    bool     `json:"include_subdags"`
	IncludeParentdag  bool     `json:"include_parentdag"`
	IncludeDownstream bool     `json:"include_downstream"`
	IncludeUpstream   bool     `json:"include_upstream"`
	DagRunID          string   `json:"dag_run_id,omitempty"`
}

type airflowTaskInstanceReferences struct {
	TaskInstances []struct {
		TaskID string `json:"task_id"`
	} `json:"task_instances"`
}

type airflowPatchTaskInstance struct {
	DryRun   bool   `json:"dry_run"`
	NewState string `json:"new_state"`
}

type airflowPatchDAGRun struct {
	State string `json:"state"`
}

func (c *AirflowClient) SetDAGRunState(dagID, runID, state string) error {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s",
		url.PathEscape(dagID), url.PathEscape(runID))
	body, _ := json.Marshal(airflowPatchDAGRun{State: state})
	_, err := c.doRequest("PATCH", path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set DAG run state: %w", err)
	}
	return nil
}

func (c *AirflowClient) SetTaskInstanceState(dagID, runID, taskID, state string) error {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s/taskInstances/%s",
		url.PathEscape(dagID), url.PathEscape(runID), url.PathEscape(taskID))
	body, _ := json.Marshal(airflowPatchTaskInstance{NewState: state})
	if _, err := c.doRequest("PATCH", path, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("set task instance state: %w", err)
	}
	return nil
}

func (c *AirflowClient) ClearTaskInstances(dagID, runID string, taskIDs []string, opts model.ClearOptions) ([]string, error) {
	path := fmt.Sprintf("/api/v1/dags/%s/clearTaskInstances", url.PathEscape(dagID))
	reqBody := airflowClearRequest{
		DryRun:     opts.DryRun,
		TaskIDs:    taskIDs,
		OnlyFailed: opts.OnlyFailed,
		// A run left in "failed" is not picked up again by the scheduler, so clearing always requeues the run.
		ResetDagRuns:      true,
		IncludeDownstream: opts.IncludeDownstream,
		IncludeUpstream:   opts.IncludeUpstream,
		DagRunID:          runID,
	}
	body, _ := json.Marshal(reqBody)
	data, err := c.doRequest("POST", path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("clear task instances: %w", err)
	}
	var resp airflowTaskInstanceReferences
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing clear response: %w", err)
	}
	ids := make([]string, len(resp.TaskInstances))
	for i, t := range resp.TaskInstances {
		ids[i] = t.TaskID
	}
	return ids, nil
}

func parseTime(s *string) time.Time {
	if s == nil || *s == "" {
		return time.Time{}
	}
	// Airflow returns ISO 8601 format
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, *s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parseScheduleInterval extracts a display string from the schedule_interval
// field, which may be a JSON string (e.g. "@daily") or an object
// (e.g. {"__type": "timedelta", "days": 1, ...}).
func parseScheduleInterval(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// Try as a plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try as an object with a __type field.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		if typ, ok := obj["__type"]; ok {
			var typStr string
			if json.Unmarshal(typ, &typStr) == nil {
				switch typStr {
				case "timedelta":
					return formatTimedelta(obj)
				case "relativedelta":
					return formatRelativedelta(obj)
				case "CronExpression":
					var cron string
					if json.Unmarshal(obj["value"], &cron) == nil && cron != "" {
						return cron
					}
				}
				return typStr
			}
		}
	}
	return string(raw)
}

func jsonInt(raw json.RawMessage) int {
	var v int
	_ = json.Unmarshal(raw, &v) // missing or non-numeric fields fall back to 0
	return v
}

func formatTimedelta(obj map[string]json.RawMessage) string {
	days := jsonInt(obj["days"])
	seconds := jsonInt(obj["seconds"])
	if days == 1 && seconds == 0 {
		return "@daily"
	}
	if days == 7 && seconds == 0 {
		return "@weekly"
	}
	hours := seconds / 3600
	mins := (seconds % 3600) / 60
	parts := ""
	if days > 0 {
		parts += fmt.Sprintf("%dd", days)
	}
	if hours > 0 {
		parts += fmt.Sprintf("%dh", hours)
	}
	if mins > 0 {
		parts += fmt.Sprintf("%dm", mins)
	}
	if parts == "" {
		parts = fmt.Sprintf("%ds", seconds)
	}
	return parts
}

func formatRelativedelta(obj map[string]json.RawMessage) string {
	months := jsonInt(obj["months"])
	days := jsonInt(obj["days"])
	if months == 1 && days == 0 {
		return "@monthly"
	}
	if months == 12 && days == 0 {
		return "@yearly"
	}
	parts := ""
	if months > 0 {
		parts += fmt.Sprintf("%dmo", months)
	}
	if days > 0 {
		parts += fmt.Sprintf("%dd", days)
	}
	if parts == "" {
		return "relativedelta"
	}
	return parts
}

// --- XCom ---

type airflowXComResponse struct {
	XComEntries []airflowXComEntry `json:"xcom_entries"`
}

type airflowXComEntry struct {
	Key      string `json:"key"`
	MapIndex int    `json:"map_index"`
	Value    string `json:"value"`
}

// The list endpoint omits "value", so each entry is fetched on its own.
// Non-JSON values come back in serialized form, e.g. "datetime.date@version=2(2026-09-09)".
func (c *AirflowClient) GetXComEntries(dagID, runID, taskID string) []model.XComEntry {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s/taskInstances/%s/xcomEntries",
		url.PathEscape(dagID), url.PathEscape(runID), url.PathEscape(taskID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil
	}
	var resp airflowXComResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}
	entries := make([]model.XComEntry, len(resp.XComEntries))
	for i, e := range resp.XComEntries {
		entries[i] = model.XComEntry{Key: e.Key, Value: c.getXComValue(path, e)}
	}
	return entries
}

func (c *AirflowClient) getXComValue(listPath string, e airflowXComEntry) string {
	path := fmt.Sprintf("%s/%s?map_index=%d", listPath, url.PathEscape(e.Key), e.MapIndex)
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return "(error: " + err.Error() + ")"
	}
	var full airflowXComEntry
	if err := json.Unmarshal(data, &full); err != nil {
		return "(error: " + err.Error() + ")"
	}
	return full.Value
}

// --- DAG Detail (task graph) ---

type airflowDAGDetailResponse struct {
	DagID     string              `json:"dag_id"`
	FileToken string              `json:"file_token"`
	FileLoc   string              `json:"fileloc"`
	Tasks     []airflowTaskDetail `json:"tasks"`
}

type airflowTaskDetail struct {
	TaskID            string   `json:"task_id"`
	DownstreamTaskIDs []string `json:"downstream_task_ids"`
}

func (c *AirflowClient) GetDAGDetail(dagID string) (model.DAGDetail, error) {
	path := fmt.Sprintf("/api/v1/dags/%s/details", url.PathEscape(dagID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return model.DAGDetail{}, fmt.Errorf("get DAG detail: %w", err)
	}
	var resp airflowDAGDetailResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return model.DAGDetail{}, fmt.Errorf("parsing DAG detail: %w", err)
	}
	tasks := make([]model.TaskDep, len(resp.Tasks))
	for i, t := range resp.Tasks {
		tasks[i] = model.TaskDep{TaskID: t.TaskID, DownstreamIDs: t.DownstreamTaskIDs}
	}
	return model.DAGDetail{
		DagID:     resp.DagID,
		FileToken: resp.FileToken,
		FileLoc:   resp.FileLoc,
		Tasks:     tasks,
	}, nil
}

// --- DAG Source ---

func (c *AirflowClient) GetDAGSource(fileToken string) (string, error) {
	path := fmt.Sprintf("/api/v1/dagSources/%s", url.PathEscape(fileToken))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("get DAG source: %w", err)
	}
	return string(data), nil
}

// --- Import Errors ---

type airflowImportErrorsResponse struct {
	ImportErrors []airflowImportError `json:"import_errors"`
}

type airflowImportError struct {
	Filename   string  `json:"filename"`
	StackTrace string  `json:"stack_trace"`
	Timestamp  *string `json:"timestamp"`
}

func (c *AirflowClient) ListImportErrors() []model.ImportError {
	data, err := c.doRequest("GET", "/api/v1/importErrors", nil)
	if err != nil {
		return nil
	}
	var resp airflowImportErrorsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}
	errors := make([]model.ImportError, len(resp.ImportErrors))
	for i, e := range resp.ImportErrors {
		errors[i] = model.ImportError{
			Filename:   e.Filename,
			StackTrace: e.StackTrace,
			Timestamp:  parseTime(e.Timestamp),
		}
	}
	return errors
}

func mapTaskState(state string) string {
	// Airflow uses various state names; normalize common ones
	switch state {
	case "success", "failed", "running", "skipped", "upstream_failed":
		return state
	case "up_for_retry":
		return state
	case "queued", "scheduled", "deferred":
		return "running"
	case "removed", "restarting":
		return state
	default:
		return state
	}
}
