package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

// problemWindow is how far back the cross-DAG scan looks. A daily DAG that failed yesterday is still in view.
const problemWindow = 72 * time.Hour

// ProblemListModel lists failed, retrying and running task instances across every DAG.
type ProblemListModel struct {
	tasks  []model.TaskInstance
	since  time.Time
	cursor int
	width  int
	height int
	offset int
}

var problemStateOrder = map[string]int{"failed": 0, "up_for_retry": 1, "running": 2}

func NewProblemListModel(tasks []model.TaskInstance, since time.Time) ProblemListModel {
	sorted := append([]model.TaskInstance(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if problemStateOrder[a.State] != problemStateOrder[b.State] {
			return problemStateOrder[a.State] < problemStateOrder[b.State]
		}
		return a.StartDate.After(b.StartDate)
	})
	return ProblemListModel{tasks: sorted, since: since}
}

func (m *ProblemListModel) SetSize(w int) { m.width = w }

func (m *ProblemListModel) SetHeight(h int) {
	m.height = h
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

func (m *ProblemListModel) rowsHeight() int {
	if m.height <= 0 {
		return 0
	}
	if h := m.height - tableHeaderLines(); h > 0 {
		return h
	}
	return 1
}

func (m *ProblemListModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

func (m *ProblemListModel) CursorDown() {
	if m.cursor < len(m.tasks)-1 {
		m.cursor++
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
}

func (m *ProblemListModel) Selected() (model.TaskInstance, bool) {
	if len(m.tasks) == 0 {
		return model.TaskInstance{}, false
	}
	return m.tasks[m.cursor], true
}

// Summary counts rows by state, e.g. "3 failed · 1 up_for_retry · 5 running".
func (m *ProblemListModel) Summary() string {
	counts := map[string]int{}
	for _, t := range m.tasks {
		counts[t.State]++
	}
	var parts []string
	for _, s := range model.ProblemTaskStates {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	if len(parts) == 0 {
		return "nothing failed, retrying or running"
	}
	return strings.Join(parts, " · ")
}

func (m *ProblemListModel) View() string {
	if len(m.tasks) == 0 {
		return HelpStyle.Render(fmt.Sprintf("  No failed, retrying or running tasks since %s.", m.since.Local().Format("2006-01-02 15:04")))
	}

	colState := 14
	colStart := 16
	rest := m.width - colState - colStart - 8
	if rest < 60 {
		rest = 60
	}
	colDAG := rest * 3 / 10
	colTask := rest * 4 / 10
	colRun := rest - colDAG - colTask

	header := TableHeaderStyle.Render(fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s",
		colState, "State", colDAG, "DAG ID", colTask, "Task ID", colRun, "Run ID", colStart, "Start"))

	var rows []string
	start, end := visibleRange(m.offset, m.cursor, len(m.tasks), m.rowsHeight())
	for i := start; i < end; i++ {
		t := m.tasks[i]
		cells := func(state string) string {
			return fmt.Sprintf("  %s %-*s %-*s %-*s %-*s", state,
				colDAG, truncate(t.DagID, colDAG),
				colTask, truncate(t.TaskID, colTask),
				colRun, truncate(t.RunID, colRun),
				colStart, t.StartDate.Local().Format("01-02 15:04"))
		}
		if i == m.cursor {
			rows = append(rows, SelectedRowStyle.Render(cells(fmt.Sprintf("%-*s", colState, t.State))))
			continue
		}
		rows = append(rows, cells(StateStyle(t.State).Render(fmt.Sprintf("%-*s", colState, t.State))))
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, strings.Join(rows, "\n"))
}
