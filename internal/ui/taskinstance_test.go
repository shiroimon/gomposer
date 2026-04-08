package ui

import (
	"strings"
	"testing"
	"time"

	"gomposer/internal/model"
)

func sampleTaskInstances() []model.TaskInstance {
	now := time.Now()
	return []model.TaskInstance{
		{TaskID: "extract_data", DagID: "dag_a", RunID: "run_1", State: "success", Duration: 3 * time.Minute, StartDate: now.Add(-30 * time.Minute)},
		{TaskID: "transform_data", DagID: "dag_a", RunID: "run_1", State: "success", Duration: 6 * time.Minute, StartDate: now.Add(-25 * time.Minute)},
		{TaskID: "load_data", DagID: "dag_a", RunID: "run_1", State: "failed", Duration: 9 * time.Minute, StartDate: now.Add(-20 * time.Minute)},
	}
}

func TestTaskInstanceListModel_CursorNavigation(t *testing.T) {
	m := NewTaskInstanceListModel("dag_a", "run_1", sampleTaskInstances())

	if task, ok := m.SelectedTask(); !ok || task.TaskID != "extract_data" {
		t.Errorf("expected extract_data initially, got %q", task.TaskID)
	}

	m.CursorDown()
	if task, _ := m.SelectedTask(); task.TaskID != "transform_data" {
		t.Errorf("expected transform_data, got %q", task.TaskID)
	}

	m.CursorDown()
	m.CursorDown() // should not go past last
	if task, _ := m.SelectedTask(); task.TaskID != "load_data" {
		t.Errorf("expected load_data, got %q", task.TaskID)
	}

	m.CursorUp()
	m.CursorUp()
	m.CursorUp() // should not go past first
	if task, _ := m.SelectedTask(); task.TaskID != "extract_data" {
		t.Errorf("expected extract_data, got %q", task.TaskID)
	}
}

func TestTaskInstanceListModel_ViewContainsTasks(t *testing.T) {
	m := NewTaskInstanceListModel("dag_a", "run_1", sampleTaskInstances())
	view := m.View()

	for _, id := range []string{"extract_data", "transform_data", "load_data"} {
		if !strings.Contains(view, id) {
			t.Errorf("expected view to contain %q", id)
		}
	}

	if !strings.Contains(view, "Task ID") {
		t.Error("expected view to contain header 'Task ID'")
	}
}

func TestTaskInstanceListModel_EmptyList(t *testing.T) {
	m := NewTaskInstanceListModel("dag_a", "run_1", nil)
	view := m.View()

	if !strings.Contains(view, "No Task Instances found") {
		t.Error("expected 'No Task Instances found' for empty list")
	}

	_, ok := m.SelectedTask()
	if ok {
		t.Error("expected no selected task for empty list")
	}
}

func TestTaskInstanceListModel_IDs(t *testing.T) {
	m := NewTaskInstanceListModel("my_dag", "my_run", nil)
	if m.DagID() != "my_dag" {
		t.Errorf("expected 'my_dag', got %q", m.DagID())
	}
	if m.RunID() != "my_run" {
		t.Errorf("expected 'my_run', got %q", m.RunID())
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "<1m"},
		{3 * time.Minute, "3m"},
		{90 * time.Minute, "90m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
