package ui

import (
	"fmt"
	"strings"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

type DAGListModel struct {
	allDAGs   []model.DAG // unfiltered
	dags      []model.DAG // filtered (displayed)
	cursor    int
	width     int
	height    int // rows available for the whole view; 0 = unlimited
	offset    int // first visible row
	filter    string
	filtered  bool
	selected  map[string]bool // multi-select by DAG ID
	favorites map[string]bool // pinned DAG IDs
}

func NewDAGListModel(dags []model.DAG) DAGListModel {
	return DAGListModel{allDAGs: dags, dags: dags, selected: map[string]bool{}, favorites: LoadFavorites()}
}

func (m *DAGListModel) SetSize(w int) {
	m.width = w
}

func (m *DAGListModel) SetHeight(h int) {
	m.height = h
	m.offset = scrollOffset(m.offset, m.cursor, len(m.dags), m.rowsHeight())
}

// rowsHeight is the number of data rows that fit below the table header.
func (m *DAGListModel) rowsHeight() int {
	if m.height <= 0 {
		return 0
	}
	if h := m.height - tableHeaderLines(); h > 0 {
		return h
	}
	return 1
}

// FilterSummary describes the active filter, e.g. `Filter: "drug" (1/19)`; empty when not filtering.
func (m *DAGListModel) FilterSummary() string {
	if !m.filtered {
		return ""
	}
	return fmt.Sprintf("Filter: %q (%d/%d)", m.filter, len(m.dags), len(m.allDAGs))
}

func (m *DAGListModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.dags), m.rowsHeight())
}

func (m *DAGListModel) CursorDown() {
	if m.cursor < len(m.dags)-1 {
		m.cursor++
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.dags), m.rowsHeight())
}

func (m *DAGListModel) SelectedDAG() (model.DAG, bool) {
	if len(m.dags) == 0 {
		return model.DAG{}, false
	}
	return m.dags[m.cursor], true
}

func (m *DAGListModel) DAGs() []model.DAG {
	return m.dags
}

func (m *DAGListModel) UpdateDAGs(dags []model.DAG) {
	m.allDAGs = dags
	m.applyFilter()
}

func (m *DAGListModel) ApplyFilter(query string) {
	m.filter = query
	m.filtered = query != ""
	m.applyFilter()
}

func (m *DAGListModel) ClearFilter() {
	m.filter = ""
	m.filtered = false
	m.applyFilter()
}

func (m *DAGListModel) Filter() string {
	return m.filter
}

func (m *DAGListModel) IsFiltered() bool {
	return m.filtered
}

func (m *DAGListModel) ToggleSelect() {
	if len(m.dags) == 0 {
		return
	}
	id := m.dags[m.cursor].ID
	if m.selected[id] {
		delete(m.selected, id)
	} else {
		m.selected[id] = true
	}
}

func (m *DAGListModel) SelectedDAGs() []model.DAG {
	var result []model.DAG
	for _, dag := range m.dags {
		if m.selected[dag.ID] {
			result = append(result, dag)
		}
	}
	return result
}

func (m *DAGListModel) HasSelection() bool {
	return len(m.selected) > 0
}

func (m *DAGListModel) ClearSelection() {
	m.selected = map[string]bool{}
}

func (m *DAGListModel) ToggleFavorite() {
	if len(m.dags) == 0 {
		return
	}
	id := m.dags[m.cursor].ID
	if m.favorites[id] {
		delete(m.favorites, id)
	} else {
		m.favorites[id] = true
	}
	SaveFavorites(m.favorites)
	m.applyFilter()
}

func (m *DAGListModel) IsFavorite(dagID string) bool {
	return m.favorites[dagID]
}

func (m *DAGListModel) applyFilter() {
	if m.filter == "" {
		m.dags = make([]model.DAG, len(m.allDAGs))
		copy(m.dags, m.allDAGs)
	} else {
		lower := strings.ToLower(m.filter)
		var filtered []model.DAG
		for _, dag := range m.allDAGs {
			if strings.Contains(strings.ToLower(dag.ID), lower) {
				filtered = append(filtered, dag)
			}
		}
		m.dags = filtered
	}
	// Sort: favorites first, then original order
	m.sortFavoritesFirst()
	if m.cursor >= len(m.dags) {
		m.cursor = len(m.dags) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *DAGListModel) sortFavoritesFirst() {
	if len(m.favorites) == 0 {
		return
	}
	var pinned, rest []model.DAG
	for _, dag := range m.dags {
		if m.favorites[dag.ID] {
			pinned = append(pinned, dag)
		} else {
			rest = append(rest, dag)
		}
	}
	m.dags = append(pinned, rest...)
}

func (m *DAGListModel) View() string {
	if len(m.dags) == 0 {
		if m.filtered {
			return HelpStyle.Render(fmt.Sprintf("  No DAGs matching %q", m.filter))
		}
		return HelpStyle.Render("  No DAGs found.")
	}

	colState := 10
	colSchedule := 16
	colLastRun := 12
	colLastDate := 20
	colNextRun := 20
	fixedCols := colState + colSchedule + colLastRun + colLastDate + colNextRun + 8 // 8 = marker(2) + separators
	colDAGID := m.width - fixedCols
	if colDAGID < 20 {
		colDAGID = 20
	}
	if colDAGID > 50 {
		colDAGID = 50
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
		colDAGID, "DAG ID",
		colState, "State",
		colSchedule, "Schedule",
		colLastRun, "Last Run",
		colLastDate, "Last Run Date",
		colNextRun, "Next Run",
	)
	headerLine := TableHeaderStyle.Render(header)

	var rows []string
	start, end := visibleRange(m.offset, m.cursor, len(m.dags), m.rowsHeight())
	for i := start; i < end; i++ {
		dag := m.dags[i]
		stateText := "active"
		if dag.IsPaused {
			stateText = "paused"
		}
		stateRendered := PausedStyle(dag.IsPaused).Render(fmt.Sprintf("%-*s", colState, stateText))

		schedule := dag.ScheduleInterval
		if schedule == "" {
			schedule = "-"
		}

		lastRunRendered := StateStyle(dag.LastRunState).Render(fmt.Sprintf("%-*s", colLastRun, dag.LastRunState))

		lastDate := "-"
		if !dag.LastRunDate.IsZero() {
			lastDate = dag.LastRunDate.Format("2006-01-02 15:04")
		}

		nextRun := "-"
		if !dag.NextDagRun.IsZero() {
			nextRun = dag.NextDagRun.Format("2006-01-02 15:04")
		}

		marker := "  "
		if m.selected[dag.ID] && m.favorites[dag.ID] {
			marker = "*●"
		} else if m.selected[dag.ID] {
			marker = " ●"
		} else if m.favorites[dag.ID] {
			marker = "* "
		}

		row := fmt.Sprintf("%s%-*s %s %-*s %s %-*s %-*s",
			marker,
			colDAGID, truncate(dag.ID, colDAGID),
			stateRendered,
			colSchedule, truncate(schedule, colSchedule),
			lastRunRendered,
			colLastDate, lastDate,
			colNextRun, nextRun,
		)

		if i == m.cursor {
			plain := fmt.Sprintf("%s%-*s %-*s %-*s %-*s %-*s %-*s",
				marker,
				colDAGID, truncate(dag.ID, colDAGID),
				colState, stateText,
				colSchedule, truncate(schedule, colSchedule),
				colLastRun, dag.LastRunState,
				colLastDate, lastDate,
				colNextRun, nextRun,
			)
			row = SelectedRowStyle.Render(plain)
		}

		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		strings.Join(rows, "\n"),
	)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
