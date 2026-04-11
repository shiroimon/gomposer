package ui

import (
	"fmt"
	"strings"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

type DAGRunListModel struct {
	dagID          string
	runs           []model.DAGRun
	cursor         int
	width          int
	prevStates     map[string]string // runID → previous state for highlight
	changed        map[string]bool   // runID → recently changed
	falseSuccess   map[string]bool   // runID → has upstream_failed tasks despite "success" state
}

func NewDAGRunListModel(dagID string, runs []model.DAGRun) DAGRunListModel {
	states := make(map[string]string, len(runs))
	for _, r := range runs {
		states[r.RunID] = r.State
	}
	return DAGRunListModel{dagID: dagID, runs: runs, prevStates: states, changed: map[string]bool{}, falseSuccess: map[string]bool{}}
}

// DetectFalseSuccess checks each "success" run for upstream_failed tasks.
func (m *DAGRunListModel) DetectFalseSuccess(getTasks func(dagID, runID string) []model.TaskInstance) {
	m.falseSuccess = map[string]bool{}
	for _, run := range m.runs {
		if run.State == "success" {
			tasks := getTasks(m.dagID, run.RunID)
			for _, t := range tasks {
				if t.State == "upstream_failed" || t.State == "skipped" {
					m.falseSuccess[run.RunID] = true
					break
				}
			}
		}
	}
}

// UpdateRuns updates the run list and detects state changes for highlighting.
func (m *DAGRunListModel) UpdateRuns(runs []model.DAGRun) {
	m.changed = map[string]bool{}
	for _, r := range runs {
		if prev, ok := m.prevStates[r.RunID]; ok && prev != r.State {
			m.changed[r.RunID] = true
		}
	}
	m.runs = runs
	m.prevStates = make(map[string]string, len(runs))
	for _, r := range runs {
		m.prevStates[r.RunID] = r.State
	}
}

func (m *DAGRunListModel) SetSize(w int) {
	m.width = w
}

func (m *DAGRunListModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *DAGRunListModel) CursorDown() {
	if m.cursor < len(m.runs)-1 {
		m.cursor++
	}
}

func (m *DAGRunListModel) SelectedRun() (model.DAGRun, bool) {
	if len(m.runs) == 0 {
		return model.DAGRun{}, false
	}
	return m.runs[m.cursor], true
}

func (m *DAGRunListModel) DagID() string {
	return m.dagID
}

func (m *DAGRunListModel) View() string {
	if len(m.runs) == 0 {
		return HelpStyle.Render("  No DAG Runs found.")
	}

	colState := 10
	colDuration := 10
	colStart := 20
	colEnd := 20
	fixedCols := colState + colDuration + colStart + colEnd + 6
	colRunID := m.width - fixedCols
	if colRunID < 25 {
		colRunID = 25
	}
	if colRunID > 60 {
		colRunID = 60
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		colRunID, "Run ID",
		colState, "State",
		colDuration, "Duration",
		colStart, "Start Date",
		colEnd, "End Date",
	)
	headerLine := TableHeaderStyle.Render(header)

	var rows []string
	for i, run := range m.runs {
		displayState := run.State
		if m.falseSuccess[run.RunID] {
			displayState = "success*"
		}
		stateRendered := m.runStateStyle(run).Render(fmt.Sprintf("%-*s", colState, displayState))

		duration := "-"
		if !run.EndDate.IsZero() {
			duration = formatDuration(run.EndDate.Sub(run.StartDate))
		} else if run.State == "running" {
			duration = "running"
		}

		startDate := run.StartDate.Format("2006-01-02 15:04")
		endDate := "-"
		if !run.EndDate.IsZero() {
			endDate = run.EndDate.Format("2006-01-02 15:04")
		}

		row := fmt.Sprintf("  %-*s %s %-*s %-*s %-*s",
			colRunID, truncate(run.RunID, colRunID),
			stateRendered,
			colDuration, duration,
			colStart, startDate,
			colEnd, endDate,
		)

		if i == m.cursor {
			plain := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
				colRunID, truncate(run.RunID, colRunID),
				colState, displayState,
				colDuration, duration,
				colStart, startDate,
				colEnd, endDate,
			)
			row = SelectedRowStyle.Render(plain)
		} else if m.changed[run.RunID] {
			row = ChangedRowStyle.Render(row)
		}

		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		strings.Join(rows, "\n"),
	)
}

func (m *DAGRunListModel) runStateStyle(run model.DAGRun) lipgloss.Style {
	if m.falseSuccess[run.RunID] {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // yellow/orange for false success
	}
	return StateStyle(run.State)
}
