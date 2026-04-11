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
}

func NewXComViewModel(dagID, runID, taskID string, entries []model.XComEntry) XComViewModel {
	return XComViewModel{dagID: dagID, runID: runID, taskID: taskID, entries: entries}
}

func (m *XComViewModel) SetSize(w int) { m.width = w }

func (m *XComViewModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *XComViewModel) CursorDown() {
	if m.cursor < len(m.entries)-1 {
		m.cursor++
	}
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
	for i, e := range m.entries {
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
