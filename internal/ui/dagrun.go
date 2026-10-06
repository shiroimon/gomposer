package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

type DAGRunListModel struct {
	dagID        string
	runs         []model.DAGRun
	total        int // runs the DAG has in all; len(runs) is capped by the API client
	cursor       int
	width        int
	height       int               // rows available for the whole view; 0 = unlimited
	offset       int               // first visible row
	prevStates   map[string]string // runID → previous state for highlight
	changed      map[string]bool   // runID → recently changed
	falseSuccess map[string]bool   // runID → has upstream_failed tasks despite "success" state
}

func NewDAGRunListModel(dagID string, runs []model.DAGRun, total int) DAGRunListModel {
	states := make(map[string]string, len(runs))
	for _, r := range runs {
		states[r.RunID] = r.State
	}
	return DAGRunListModel{dagID: dagID, runs: runs, total: total, prevStates: states, changed: map[string]bool{}, falseSuccess: map[string]bool{}}
}

// CountLabel says how many of the DAG's runs are listed, so a capped list is not mistaken for the full history.
func (m *DAGRunListModel) CountLabel() string {
	if m.total > len(m.runs) {
		return fmt.Sprintf("latest %d of %d runs", len(m.runs), m.total)
	}
	return fmt.Sprintf("%d runs", len(m.runs))
}

// SelectRun moves the cursor to runID; it stays put when the run is not in the list.
func (m *DAGRunListModel) SelectRun(runID string) {
	for i, r := range m.runs {
		if r.RunID == runID {
			m.cursor = i
			m.offset = scrollOffset(m.offset, m.cursor, len(m.runs), m.rowsHeight())
			return
		}
	}
}

// RunInfo renders what the list has no room for: who started the run and with what conf.
func RunInfo(run model.DAGRun) string {
	conf := "(none)"
	if run.Conf != "" {
		var buf bytes.Buffer
		if json.Indent(&buf, []byte(run.Conf), "", "  ") == nil {
			conf = buf.String()
		} else {
			conf = run.Conf
		}
	}
	note := run.Note
	if note == "" {
		note = "(none)"
	}
	date := func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return fmt.Sprintf("Run ID:       %s\nType:         %s\nState:        %s\nLogical date: %s\nStart:        %s\nEnd:          %s\nNote:         %s\n\nConf:\n%s\n",
		run.RunID, runTypeLabel(run.RunType), run.State, date(run.LogicalDate), date(run.StartDate), date(run.EndDate), note, conf)
}

func runTypeLabel(t string) string {
	if t == "dataset_triggered" {
		return "dataset"
	}
	if t == "" {
		return "-"
	}
	return t
}

// SetFalseSuccess applies a false-success map computed in a background command.
func (m *DAGRunListModel) SetFalseSuccess(fs map[string]bool) {
	if fs == nil {
		fs = map[string]bool{}
	}
	m.falseSuccess = fs
}

func (m *DAGRunListModel) UpdateTotal(total int) { m.total = total }

// falseSuccessWorkers bounds concurrent task-instance requests per DAG.
const falseSuccessWorkers = 8

// DetectFalseSuccess checks each "success" run for upstream_failed tasks.
func (m *DAGRunListModel) DetectFalseSuccess(getTasks func(dagID, runID string) []model.TaskInstance) {
	m.falseSuccess = map[string]bool{}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, falseSuccessWorkers)
	)
	for _, run := range m.runs {
		if run.State != "success" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(runID string) {
			defer wg.Done()
			defer func() { <-sem }()
			for _, t := range getTasks(m.dagID, runID) {
				if t.State == "upstream_failed" || t.State == "skipped" {
					mu.Lock()
					m.falseSuccess[runID] = true
					mu.Unlock()
					return
				}
			}
		}(run.RunID)
	}
	wg.Wait()
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

func (m *DAGRunListModel) SetHeight(h int) {
	m.height = h
	m.offset = scrollOffset(m.offset, m.cursor, len(m.runs), m.rowsHeight())
}

// rowsHeight is the number of data rows that fit below the table header.
func (m *DAGRunListModel) rowsHeight() int {
	if m.height <= 0 {
		return 0
	}
	if h := m.height - tableHeaderLines(); h > 0 {
		return h
	}
	return 1
}

func (m *DAGRunListModel) CursorUp() {
	if m.cursor > 0 {
		m.cursor--
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.runs), m.rowsHeight())
}

func (m *DAGRunListModel) CursorDown() {
	if m.cursor < len(m.runs)-1 {
		m.cursor++
	}
	m.offset = scrollOffset(m.offset, m.cursor, len(m.runs), m.rowsHeight())
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
	colType := 9
	colDuration := 10
	colStart := 20
	colEnd := 20
	fixedCols := colState + colType + colDuration + colStart + colEnd + 7
	colRunID := m.width - fixedCols
	if colRunID < 25 {
		colRunID = 25
	}
	if colRunID > 60 {
		colRunID = 60
	}

	header := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
		colRunID, "Run ID",
		colState, "State",
		colType, "Type",
		colDuration, "Duration",
		colStart, "Start Date",
		colEnd, "End Date",
	)
	headerLine := TableHeaderStyle.Render(header)

	var rows []string
	start, end := visibleRange(m.offset, m.cursor, len(m.runs), m.rowsHeight())
	for i := start; i < end; i++ {
		run := m.runs[i]
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

		runType := truncate(runTypeLabel(run.RunType), colType)
		row := fmt.Sprintf("  %-*s %s %-*s %-*s %-*s %-*s",
			colRunID, truncate(run.RunID, colRunID),
			stateRendered,
			colType, runType,
			colDuration, duration,
			colStart, startDate,
			colEnd, endDate,
		)

		if i == m.cursor {
			plain := fmt.Sprintf("  %-*s %-*s %-*s %-*s %-*s %-*s",
				colRunID, truncate(run.RunID, colRunID),
				colState, displayState,
				colType, runType,
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
