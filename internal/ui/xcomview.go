package ui

import (
	"fmt"
	"strings"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

type XComViewModel struct {
	dagID   string
	runID   string
	taskID  string
	entries []model.XComEntry
	cursor  int
	width   int
	height  int // rows available for the whole view; 0 = unlimited
	offset  int // first visible row
}

func NewXComViewModel(dagID, runID, taskID string, entries []model.XComEntry) XComViewModel {
	return XComViewModel{dagID: dagID, runID: runID, taskID: taskID, entries: entries}
}

func (m *XComViewModel) SetSize(w int) { m.width = w }

func (m *XComViewModel) SetHeight(h int) {
	m.height = h
	m.offset = scrollOffset(m.offset, m.cursor, len(m.entries), m.rowsHeight())
}

// rowsHeight is the number of data rows that fit below the table header.
func (m *XComViewModel) rowsHeight() int {
	if m.height <= 0 {
		return 0
	}
	if h := m.height - tableHeaderLines(); h > 0 {
		return h
	}
	return 1
}

func (m *XComViewModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.entries), m.rowsHeight())
}

func (m *XComViewModel) CursorDown() {
	if m.cursor < len(m.entries)-1 {
		m.cursor++
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.entries), m.rowsHeight())
}

func (m *XComViewModel) View() string {
	if len(m.entries) == 0 {
		return HelpStyle.Render("  No XCom entries found.")
	}

	colKey := 30
	colVal := m.width - colKey - 6
	if colVal < 20 {
		colVal = 20
	}
	if colVal > 80 {
		colVal = 80
	}

	header := fmt.Sprintf("  %-*s %-*s", colKey, "Key", colVal, "Value")
	headerLine := TableHeaderStyle.Render(header)

	var rows []string
	start, end := visibleRange(m.offset, m.cursor, len(m.entries), m.rowsHeight())
	for i := start; i < end; i++ {
		e := m.entries[i]
		val := strings.ReplaceAll(e.Value, "\n", " ")
		row := fmt.Sprintf("  %-*s %-*s", colKey, truncate(e.Key, colKey), colVal, truncate(val, colVal))
		if i == m.cursor {
			row = SelectedRowStyle.Render(row)
		}
		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		headerLine,
		strings.Join(rows, "\n"),
	)
}
