package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/shiroimon/gomposer/internal/api"
	"github.com/shiroimon/gomposer/internal/model"
)

func sampleDAGRuns() []model.DAGRun {
	now := time.Now()
	return []model.DAGRun{
		{DagID: "dag_a", RunID: "run_1", State: "success", StartDate: now.Add(-3 * time.Hour), EndDate: now.Add(-2 * time.Hour)},
		{DagID: "dag_a", RunID: "run_2", State: "failed", StartDate: now.Add(-2 * time.Hour), EndDate: now.Add(-1 * time.Hour)},
		{DagID: "dag_a", RunID: "run_3", State: "running", StartDate: now.Add(-30 * time.Minute)},
	}
}

func TestDAGRunListModel_CursorNavigation(t *testing.T) {
	m := NewDAGRunListModel("dag_a", sampleDAGRuns())

	if run, ok := m.SelectedRun(); !ok || run.RunID != "run_1" {
		t.Errorf("expected run_1 initially, got %q", run.RunID)
	}

	m.CursorDown()
	if run, _ := m.SelectedRun(); run.RunID != "run_2" {
		t.Errorf("expected run_2, got %q", run.RunID)
	}

	m.CursorDown()
	m.CursorDown() // should not go past last
	if run, _ := m.SelectedRun(); run.RunID != "run_3" {
		t.Errorf("expected run_3, got %q", run.RunID)
	}

	m.CursorUp()
	m.CursorUp()
	m.CursorUp() // should not go past first
	if run, _ := m.SelectedRun(); run.RunID != "run_1" {
		t.Errorf("expected run_1, got %q", run.RunID)
	}
}

func TestDAGRunListModel_ViewContainsRuns(t *testing.T) {
	m := NewDAGRunListModel("dag_a", sampleDAGRuns())
	view := m.View()

	for _, id := range []string{"run_1", "run_2", "run_3"} {
		if !strings.Contains(view, id) {
			t.Errorf("expected view to contain %q", id)
		}
	}

	for _, state := range []string{"success", "failed", "running"} {
		if !strings.Contains(view, state) {
			t.Errorf("expected view to contain state %q", state)
		}
	}
}

func TestDAGRunListModel_EmptyList(t *testing.T) {
	m := NewDAGRunListModel("dag_a", nil)
	view := m.View()

	if !strings.Contains(view, "No DAG Runs found") {
		t.Error("expected 'No DAG Runs found' for empty list")
	}

	_, ok := m.SelectedRun()
	if ok {
		t.Error("expected no selected run for empty list")
	}
}

func TestDAGRunListModel_DagID(t *testing.T) {
	m := NewDAGRunListModel("my_dag", nil)
	if m.DagID() != "my_dag" {
		t.Errorf("expected 'my_dag', got %q", m.DagID())
	}
}

func TestDetectFalseSuccessMap(t *testing.T) {
	ds := api.NewMockDataSource()

	// report_weekly_summary's latest run is "success" but its last task is
	// upstream_failed in the mock, so it must be flagged as a false success.
	runs := ds.ListDAGRuns("report_weekly_summary")
	fs := detectFalseSuccessMap(ds, "report_weekly_summary", runs)
	flagged := false
	for _, ok := range fs {
		if ok {
			flagged = true
		}
	}
	if !flagged {
		t.Error("expected a false-success run to be flagged for report_weekly_summary")
	}

	// A clean DAG whose successful runs have no upstream_failed/skipped tasks
	// must not be flagged.
	cleanRuns := ds.ListDAGRuns("etl_patient_records")
	if got := detectFalseSuccessMap(ds, "etl_patient_records", cleanRuns); len(got) != 0 {
		t.Errorf("expected no false-success flags for etl_patient_records, got %v", got)
	}
}
