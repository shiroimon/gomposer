package ui

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	m := NewDAGRunListModel("dag_a", sampleDAGRuns(), 0)

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
	m := NewDAGRunListModel("dag_a", sampleDAGRuns(), 0)
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
	m := NewDAGRunListModel("dag_a", nil, 0)
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
	m := NewDAGRunListModel("my_dag", nil, 0)
	if m.DagID() != "my_dag" {
		t.Errorf("expected 'my_dag', got %q", m.DagID())
	}
}

func TestDetectFalseSuccess_Concurrent(t *testing.T) {
	var runs []model.DAGRun
	for i := 0; i < 25; i++ {
		runs = append(runs, model.DAGRun{DagID: "d", RunID: fmt.Sprintf("run_%d", i), State: "success"})
	}
	runs = append(runs, model.DAGRun{DagID: "d", RunID: "run_failed", State: "failed"})
	m := NewDAGRunListModel("d", runs, 0)

	var calls int32
	m.DetectFalseSuccess(func(dagID, runID string) []model.TaskInstance {
		atomic.AddInt32(&calls, 1)
		if runID == "run_3" || runID == "run_17" {
			return []model.TaskInstance{{TaskID: "a", State: "upstream_failed"}}
		}
		return []model.TaskInstance{{TaskID: "a", State: "success"}}
	})

	if calls != 25 {
		t.Errorf("expected 25 lookups (success runs only), got %d", calls)
	}
	if len(m.falseSuccess) != 2 || !m.falseSuccess["run_3"] || !m.falseSuccess["run_17"] {
		t.Errorf("unexpected falseSuccess: %v", m.falseSuccess)
	}
}
