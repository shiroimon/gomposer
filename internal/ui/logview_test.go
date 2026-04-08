package ui

import (
	"strings"
	"testing"
)

func TestLogViewModel_IDs(t *testing.T) {
	m := NewLogViewModel("dag_a", "run_1", "task_1", "log content", 1, 1, 80, 20)
	if m.DagID() != "dag_a" {
		t.Errorf("expected 'dag_a', got %q", m.DagID())
	}
	if m.RunID() != "run_1" {
		t.Errorf("expected 'run_1', got %q", m.RunID())
	}
	if m.TaskID() != "task_1" {
		t.Errorf("expected 'task_1', got %q", m.TaskID())
	}
}

func TestLogViewModel_ViewContainsContent(t *testing.T) {
	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	m := NewLogViewModel("dag_a", "run_1", "task_1", content, 1, 1, 80, 20)
	view := m.View()

	if !strings.Contains(view, "line 1") {
		t.Error("expected view to contain 'line 1'")
	}
}

func TestLogViewModel_ScrollInfo(t *testing.T) {
	m := NewLogViewModel("dag_a", "run_1", "task_1", "short", 1, 1, 80, 20)
	info := m.ScrollInfo()

	// Should contain a percentage
	if !strings.Contains(info, "%") {
		t.Errorf("expected scroll info to contain '%%', got %q", info)
	}
}

func TestLogViewModel_SetSize(t *testing.T) {
	m := NewLogViewModel("dag_a", "run_1", "task_1", "content", 1, 1, 80, 20)
	m.SetSize(120, 40)
	// No panic = success; viewport resizes internally
}
