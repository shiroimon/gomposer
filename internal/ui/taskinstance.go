package ui

import (
	"fmt"
	"strings"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

type TaskInstanceListModel struct {
	dagID      string
	runID      string
	tasks      []model.TaskInstance
	cursor     int
	width      int
	height     int               // rows available for the whole view; 0 = unlimited
	offset     int               // first visible row
	prevStates map[string]string // taskID → previous state
	changed    map[string]bool   // taskID → recently changed
}

func NewTaskInstanceListModel(dagID, runID string, tasks []model.TaskInstance) TaskInstanceListModel {
	states := make(map[string]string, len(tasks))
	for _, t := range tasks {
		states[t.TaskID] = t.State
	}
	return TaskInstanceListModel{dagID: dagID, runID: runID, tasks: tasks, prevStates: states, changed: map[string]bool{}}
}

// UpdateTasks updates the task list and detects state changes for highlighting.
func (m *TaskInstanceListModel) UpdateTasks(tasks []model.TaskInstance) {
	m.changed = map[string]bool{}
	for _, t := range tasks {
		if prev, ok := m.prevStates[t.TaskID]; ok && prev != t.State {
			m.changed[t.TaskID] = true
		}
	}
	m.tasks = tasks
	m.prevStates = make(map[string]string, len(tasks))
	for _, t := range tasks {
		m.prevStates[t.TaskID] = t.State
	}
}

func (m *TaskInstanceListModel) SetSize(w int) {
	m.width = w
}

func (m *TaskInstanceListModel) SetHeight(h int) {
	m.height = h
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

// rowsHeight is the number of data rows that fit below the table header.
func (m *TaskInstanceListModel) rowsHeight() int {
	if m.height <= 0 {
		return 0
	}
	if h := m.height - tableHeaderLines(); h > 0 {
		return h
	}
	return 1
}

func (m *TaskInstanceListModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

func (m *TaskInstanceListModel) CursorDown() {
	if m.cursor < len(m.tasks)-1 {
		m.cursor++
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

func (m *TaskInstanceListModel) SelectedTask() (model.TaskInstance, bool) {
	if len(m.tasks) == 0 {
		return model.TaskInstance{}, false
	}
	return m.tasks[m.cursor], true
}

// SelectTask moves the cursor to taskID; it stays put when the task is not in the list.
func (m *TaskInstanceListModel) SelectTask(taskID string) {
	for i, t := range m.tasks {
		if t.TaskID == taskID {
			m.cursor = i
			m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
			return
		}
	}
}

func (m *TaskInstanceListModel) DagID() string { return m.dagID }
func (m *TaskInstanceListModel) RunID() string { return m.runID }

func (m *TaskInstanceListModel) View() string {
	if len(m.tasks) == 0 {
		return HelpStyle.Render("  No Task Instances found.")
	}

	colState := 18
	colTries := 6
	colDuration := 12
	colStart := 20
	fixedCols := colState + colTries + colDuration + colStart + 6
	colTaskID := m.width - fixedCols
	if colTaskID < 20 {
		colTaskID = 20
	}
	if colTaskID > 50 {
		colTaskID = 50
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		colTaskID, "Task ID",
		colState, "State",
		colTries, "Tries",
		colDuration, "Duration",
		colStart, "Start Date",
	)
	headerLine := TableHeaderStyle.Render(header)

	var rows []string
	start, end := visibleRange(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
	for i := start; i < end; i++ {
		task := m.tasks[i]
		stateRendered := StateStyle(task.State).Render(fmt.Sprintf("%-*s", colState, task.State))
		tries := fmt.Sprintf("%d", task.TryNumber)
		duration := formatDuration(task.Duration)
		startDate := task.StartDate.Format("2006-01-02 15:04")

		row := fmt.Sprintf("  %-*s %s %-*s %-*s %-*s",
			colTaskID, truncate(task.TaskID, colTaskID),
			stateRendered,
			colTries, tries,
			colDuration, duration,
			colStart, startDate,
		)

		if i == m.cursor {
			plain := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
				colTaskID, truncate(task.TaskID, colTaskID),
				colState, task.State,
				colTries, tries,
				colDuration, duration,
				colStart, startDate,
			)
			row = SelectedRowStyle.Render(plain)
		} else if m.changed[task.TaskID] {
			row = ChangedRowStyle.Render(row)
		}

		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		strings.Join(rows, "\n"),
	)
}

func formatDuration(d interface{ Minutes() float64 }) string {
	mins := d.Minutes()
	if mins < 1 {
		return "<1m"
	}
	return fmt.Sprintf("%.0fm", mins)
}
