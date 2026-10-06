package api

import (
	"testing"

	"github.com/shiroimon/gomposer/internal/model"
)

func TestMockDataSource_ListDAGs(t *testing.T) {
	ds := NewMockDataSource()
	dags, err := ds.ListDAGs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(dags) != 6 {
		t.Fatalf("expected 6 DAGs, got %d", len(dags))
	}

	if dags[0].ID != "etl_patient_records" {
		t.Errorf("expected first DAG to be etl_patient_records, got %q", dags[0].ID)
	}
}

func TestMockDataSource_ListDAGRuns(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("etl_patient_records")

	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}

	for _, r := range runs {
		if r.DagID != "etl_patient_records" {
			t.Errorf("expected DagID 'etl_patient_records', got %q", r.DagID)
		}
	}
}

func TestMockDataSource_ListDAGRuns_Unknown(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("nonexistent")

	if len(runs) != 0 {
		t.Fatalf("expected 0 runs for unknown DAG, got %d", len(runs))
	}
}

func TestMockDataSource_ListTaskInstances(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("etl_patient_records")
	if len(runs) == 0 {
		t.Fatal("expected runs")
	}

	tasks := ds.ListTaskInstances("etl_patient_records", runs[0].RunID)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 task instances, got %d", len(tasks))
	}
}

func TestMockDataSource_GetTaskLog(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("etl_patient_records")
	tasks := ds.ListTaskInstances("etl_patient_records", runs[0].RunID)

	log := ds.GetTaskLog("etl_patient_records", runs[0].RunID, tasks[0].TaskID, 1)
	if log == "" || log == "(no log available)" {
		t.Error("expected log content")
	}
}

func TestMockDataSource_GetTaskLog_NotFound(t *testing.T) {
	ds := NewMockDataSource()
	log := ds.GetTaskLog("x", "y", "z", 1)
	if log != "(no log available)" {
		t.Errorf("expected fallback message, got %q", log)
	}
}

func TestMockDataSource_TriggerDAG(t *testing.T) {
	ds := NewMockDataSource()
	initialRuns := len(first(ds.ListDAGRuns("etl_patient_records")))

	run, err := ds.TriggerDAG("etl_patient_records", nil)
	if err != nil {
		t.Fatalf("TriggerDAG failed: %v", err)
	}
	if run.State != "running" {
		t.Errorf("expected state 'running', got %q", run.State)
	}
	if run.DagID != "etl_patient_records" {
		t.Errorf("expected DagID 'etl_patient_records', got %q", run.DagID)
	}

	newRuns := len(first(ds.ListDAGRuns("etl_patient_records")))
	if newRuns != initialRuns+1 {
		t.Errorf("expected %d runs after trigger, got %d", initialRuns+1, newRuns)
	}

	// DAG last run state should be updated
	dags, _ := ds.ListDAGs()
	for _, dag := range dags {
		if dag.ID == "etl_patient_records" {
			if dag.LastRunState != "running" {
				t.Errorf("expected LastRunState 'running', got %q", dag.LastRunState)
			}
			break
		}
	}
}

func TestMockDataSource_TriggerDAG_NotFound(t *testing.T) {
	ds := NewMockDataSource()
	_, err := ds.TriggerDAG("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown DAG")
	}
}

func TestMockDataSource_TogglePause(t *testing.T) {
	ds := NewMockDataSource()

	// etl_patient_records starts as active (IsPaused=false)
	paused, err := ds.TogglePause("etl_patient_records")
	if err != nil {
		t.Fatalf("TogglePause failed: %v", err)
	}
	if !paused {
		t.Error("expected paused=true after toggle from active")
	}

	// Toggle again
	paused, err = ds.TogglePause("etl_patient_records")
	if err != nil {
		t.Fatalf("TogglePause failed: %v", err)
	}
	if paused {
		t.Error("expected paused=false after second toggle")
	}
}

func TestMockDataSource_TogglePause_NotFound(t *testing.T) {
	ds := NewMockDataSource()
	_, err := ds.TogglePause("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown DAG")
	}
}

func TestMockDataSource_ImplementsInterface(t *testing.T) {
	var _ DataSource = (*MockDataSource)(nil)
}

func first[T any](v T, _ int) T { return v }

func TestMockDataSource_ListDAGRunsNewestFirst(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("etl_prescription_sync")
	if runs[0].State != "failed" || runs[0].StartDate.Before(runs[1].StartDate) {
		t.Errorf("runs[0] should be the latest (failed) run: %+v", runs[0])
	}
}

func TestMockDataSource_ClearDryRunLeavesState(t *testing.T) {
	ds := NewMockDataSource()
	runs, _ := ds.ListDAGRuns("etl_prescription_sync")
	tasks := ds.ListTaskInstances("etl_prescription_sync", runs[0].RunID)

	ids, err := ds.ClearTaskInstances("etl_prescription_sync", runs[0].RunID, []string{tasks[0].TaskID},
		model.ClearOptions{DryRun: true, IncludeDownstream: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Errorf("downstream should pull in all 3 tasks, got %v", ids)
	}
	after := ds.ListTaskInstances("etl_prescription_sync", runs[0].RunID)
	if after[0].State != tasks[0].State || after[2].State != "failed" {
		t.Errorf("dry run changed state: %+v", after)
	}
}
