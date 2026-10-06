package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/shiroimon/gomposer/internal/model"
)

func sampleDAGs() []model.DAG {
	now := time.Now()
	return []model.DAG{
		{ID: "dag_a", IsPaused: false, LastRunState: "success", LastRunDate: now},
		{ID: "dag_b", IsPaused: true, LastRunState: "failed", LastRunDate: now},
		{ID: "dag_c", IsPaused: false, LastRunState: "running", LastRunDate: now},
	}
}

func TestDAGListModel_CursorNavigation(t *testing.T) {
	m := NewDAGListModel(sampleDAGs())

	if dag, ok := m.SelectedDAG(); !ok || dag.ID != "dag_a" {
		t.Errorf("expected dag_a selected initially, got %q", dag.ID)
	}

	m.CursorDown()
	if dag, _ := m.SelectedDAG(); dag.ID != "dag_b" {
		t.Errorf("expected dag_b after CursorDown, got %q", dag.ID)
	}

	m.CursorDown()
	m.CursorDown() // should not go past last
	if dag, _ := m.SelectedDAG(); dag.ID != "dag_c" {
		t.Errorf("expected dag_c at bottom, got %q", dag.ID)
	}

	m.CursorUp()
	if dag, _ := m.SelectedDAG(); dag.ID != "dag_b" {
		t.Errorf("expected dag_b after CursorUp, got %q", dag.ID)
	}

	m.CursorUp()
	m.CursorUp() // should not go past first
	if dag, _ := m.SelectedDAG(); dag.ID != "dag_a" {
		t.Errorf("expected dag_a at top, got %q", dag.ID)
	}
}

func TestDAGListModel_ViewContainsDAGs(t *testing.T) {
	m := NewDAGListModel(sampleDAGs())
	view := m.View()

	for _, id := range []string{"dag_a", "dag_b", "dag_c"} {
		if !strings.Contains(view, id) {
			t.Errorf("expected view to contain %q", id)
		}
	}

	if !strings.Contains(view, "DAG ID") {
		t.Error("expected view to contain header 'DAG ID'")
	}
}

func TestDAGListModel_EmptyList(t *testing.T) {
	m := NewDAGListModel(nil)
	view := m.View()

	if !strings.Contains(view, "No DAGs found") {
		t.Error("expected 'No DAGs found' for empty list")
	}

	_, ok := m.SelectedDAG()
	if ok {
		t.Error("expected no selected DAG for empty list")
	}
}

func TestDAGListModel_Filter(t *testing.T) {
	m := NewDAGListModel(sampleDAGs())

	m.ApplyFilter("dag_a")
	if len(m.DAGs()) != 1 {
		t.Fatalf("expected 1 DAG after filter, got %d", len(m.DAGs()))
	}
	if dag, _ := m.SelectedDAG(); dag.ID != "dag_a" {
		t.Errorf("expected dag_a, got %q", dag.ID)
	}
	if !m.IsFiltered() {
		t.Error("expected IsFiltered() to be true")
	}

	// Case-insensitive
	m.ApplyFilter("DAG_B")
	if len(m.DAGs()) != 1 {
		t.Fatalf("expected 1 DAG for case-insensitive filter, got %d", len(m.DAGs()))
	}

	// Partial match
	m.ApplyFilter("dag_")
	if len(m.DAGs()) != 3 {
		t.Fatalf("expected 3 DAGs for partial match, got %d", len(m.DAGs()))
	}

	// No match
	m.ApplyFilter("nonexistent")
	if len(m.DAGs()) != 0 {
		t.Fatalf("expected 0 DAGs for no match, got %d", len(m.DAGs()))
	}
	view := m.View()
	if !strings.Contains(view, "No DAGs matching") {
		t.Error("expected 'No DAGs matching' message")
	}
}

func TestDAGListModel_ClearFilter(t *testing.T) {
	m := NewDAGListModel(sampleDAGs())

	m.ApplyFilter("dag_a")
	if len(m.DAGs()) != 1 {
		t.Fatal("filter not applied")
	}

	m.ClearFilter()
	if len(m.DAGs()) != 3 {
		t.Fatalf("expected 3 DAGs after clear, got %d", len(m.DAGs()))
	}
	if m.IsFiltered() {
		t.Error("expected IsFiltered() to be false after clear")
	}
}

func TestDAGListModel_FilterPreservedOnUpdate(t *testing.T) {
	m := NewDAGListModel(sampleDAGs())
	m.ApplyFilter("dag_a")

	// Simulate data refresh
	m.UpdateDAGs(sampleDAGs())
	if len(m.DAGs()) != 1 {
		t.Fatalf("expected filter preserved after UpdateDAGs, got %d DAGs", len(m.DAGs()))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly_ten", 11, "exactly_ten"},
		{"this_is_a_very_long_string", 10, "this_is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}
