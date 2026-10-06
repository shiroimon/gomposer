package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shiroimon/gomposer/internal/model"
)

func TestParseDAGsResponse(t *testing.T) {
	body := `{
		"dags": [
			{"dag_id": "etl_pipeline", "is_paused": false},
			{"dag_id": "daily_report", "is_paused": true}
		],
		"total_entries": 2
	}`

	var resp airflowDAGsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(resp.DAGs) != 2 {
		t.Fatalf("expected 2 DAGs, got %d", len(resp.DAGs))
	}
	if resp.DAGs[0].DagID != "etl_pipeline" {
		t.Errorf("expected 'etl_pipeline', got %q", resp.DAGs[0].DagID)
	}
	if resp.DAGs[1].IsPaused != true {
		t.Error("expected daily_report to be paused")
	}
}

func TestParseDAGRunsResponse(t *testing.T) {
	body := `{
		"dag_runs": [
			{
				"dag_id": "etl_pipeline",
				"dag_run_id": "manual__2024-01-01",
				"state": "success",
				"start_date": "2024-01-01T10:00:00+00:00",
				"end_date": "2024-01-01T10:15:00+00:00"
			},
			{
				"dag_id": "etl_pipeline",
				"dag_run_id": "manual__2024-01-02",
				"state": "running",
				"start_date": "2024-01-02T10:00:00+00:00",
				"end_date": null
			}
		]
	}`

	var resp airflowDAGRunsResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(resp.DAGRuns) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(resp.DAGRuns))
	}
	if resp.DAGRuns[0].State != "success" {
		t.Errorf("expected 'success', got %q", resp.DAGRuns[0].State)
	}
	if resp.DAGRuns[1].EndDate != nil {
		t.Error("expected nil end_date for running run")
	}
}

func TestParseTaskInstancesResponse(t *testing.T) {
	body := `{
		"task_instances": [
			{
				"task_id": "extract",
				"dag_id": "etl_pipeline",
				"dag_run_id": "manual__2024-01-01",
				"state": "success",
				"duration": 180.5,
				"start_date": "2024-01-01T10:00:00+00:00",
				"try_number": 1
			},
			{
				"task_id": "load",
				"dag_id": "etl_pipeline",
				"dag_run_id": "manual__2024-01-01",
				"state": "up_for_retry",
				"duration": 60.0,
				"start_date": "2024-01-01T10:03:00+00:00",
				"try_number": 2
			}
		]
	}`

	var resp airflowTaskInstancesResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(resp.TaskInstances) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(resp.TaskInstances))
	}
	if resp.TaskInstances[0].Duration != 180.5 {
		t.Errorf("expected duration 180.5, got %f", resp.TaskInstances[0].Duration)
	}
	if resp.TaskInstances[1].TryNumber != 2 {
		t.Errorf("expected try_number 2, got %d", resp.TaskInstances[1].TryNumber)
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		input  *string
		isZero bool
	}{
		{nil, true},
		{strPtr(""), true},
		{strPtr("2024-01-01T10:00:00+00:00"), false},
		{strPtr("2024-01-01T10:00:00.123456+00:00"), false},
		{strPtr("2024-01-01T10:00:00"), false},
		{strPtr("invalid"), true},
	}

	for _, tt := range tests {
		result := parseTime(tt.input)
		if result.IsZero() != tt.isZero {
			label := "<nil>"
			if tt.input != nil {
				label = *tt.input
			}
			t.Errorf("parseTime(%q): isZero=%v, want %v", label, result.IsZero(), tt.isZero)
		}
	}
}

func TestMapTaskState(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"success", "success"},
		{"failed", "failed"},
		{"running", "running"},
		{"skipped", "skipped"},
		{"upstream_failed", "upstream_failed"},
		{"up_for_retry", "up_for_retry"},
		{"queued", "running"},
		{"scheduled", "running"},
		{"deferred", "running"},
	}

	for _, tt := range tests {
		got := mapTaskState(tt.input)
		if got != tt.want {
			t.Errorf("mapTaskState(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAirflowClient_ImplementsInterface(t *testing.T) {
	var _ DataSource = (*AirflowClient)(nil)
}

func TestAirflowClient_ListDAGRuns_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"dag_runs": [
				{
					"dag_id": "test_dag",
					"dag_run_id": "run_1",
					"state": "success",
					"start_date": "2024-01-01T10:00:00+00:00",
					"end_date": "2024-01-01T10:15:00+00:00"
				}
			]
		}`)
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	runs, _ := client.ListDAGRuns("test_dag")
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].RunID != "run_1" {
		t.Errorf("expected run_1, got %q", runs[0].RunID)
	}
	if runs[0].State != "success" {
		t.Errorf("expected success, got %q", runs[0].State)
	}
}

func TestAirflowClient_ListDAGs_EnrichesLastRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/dags":
			_, _ = fmt.Fprint(w, `{"dags": [
				{"dag_id": "dag_ok", "is_paused": false},
				{"dag_id": "dag_bad", "is_paused": false}
			]}`)
		case "/api/v1/dags/~/dagRuns":
			// No recent runs across DAGs, so each DAG is looked up on its own.
			_, _ = fmt.Fprint(w, `{"dag_runs": [], "total_entries": 0}`)
		case "/api/v1/dags/dag_ok/dagRuns":
			// order_by=-execution_date&limit=1 → latest run first
			_, _ = fmt.Fprint(w, `{"dag_runs": [
				{"dag_id": "dag_ok", "dag_run_id": "r1", "state": "success", "start_date": "2024-01-02T10:00:00+00:00"}
			]}`)
		case "/api/v1/dags/dag_bad/dagRuns":
			_, _ = fmt.Fprint(w, `{"dag_runs": [
				{"dag_id": "dag_bad", "dag_run_id": "r2", "state": "failed", "start_date": "2024-01-03T10:00:00+00:00"}
			]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	dags, err := client.ListDAGs()
	if err != nil {
		t.Fatalf("ListDAGs failed: %v", err)
	}
	if len(dags) != 2 {
		t.Fatalf("expected 2 DAGs, got %d", len(dags))
	}
	byID := map[string]string{}
	for _, d := range dags {
		byID[d.ID] = d.LastRunState
	}
	if byID["dag_ok"] != "success" {
		t.Errorf("dag_ok LastRunState: expected success, got %q", byID["dag_ok"])
	}
	if byID["dag_bad"] != "failed" {
		t.Errorf("dag_bad LastRunState: expected failed, got %q", byID["dag_bad"])
	}
}

func TestAirflowClient_TriggerDAG_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"dag_id": "test_dag",
			"dag_run_id": "manual__2024-01-01T12:00:00",
			"state": "queued",
			"start_date": null,
			"end_date": null
		}`)
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	run, err := client.TriggerDAG("test_dag", nil)
	if err != nil {
		t.Fatalf("trigger failed: %v", err)
	}
	if run.DagID != "test_dag" {
		t.Errorf("expected test_dag, got %q", run.DagID)
	}
}

func TestAirflowClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"detail":"Not authorized"}`)
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	runs, _ := client.ListDAGRuns("test_dag")
	if len(runs) != 0 {
		t.Errorf("expected empty result on error, got %d", len(runs))
	}
}

func strPtr(s string) *string { return &s }

func TestParseScheduleInterval_CronExpression(t *testing.T) {
	got := parseScheduleInterval(json.RawMessage(`{"__type": "CronExpression", "value": "12 4 * * *"}`))
	if got != "12 4 * * *" {
		t.Errorf("got %q, want the cron string", got)
	}
}

func TestAirflowClient_ListDAGs_FillsLastRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/dags":
			_, _ = fmt.Fprint(w, `{"dags": [{"dag_id": "ran"}, {"dag_id": "never"}]}`)
		case "/api/v1/dags/ran/dagRuns":
			if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("order_by") != "-execution_date" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, `{"dag_runs": [{"dag_id": "ran", "dag_run_id": "r", "state": "failed", "start_date": "2026-10-01T06:12:00+00:00"}]}`)
		default:
			_, _ = fmt.Fprint(w, `{"dag_runs": []}`)
		}
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	dags, err := client.ListDAGs()
	if err != nil {
		t.Fatal(err)
	}
	if dags[0].LastRunState != "failed" || dags[0].LastRunDate.IsZero() {
		t.Errorf("last run not filled: %+v", dags[0])
	}
	if dags[1].LastRunState != "" || !dags[1].LastRunDate.IsZero() {
		t.Errorf("DAG without runs should stay empty: %+v", dags[1])
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *AirflowClient {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }
	return client
}

func TestAirflowClient_ListTaskInstances_FollowsPages(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		var items []string
		start := 0
		if offset == "100" {
			start = 100
		}
		n := 100
		if start == 100 {
			n = 30
		}
		for i := start; i < start+n; i++ {
			items = append(items, fmt.Sprintf(`{"task_id":"t%d","state":"success"}`, i))
		}
		_, _ = fmt.Fprintf(w, `{"task_instances":[%s],"total_entries":130}`, strings.Join(items, ","))
	})
	tasks := client.ListTaskInstances("d", "r")
	if len(tasks) != 130 {
		t.Fatalf("got %d tasks, want all 130 across 2 pages", len(tasks))
	}
}

func TestAirflowClient_ListDAGs_IncrementalLastRun(t *testing.T) {
	var perDAG, crossDAG int
	var lastSince string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/dags":
			_, _ = fmt.Fprint(w, `{"dags":[{"dag_id":"a"},{"dag_id":"b"}],"total_entries":2}`)
		case "/api/v1/dags/~/dagRuns":
			crossDAG++
			lastSince = r.URL.Query().Get("updated_at_gte")
			if crossDAG == 1 {
				_, _ = fmt.Fprint(w, `{"dag_runs":[{"dag_id":"a","state":"success","logical_date":"2026-10-01T00:00:00Z"}],"total_entries":1}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"dag_runs":[{"dag_id":"b","state":"failed","logical_date":"2026-10-01T01:00:00Z"}],"total_entries":1}`)
		default:
			perDAG++
			_, _ = fmt.Fprint(w, `{"dag_runs":[],"total_entries":0}`)
		}
	})

	dags, _ := client.ListDAGs()
	if dags[0].LastRunState != "success" || perDAG != 1 {
		t.Fatalf("first load: a=%q, per-DAG lookups=%d (want only b)", dags[0].LastRunState, perDAG)
	}
	dags, _ = client.ListDAGs()
	if perDAG != 1 {
		t.Errorf("second load asked DAGs one by one again (%d)", perDAG)
	}
	if dags[0].LastRunState != "success" || dags[1].LastRunState != "failed" {
		t.Errorf("merge lost a run: %+v", dags)
	}
	if since, err := time.Parse(time.RFC3339, lastSince); err != nil || time.Since(since) > 10*time.Minute {
		t.Errorf("second poll should read only recent updates, got updated_at_gte=%q", lastSince)
	}
}

func TestAirflowClient_ListDAGs_CrossDAGFailureFallsBack(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/dags":
			_, _ = fmt.Fprint(w, `{"dags":[{"dag_id":"a"}],"total_entries":1}`)
		case "/api/v1/dags/~/dagRuns":
			w.WriteHeader(http.StatusBadRequest)
		default:
			_, _ = fmt.Fprint(w, `{"dag_runs":[{"dag_id":"a","state":"failed"}],"total_entries":1}`)
		}
	})
	dags, _ := client.ListDAGs()
	if dags[0].LastRunState != "failed" {
		t.Errorf("per-DAG fallback not used: %+v", dags[0])
	}
}

func TestAirflowClient_ClearTaskInstances_SendsOptions(t *testing.T) {
	var got map[string]interface{}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = fmt.Fprint(w, `{"task_instances":[{"task_id":"a"},{"task_id":"b"}]}`)
	})
	ids, err := client.ClearTaskInstances("d", "r", []string{"a"},
		model.ClearOptions{DryRun: true, IncludeDownstream: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[1] != "b" {
		t.Errorf("ids = %v", ids)
	}
	for k, want := range map[string]interface{}{"dry_run": true, "include_downstream": true, "include_upstream": false, "only_failed": false, "reset_dag_runs": true, "dag_run_id": "r"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}

func TestAirflowClient_SetTaskInstanceState(t *testing.T) {
	var method, path string
	var got map[string]interface{}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = fmt.Fprint(w, `{}`)
	})
	if err := client.SetTaskInstanceState("d", "r", "grp.t", "success"); err != nil {
		t.Fatal(err)
	}
	if method != "PATCH" || path != "/api/v1/dags/d/dagRuns/r/taskInstances/grp.t" {
		t.Errorf("%s %s", method, path)
	}
	if got["new_state"] != "success" || got["dry_run"] != false {
		t.Errorf("body = %v", got)
	}
}

func TestAirflowClient_ListProblemTaskInstances_Query(t *testing.T) {
	var q url.Values
	var p string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		p, q = r.URL.Path, r.URL.Query()
		_, _ = fmt.Fprint(w, `{"task_instances":[{"task_id":"t","dag_id":"d","state":"up_for_retry"}],"total_entries":1}`)
	})
	tasks, err := client.ListProblemTaskInstances(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if p != "/api/v1/dags/~/dagRuns/~/taskInstances" || len(q["state"]) != 3 || q.Get("start_date_gte") != "2026-09-30T00:00:00Z" {
		t.Errorf("path=%s query=%v", p, q)
	}
	if tasks[0].State != "up_for_retry" {
		t.Errorf("up_for_retry must not be folded into running: %q", tasks[0].State)
	}
}

func TestAirflowDAGRun_ToModel(t *testing.T) {
	var r airflowDAGRun
	_ = json.Unmarshal([]byte(`{"dag_run_id":"x","run_type":"manual","conf":{"k":1},"note":"retry","logical_date":"2026-10-01T00:00:00Z"}`), &r)
	run := r.toModel()
	if run.RunType != "manual" || run.Conf != `{"k":1}` || run.Note != "retry" || run.LogicalDate.IsZero() {
		t.Errorf("%+v", run)
	}
	_ = json.Unmarshal([]byte(`{"conf":{}}`), &r)
	if r.toModel().Conf != "" {
		t.Error("empty conf should read as none")
	}
}

func TestGetXComEntries_FetchesValuePerEntry(t *testing.T) {
	// Mirrors Airflow 2.10: the list omits "value", the single-entry endpoint carries it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		base := "/api/v1/dags/d/dagRuns/scheduled__2026-10-05T06:12:00+00:00/taskInstances/t/xcomEntries"
		switch r.URL.Path {
		case base:
			_, _ = fmt.Fprint(w, `{"xcom_entries": [{"key": "return_value", "map_index": -1}], "total_entries": 1}`)
		case base + "/return_value":
			if got := r.URL.Query().Get("map_index"); got != "-1" {
				t.Errorf("map_index = %q, want -1", got)
			}
			_, _ = fmt.Fprint(w, `{"key": "return_value", "map_index": -1, "value": "datetime.date@version=2(2026-09-09)"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	got := client.GetXComEntries("d", "scheduled__2026-10-05T06:12:00+00:00", "t")
	if len(got) != 1 || got[0].Value != "datetime.date@version=2(2026-09-09)" {
		t.Errorf("got %+v", got)
	}
}
