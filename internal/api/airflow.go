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
	DAGs []airflowDAG `json:"dags"`
}

type airflowDAG struct {
	DagID            string          `json:"dag_id"`
	IsPaused         bool            `json:"is_paused"`
	ScheduleInterval json.RawMessage `json:"schedule_interval"`
	NextDagRun       *string         `json:"next_dagrun"`
}

type airflowDAGRunsResponse struct {
	DAGRuns []airflowDAGRun `json:"dag_runs"`
}

type airflowDAGRun struct {
	DagID     string  `json:"dag_id"`
	DagRunID  string  `json:"dag_run_id"`
	State     string  `json:"state"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

type airflowTaskInstancesResponse struct {
	TaskInstances []airflowTaskInstance `json:"task_instances"`
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
}

// NewAirflowClient creates a new API client for the given Composer webserver URL.
func NewAirflowClient(webserverURL string) *AirflowClient {
	return &AirflowClient{
		baseURL:    webserverURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		getToken:   GetAccessToken,
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
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *AirflowClient) ListDAGs() ([]model.DAG, error) {
	data, err := c.doRequest("GET", "/api/v1/dags?limit=100", nil)
	if err != nil {
		return nil, fmt.Errorf("listing DAGs: %w", err)
	}

	var resp airflowDAGsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing DAGs response: %w", err)
	}

	dags := make([]model.DAG, len(resp.DAGs))
	for i, d := range resp.DAGs {
		dags[i] = model.DAG{
			ID:               d.DagID,
			IsPaused:         d.IsPaused,
			ScheduleInterval: parseScheduleInterval(d.ScheduleInterval),
			NextDagRun:       parseTime(d.NextDagRun),
		}
	}
	// The /dags endpoint does not include last-run info, so enrich each DAG
	// with its latest run's state/date. This is an N+1, but ListDAGs is only
	// ever called from a background tea.Cmd, so it never blocks the UI loop.
	c.enrichLastRun(dags)
	return dags, nil
}

// latestRunFetchWorkers bounds the concurrency of per-DAG last-run lookups
// in enrichLastRun so we don't open one connection per DAG at once.
const latestRunFetchWorkers = 8

// enrichLastRun populates LastRunState/LastRunDate for each DAG by fetching its
// most recent DAG run. Runs concurrently with a bounded worker pool; each
// goroutine writes only to its own index, so no locking is required.
func (c *AirflowClient) enrichLastRun(dags []model.DAG) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, latestRunFetchWorkers)
	for i := range dags {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			state, date := c.latestRun(dags[i].ID)
			dags[i].LastRunState = state
			dags[i].LastRunDate = date
		}(i)
	}
	wg.Wait()
}

// latestRun returns the state and start date of the most recent run for a DAG.
// Returns ("", zero time) if the DAG has never run or the request fails.
func (c *AirflowClient) latestRun(dagID string) (string, time.Time) {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns?order_by=-start_date&limit=1", url.PathEscape(dagID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return "", time.Time{}
	}
	var resp airflowDAGRunsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", time.Time{}
	}
	if len(resp.DAGRuns) == 0 {
		return "", time.Time{}
	}
	r := resp.DAGRuns[0]
	return r.State, parseTime(r.StartDate)
}

func (c *AirflowClient) ListDAGRuns(dagID string) []model.DAGRun {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns?order_by=start_date&limit=25", url.PathEscape(dagID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil
	}

	var resp airflowDAGRunsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}

	runs := make([]model.DAGRun, len(resp.DAGRuns))
	for i, r := range resp.DAGRuns {
		runs[i] = model.DAGRun{
			DagID:     r.DagID,
			RunID:     r.DagRunID,
			State:     r.State,
			StartDate: parseTime(r.StartDate),
			EndDate:   parseTime(r.EndDate),
		}
	}
	return runs
}

func (c *AirflowClient) ListTaskInstances(dagID, runID string) []model.TaskInstance {
	path := fmt.Sprintf("/api/v1/dags/%s/dagRuns/%s/taskInstances",
		url.PathEscape(dagID), url.PathEscape(runID))
	data, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil
	}

	var resp airflowTaskInstancesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}

	tasks := make([]model.TaskInstance, len(resp.TaskInstances))
	for i, t := range resp.TaskInstances {
		tasks[i] = model.TaskInstance{
			TaskID:    t.TaskID,
			DagID:     t.DagID,
			RunID:     t.DagRunID,
			State:     mapTaskState(t.State),
			Duration:  time.Duration(t.Duration * float64(time.Second)),
			StartDate: parseTime(t.StartDate),
			TryNumber: t.TryNumber,
		}
	}
	return tasks
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

	return model.DAGRun{
		DagID:     r.DagID,
		RunID:     r.DagRunID,
		State:     r.State,
		StartDate: parseTime(r.StartDate),
		EndDate:   parseTime(r.EndDate),
	}, nil
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
	DagRunID          string   `json:"dag_run_id,omitempty"`
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

func (c *AirflowClient) ClearTaskInstances(dagID, runID string, taskIDs []string) error {
	path := fmt.Sprintf("/api/v1/dags/%s/clearTaskInstances", url.PathEscape(dagID))
	reqBody := airflowClearRequest{
		DryRun:       false,
		TaskIDs:      taskIDs,
		OnlyFailed:   false,
		ResetDagRuns: true,
		DagRunID:     runID,
	}
	body, _ := json.Marshal(reqBody)
	_, err := c.doRequest("POST", path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("clear task instances: %w", err)
	}
	return nil
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
				}
				return typStr
			}
		}
	}
	return string(raw)
}

func jsonInt(raw json.RawMessage) int {
	var v int
	// Missing or non-numeric fields fall back to zero.
	_ = json.Unmarshal(raw, &v)
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
	Key   string `json:"key"`
	Value string `json:"value"`
}

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
		entries[i] = model.XComEntry{Key: e.Key, Value: e.Value}
	}
	return entries
}

// --- DAG Detail (task graph) ---

type airflowDAGDetailResponse struct {
	DagID     string              `json:"dag_id"`
	FileToken string              `json:"file_token"`
	FileLoc   string              `json:"fileloc"`
	Tasks     []airflowTaskDetail `json:"tasks"`
}

type airflowTaskDetail struct {
	TaskID          string   `json:"task_id"`
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
		return "running"
	case "queued", "scheduled", "deferred":
		return "running"
	case "removed", "restarting":
		return state
	default:
		return state
	}
}
