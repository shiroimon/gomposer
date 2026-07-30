package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
		input *string
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
		{"up_for_retry", "running"},
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
		fmt.Fprint(w, `{
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

	runs := client.ListDAGRuns("test_dag")
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
			fmt.Fprint(w, `{"dags": [
				{"dag_id": "dag_ok", "is_paused": false},
				{"dag_id": "dag_bad", "is_paused": false}
			]}`)
		case "/api/v1/dags/dag_ok/dagRuns":
			// order_by=-start_date&limit=1 → latest run first
			fmt.Fprint(w, `{"dag_runs": [
				{"dag_id": "dag_ok", "dag_run_id": "r1", "state": "success", "start_date": "2024-01-02T10:00:00+00:00"}
			]}`)
		case "/api/v1/dags/dag_bad/dagRuns":
			fmt.Fprint(w, `{"dag_runs": [
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
		fmt.Fprint(w, `{
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
		fmt.Fprint(w, `{"detail":"Not authorized"}`)
	}))
	defer server.Close()

	client := NewAirflowClient(server.URL)
	client.getToken = func() (string, error) { return "test-token", nil }

	runs := client.ListDAGRuns("test_dag")
	if len(runs) != 0 {
		t.Errorf("expected empty result on error, got %d", len(runs))
	}
}

func strPtr(s string) *string { return &s }
