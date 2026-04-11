package ui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/shiroimon/gomposer/internal/api"
	"github.com/shiroimon/gomposer/internal/config"
	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type overlayMode int

const (
	overlayNone overlayMode = iota
	overlayXCom
	overlayGraph
	overlaySource
	overlayDiagnose
	overlayImportErrors
)

// headerLines returns the number of lines used by header chrome.
// title(1) + blank(1) + tabs(2, including border) + margin(1) + subtitle+blank(0 or 2) + status/help(2-3)
func (m AppModel) headerLines() int {
	lines := 7 // title(1) + blank before tabs(1) + tabs(2) + margin(1) + status(1) + help row 1(1)
	if m.tab != TabDAGs {
		lines += 2 // subtitle + blank line after it
	}
	_, secondary := m.helpRows()
	if len(secondary) > 0 {
		lines++ // help row 2
	}
	return lines
}

var statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).PaddingLeft(1)
var confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).PaddingLeft(1)
var filterInputStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")).PaddingLeft(1)

// clearStatusMsg is sent after a delay to clear the status message.
type clearStatusMsg struct{}

// autoRefreshMsg triggers a periodic refresh.
type autoRefreshMsg struct{}

// dagsLoadedMsg is sent when the initial DAG list has been fetched.
type dagsLoadedMsg struct {
	dags []model.DAG
	err  error
}

type AppModel struct {
	ds               api.DataSource
	tab              Tab
	dagList          DAGListModel
	dagRunList       DAGRunListModel
	taskInstanceList TaskInstanceListModel
	logView          LogViewModel
	width            int
	height           int
	statusMsg        string
	confirmTrigger   bool
	confirmDagID     string
	triggerConfMode  bool   // JSON conf input mode
	triggerConfInput string // JSON conf text
	confirmClear     bool   // clear task instance confirmation
	confirmClearAll  bool   // clear all tasks in run
	clearTaskID      string
	confirmMarkState string // "success" or "failed" — DAG Run manual mark
	markRunID        string
	refreshInterval  time.Duration // 0 = auto-refresh disabled
	filterMode       bool
	filterInput      string
	loading          bool
	loadError        string // API error message to display
	// Environment management
	cfg            *config.Config // nil in mock mode
	envName        string         // current environment key
	envSelectMode  bool           // environment picker dialog
	envSelectIdx   int
	prevFailedDAGs map[string]bool // track previous failed DAGs for notifications
	// Overlay views
	overlay        overlayMode
	xcomView       XComViewModel
	overlayVP      viewport.Model // used for graph, source, diagnose views
	overlayContent string
	overlayTitle   string
}

// EnvInfo holds environment display info passed from main.
type EnvInfo struct {
	Name       string
	ColorTheme string
}

func NewAppModel(ds api.DataSource, refreshIntervalSec int, cfg *config.Config, envName string) AppModel {
	var interval time.Duration
	if refreshIntervalSec > 0 {
		interval = time.Duration(refreshIntervalSec) * time.Second
	}
	// Apply color theme from config
	if cfg != nil {
		if env, ok := cfg.Environments[envName]; ok && env.ColorTheme != "" {
			SetTheme(env.ColorTheme)
		}
	}
	return AppModel{
		ds:              ds,
		tab:             TabDAGs,
		dagList:         NewDAGListModel(nil),
		loading:         true,
		refreshInterval: interval,
		cfg:             cfg,
		envName:         envName,
		prevFailedDAGs:  map[string]bool{},
	}
}

func (m AppModel) Init() tea.Cmd {
	loadDAGs := func() tea.Msg {
		dags, err := m.ds.ListDAGs()
		return dagsLoadedMsg{dags: dags, err: err}
	}
	if m.refreshInterval > 0 {
		return tea.Batch(loadDAGs, m.autoRefreshTick())
	}
	return loadDAGs
}

func (m AppModel) autoRefreshTick() tea.Cmd {
	return tea.Tick(m.refreshInterval, func(time.Time) tea.Msg {
		return autoRefreshMsg{}
	})
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dagsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			errText := msg.err.Error()
			if strings.Contains(errText, "auth") || strings.Contains(errText, "401") || strings.Contains(errText, "403") || strings.Contains(errText, "token") {
				m.loadError = fmt.Sprintf("Error: %s\n\nRun: gcloud auth login", errText)
			} else {
				m.loadError = fmt.Sprintf("Error: %s", errText)
			}
			return m, nil
		}
		m.loadError = ""
		m.dagList.UpdateDAGs(msg.dags)
		// Initialize failed DAG tracking (don't notify on first load)
		m.prevFailedDAGs = map[string]bool{}
		for _, dag := range msg.dags {
			if dag.LastRunState == "failed" {
				m.prevFailedDAGs[dag.ID] = true
			}
		}
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case autoRefreshMsg:
		m.refresh()
		return m, m.autoRefreshTick()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dagList.SetSize(msg.Width)
		m.dagRunList.SetSize(msg.Width)
		m.taskInstanceList.SetSize(msg.Width)
		m.logView.SetSize(msg.Width, msg.Height-m.headerLines())
		return m, nil

	case tea.KeyMsg:
		// Handle filter input mode
		if m.filterMode {
			switch msg.String() {
			case "enter":
				m.filterMode = false
				m.dagList.ApplyFilter(m.filterInput)
				if m.filterInput == "" {
					m.statusMsg = "Filter cleared"
				} else {
					m.statusMsg = fmt.Sprintf("Filter applied: %q", m.filterInput)
				}
				return m, clearStatusAfter(3 * time.Second)
			case "esc":
				m.filterMode = false
				m.filterInput = ""
				m.dagList.ClearFilter()
				m.statusMsg = "Filter cleared"
				return m, clearStatusAfter(3 * time.Second)
			case "backspace":
				if len(m.filterInput) > 0 {
					m.filterInput = m.filterInput[:len(m.filterInput)-1]
				}
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				// Only accept printable characters
				if len(msg.String()) == 1 {
					m.filterInput += msg.String()
				}
				return m, nil
			}
		}

		// Handle environment selector
		if m.envSelectMode {
			envNames := m.cfg.EnvNames()
			switch msg.String() {
			case "up", "k":
				if m.envSelectIdx > 0 {
					m.envSelectIdx--
				}
				return m, nil
			case "down", "j":
				if m.envSelectIdx < len(envNames)-1 {
					m.envSelectIdx++
				}
				return m, nil
			case "enter":
				selected := envNames[m.envSelectIdx]
				m.envSelectMode = false
				if selected != m.envName {
					env := m.cfg.Environments[selected]
					m.envName = selected
					m.ds = api.NewAirflowClient(env.WebserverURL)
					if env.ColorTheme != "" {
						SetTheme(env.ColorTheme)
					} else {
						SetTheme("default")
					}
					m.tab = TabDAGs
					m.loading = true
					m.dagList = NewDAGListModel(nil)
					loadDAGs := func() tea.Msg {
						dags, err := m.ds.ListDAGs()
						return dagsLoadedMsg{dags: dags, err: err}
					}
					m.statusMsg = fmt.Sprintf("Switched to %s", selected)
					return m, tea.Batch(loadDAGs, clearStatusAfter(3*time.Second))
				}
				return m, nil
			case "esc", "q":
				m.envSelectMode = false
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}

		// Handle trigger conf input mode
		if m.triggerConfMode {
			switch msg.String() {
			case "enter":
				m.triggerConfMode = false
				var conf map[string]interface{}
				if m.triggerConfInput != "" {
					if err := json.Unmarshal([]byte(m.triggerConfInput), &conf); err != nil {
						m.statusMsg = fmt.Sprintf("Invalid JSON: %v", err)
						m.triggerConfInput = ""
						m.confirmDagID = ""
						return m, clearStatusAfter(5 * time.Second)
					}
				}
				run, err := m.ds.TriggerDAG(m.confirmDagID, conf)
				dagID := m.confirmDagID
				m.confirmDagID = ""
				m.triggerConfInput = ""
				if err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("Triggered DAG %q → Run: %s", dagID, run.RunID)
					if dags, err := m.ds.ListDAGs(); err == nil {
						m.dagList.UpdateDAGs(dags)
					}
				}
				return m, clearStatusAfter(5 * time.Second)
			case "esc":
				m.triggerConfMode = false
				m.triggerConfInput = ""
				m.confirmDagID = ""
				m.statusMsg = "Trigger cancelled"
				return m, clearStatusAfter(3 * time.Second)
			case "backspace":
				if len(m.triggerConfInput) > 0 {
					m.triggerConfInput = m.triggerConfInput[:len(m.triggerConfInput)-1]
				}
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				if len(msg.String()) == 1 {
					m.triggerConfInput += msg.String()
				}
				return m, nil
			}
		}

		// Handle trigger confirmation dialog
		if m.confirmTrigger {
			switch msg.String() {
			case "y", "Y":
				m.confirmTrigger = false
				m.triggerConfMode = true
				m.triggerConfInput = ""
				return m, nil
			default:
				m.confirmTrigger = false
				m.confirmDagID = ""
				m.statusMsg = "Trigger cancelled"
				return m, clearStatusAfter(3 * time.Second)
			}
		}

		// Handle DAG Run mark state confirmation
		if m.confirmMarkState != "" {
			switch msg.String() {
			case "y", "Y":
				dagID := m.dagRunList.DagID()
				err := m.ds.SetDAGRunState(dagID, m.markRunID, m.confirmMarkState)
				state := m.confirmMarkState
				m.confirmMarkState = ""
				m.markRunID = ""
				if err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("DAG Run marked as %s", state)
					runs := m.ds.ListDAGRuns(dagID)
					m.dagRunList = NewDAGRunListModel(dagID, runs)
					m.dagRunList.SetSize(m.width)
				}
				return m, clearStatusAfter(5 * time.Second)
			default:
				m.confirmMarkState = ""
				m.markRunID = ""
				m.statusMsg = "Mark cancelled"
				return m, clearStatusAfter(3 * time.Second)
			}
		}

		// Handle clear task confirmation dialog
		if m.confirmClear || m.confirmClearAll {
			switch msg.String() {
			case "y", "Y":
				dagID := m.taskInstanceList.DagID()
				runID := m.taskInstanceList.RunID()
				var taskIDs []string
				label := "all tasks"
				if m.confirmClear {
					taskIDs = []string{m.clearTaskID}
					label = fmt.Sprintf("task %q", m.clearTaskID)
				}
				err := m.ds.ClearTaskInstances(dagID, runID, taskIDs)
				m.confirmClear = false
				m.confirmClearAll = false
				m.clearTaskID = ""
				if err != nil {
					m.statusMsg = fmt.Sprintf("Error: %v", err)
				} else {
					m.statusMsg = fmt.Sprintf("Cleared %s in %s/%s", label, dagID, runID)
					tasks := m.ds.ListTaskInstances(dagID, runID)
					m.taskInstanceList = NewTaskInstanceListModel(dagID, runID, tasks)
					m.taskInstanceList.SetSize(m.width)
				}
				return m, clearStatusAfter(5 * time.Second)
			default:
				m.confirmClear = false
				m.confirmClearAll = false
				m.clearTaskID = ""
				m.statusMsg = "Clear cancelled"
				return m, clearStatusAfter(3 * time.Second)
			}
		}

		// Handle overlay views (XCom, Graph, Source, Diagnose)
		if m.overlay != overlayNone {
			switch m.overlay {
			case overlayXCom:
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				case "esc", "backspace":
					m.overlay = overlayNone
					return m, nil
				case "up", "k":
					m.xcomView.CursorUp()
					return m, nil
				case "down", "j":
					m.xcomView.CursorDown()
					return m, nil
				}
			default: // graph, source, diagnose — scrollable viewport
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				case "esc", "backspace":
					m.overlay = overlayNone
					return m, nil
				}
				var cmd tea.Cmd
				m.overlayVP, cmd = m.overlayVP.Update(msg)
				return m, cmd
			}
			return m, nil
		}

		// In log view, let viewport handle scrolling but intercept navigation
		if m.tab == TabLogs {
			if m.logView.IsSearchMode() {
				var cmd tea.Cmd
				m.logView, cmd = m.logView.Update(msg)
				return m, cmd
			}
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc", "backspace", "shift+tab":
				m.tab = TabTaskInstances
				return m, nil
			case "r":
				return m, m.refresh()
			case "[":
				if m.logView.TryNumber() > 1 {
					newTry := m.logView.TryNumber() - 1
					log := m.ds.GetTaskLog(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					if IsLogEmpty(log) {
						log = FetchCloudLogs(m.logView.DagID(), m.logView.TaskID())
					}
					m.logView = NewLogViewModel(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), log, newTry, m.logView.MaxTry(), m.width, m.height-m.headerLines())
					m.statusMsg = fmt.Sprintf("Try %d/%d", newTry, m.logView.MaxTry())
					return m, clearStatusAfter(3 * time.Second)
				}
				return m, nil
			case "]":
				if m.logView.TryNumber() < m.logView.MaxTry() {
					newTry := m.logView.TryNumber() + 1
					log := m.ds.GetTaskLog(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					if IsLogEmpty(log) {
						log = FetchCloudLogs(m.logView.DagID(), m.logView.TaskID())
					}
					m.logView = NewLogViewModel(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), log, newTry, m.logView.MaxTry(), m.width, m.height-m.headerLines())
					m.statusMsg = fmt.Sprintf("Try %d/%d", newTry, m.logView.MaxTry())
					return m, clearStatusAfter(3 * time.Second)
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.logView, cmd = m.logView.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "up", "k":
			switch m.tab {
			case TabDAGs:
				m.dagList.CursorUp()
			case TabDAGRuns:
				m.dagRunList.CursorUp()
			case TabTaskInstances:
				m.taskInstanceList.CursorUp()
			}

		case "down", "j":
			switch m.tab {
			case TabDAGs:
				m.dagList.CursorDown()
			case TabDAGRuns:
				m.dagRunList.CursorDown()
			case TabTaskInstances:
				m.taskInstanceList.CursorDown()
			}

		case "enter":
			switch m.tab {
			case TabDAGs:
				if dag, ok := m.dagList.SelectedDAG(); ok {
					runs := m.ds.ListDAGRuns(dag.ID)
					m.dagRunList = NewDAGRunListModel(dag.ID, runs)
					m.dagRunList.SetSize(m.width)
					m.dagRunList.DetectFalseSuccess(m.ds.ListTaskInstances)
					m.tab = TabDAGRuns
				}
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					tasks := m.ds.ListTaskInstances(run.DagID, run.RunID)
					m.taskInstanceList = NewTaskInstanceListModel(run.DagID, run.RunID, tasks)
					m.taskInstanceList.SetSize(m.width)
					m.tab = TabTaskInstances
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					log := m.ds.GetTaskLog(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					if IsLogEmpty(log) {
						log = FetchCloudLogs(task.DagID, task.TaskID)
					}
					vpHeight := m.height - m.headerLines()
					if vpHeight < 1 {
						vpHeight = 1
					}
					m.logView = NewLogViewModel(task.DagID, task.RunID, task.TaskID, log, task.TryNumber, task.TryNumber, m.width, vpHeight)
					m.tab = TabLogs
				}
			}

		case "t":
			if m.tab == TabDAGs {
				if dag, ok := m.dagList.SelectedDAG(); ok {
					m.confirmTrigger = true
					m.confirmDagID = dag.ID
				}
			}

		case "p":
			if m.tab == TabDAGs {
				if m.dagList.HasSelection() {
					// Bulk pause/unpause
					selected := m.dagList.SelectedDAGs()
					var errCount int
					for _, dag := range selected {
						if _, err := m.ds.TogglePause(dag.ID); err != nil {
							errCount++
						}
					}
					m.dagList.ClearSelection()
					if dags, err := m.ds.ListDAGs(); err == nil {
						m.dagList.UpdateDAGs(dags)
					}
					if errCount > 0 {
						m.statusMsg = fmt.Sprintf("Toggled %d DAGs (%d errors)", len(selected), errCount)
					} else {
						m.statusMsg = fmt.Sprintf("Toggled %d DAGs", len(selected))
					}
					return m, clearStatusAfter(3 * time.Second)
				}
				if dag, ok := m.dagList.SelectedDAG(); ok {
					paused, err := m.ds.TogglePause(dag.ID)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
					} else {
						state := "active"
						if paused {
							state = "paused"
						}
						m.statusMsg = fmt.Sprintf("DAG %q is now %s", dag.ID, state)
						if dags, err := m.ds.ListDAGs(); err == nil {
							m.dagList.UpdateDAGs(dags)
						}
					}
					return m, clearStatusAfter(3 * time.Second)
				}
			}

		case " ":
			if m.tab == TabDAGs {
				m.dagList.ToggleSelect()
				m.dagList.CursorDown()
			}

		case "/":
			if m.tab == TabDAGs {
				m.filterMode = true
				m.filterInput = m.dagList.Filter()
			}

		case "tab":
			switch m.tab {
			case TabDAGs:
				if dag, ok := m.dagList.SelectedDAG(); ok {
					runs := m.ds.ListDAGRuns(dag.ID)
					m.dagRunList = NewDAGRunListModel(dag.ID, runs)
					m.dagRunList.SetSize(m.width)
					m.dagRunList.DetectFalseSuccess(m.ds.ListTaskInstances)
					m.tab = TabDAGRuns
				}
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					tasks := m.ds.ListTaskInstances(run.DagID, run.RunID)
					m.taskInstanceList = NewTaskInstanceListModel(run.DagID, run.RunID, tasks)
					m.taskInstanceList.SetSize(m.width)
					m.tab = TabTaskInstances
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					log := m.ds.GetTaskLog(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					if IsLogEmpty(log) {
						log = FetchCloudLogs(task.DagID, task.TaskID)
					}
					vpHeight := m.height - m.headerLines()
					if vpHeight < 1 {
						vpHeight = 1
					}
					m.logView = NewLogViewModel(task.DagID, task.RunID, task.TaskID, log, task.TryNumber, task.TryNumber, m.width, vpHeight)
					m.tab = TabLogs
				}
			}

		case "shift+tab":
			switch m.tab {
			case TabDAGRuns:
				m.tab = TabDAGs
			case TabTaskInstances:
				m.tab = TabDAGRuns
			case TabLogs:
				m.tab = TabTaskInstances
			}

		case "s":
			if m.tab == TabDAGRuns {
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.confirmMarkState = "success"
					m.markRunID = run.RunID
				}
			}

		case "f":
			if m.tab == TabDAGRuns {
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.confirmMarkState = "failed"
					m.markRunID = run.RunID
				}
			}

		case "c":
			if m.tab == TabTaskInstances {
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					m.confirmClear = true
					m.clearTaskID = task.TaskID
				}
			}

		case "C":
			if m.tab == TabTaskInstances {
				m.confirmClearAll = true
			}

		case "E":
			if m.cfg != nil && len(m.cfg.Environments) > 1 {
				m.envSelectMode = true
				envNames := m.cfg.EnvNames()
				for i, name := range envNames {
					if name == m.envName {
						m.envSelectIdx = i
						break
					}
				}
			}

		case "F":
			if m.tab == TabDAGs {
				m.dagList.ToggleFavorite()
			}

		case "x":
			if m.tab == TabTaskInstances {
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					entries := m.ds.GetXComEntries(task.DagID, task.RunID, task.TaskID)
					m.xcomView = NewXComViewModel(task.DagID, task.RunID, task.TaskID, entries)
					m.xcomView.SetSize(m.width)
					m.overlay = overlayXCom
				}
			}

		case "S":
			if m.tab == TabDAGs {
				if dag, ok := m.dagList.SelectedDAG(); ok {
					detail, err := m.ds.GetDAGDetail(dag.ID)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
						return m, clearStatusAfter(3 * time.Second)
					}
					source, err := m.ds.GetDAGSource(detail.FileToken)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
						return m, clearStatusAfter(3 * time.Second)
					}
					m.overlayTitle = fmt.Sprintf("Source: %s (%s)", dag.ID, detail.FileLoc)
					m.overlayContent = source
					vpH := m.height - 4
					if vpH < 1 {
						vpH = 1
					}
					m.overlayVP = viewport.New(m.width, vpH)
					m.overlayVP.SetContent(source)
					m.overlay = overlaySource
				}
			}

		case "G":
			if m.tab == TabDAGs {
				if dag, ok := m.dagList.SelectedDAG(); ok {
					detail, err := m.ds.GetDAGDetail(dag.ID)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
						return m, clearStatusAfter(3 * time.Second)
					}
					content := RenderDAGGraph(detail)
					m.overlayTitle = fmt.Sprintf("Graph: %s", dag.ID)
					m.overlayContent = content
					vpH := m.height - 4
					if vpH < 1 {
						vpH = 1
					}
					m.overlayVP = viewport.New(m.width, vpH)
					m.overlayVP.SetContent(content)
					m.overlay = overlayGraph
				}
			}

		case "I":
			if m.tab == TabDAGs {
				errors := m.ds.ListImportErrors()
				var content string
				if len(errors) == 0 {
					content = "No import errors found."
				} else {
					var sb strings.Builder
					for i, e := range errors {
						if i > 0 {
							sb.WriteString("\n" + strings.Repeat("─", 60) + "\n\n")
						}
						sb.WriteString(fmt.Sprintf("File: %s\n", e.Filename))
						sb.WriteString(fmt.Sprintf("Time: %s\n\n", e.Timestamp.Format("2006-01-02 15:04:05")))
						sb.WriteString(e.StackTrace)
						sb.WriteString("\n")
					}
					content = sb.String()
				}
				m.overlayTitle = fmt.Sprintf("Import Errors (%d)", len(errors))
				m.overlayContent = content
				vpH := m.height - 4
				if vpH < 1 {
					vpH = 1
				}
				m.overlayVP = viewport.New(m.width, vpH)
				m.overlayVP.SetContent(content)
				m.overlay = overlayImportErrors
			}

		case "d":
			if m.tab == TabDAGs {
				result := RunDiagnosis(m.ds)
				content := strings.Join(result.Lines, "\n")
				m.overlayTitle = "Diagnosis"
				m.overlayContent = content
				vpH := m.height - 4
				if vpH < 1 {
					vpH = 1
				}
				m.overlayVP = viewport.New(m.width, vpH)
				m.overlayVP.SetContent(content)
				m.overlay = overlayDiagnose
			}

		case "r":
			return m, m.refresh()

		case "esc", "backspace":
			switch m.tab {
			case TabDAGs:
				if m.dagList.IsFiltered() {
					m.dagList.ClearFilter()
					m.statusMsg = "Filter cleared"
					return m, clearStatusAfter(3 * time.Second)
				}
			case TabDAGRuns:
				m.tab = TabDAGs
			case TabTaskInstances:
				m.tab = TabDAGRuns
			}
		}
	}
	return m, nil
}

func (m *AppModel) refresh() tea.Cmd {
	switch m.tab {
	case TabDAGs:
		dags, err := m.ds.ListDAGs()
		if err != nil {
			m.statusMsg = fmt.Sprintf("Refresh failed: %v", err)
		} else {
			m.checkFailureNotifications(dags)
			m.dagList.UpdateDAGs(dags)
			m.statusMsg = "DAG list refreshed"
		}
	case TabDAGRuns:
		dagID := m.dagRunList.DagID()
		runs := m.ds.ListDAGRuns(dagID)
		m.dagRunList.UpdateRuns(runs)
		m.statusMsg = fmt.Sprintf("DAG Runs refreshed for %s", dagID)
	case TabTaskInstances:
		dagID := m.taskInstanceList.DagID()
		runID := m.taskInstanceList.RunID()
		tasks := m.ds.ListTaskInstances(dagID, runID)
		m.taskInstanceList.UpdateTasks(tasks)
		m.statusMsg = fmt.Sprintf("Task Instances refreshed for %s/%s", dagID, runID)
	case TabLogs:
		m.statusMsg = "Refreshed"
	}
	return clearStatusAfter(3 * time.Second)
}

func (m *AppModel) checkFailureNotifications(dags []model.DAG) {
	for _, dag := range dags {
		if dag.LastRunState == "failed" && !m.prevFailedDAGs[dag.ID] {
			sendDesktopNotification(fmt.Sprintf("DAG %q failed", dag.ID), m.envName)
		}
	}
	// Rebuild prev state
	m.prevFailedDAGs = map[string]bool{}
	for _, dag := range dags {
		if dag.LastRunState == "failed" {
			m.prevFailedDAGs[dag.ID] = true
		}
	}
}

func sendDesktopNotification(message, envName string) {
	title := "Gomposer"
	if envName != "" {
		title = fmt.Sprintf("Gomposer [%s]", envName)
	}
	// macOS notification via osascript (best-effort, fire-and-forget)
	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	exec.Command("osascript", "-e", script).Start() //nolint:errcheck
}

func (m *AppModel) failedDAGCount() int {
	count := 0
	for _, dag := range m.dagList.DAGs() {
		if dag.LastRunState == "failed" {
			count++
		}
	}
	return count
}

func (m AppModel) View() string {
	titleText := "Gomposer"
	if m.envName != "" {
		titleText = fmt.Sprintf("Gomposer [%s]", m.envName)
	}
	var title string
	failedCount := m.failedDAGCount()
	if failedCount > 0 {
		failedBadge := lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("196")).
			Background(GopherBlue).
			Render(fmt.Sprintf("  %d failed", failedCount))
		titleRendered := HeaderStyle.Render(titleText)
		title = lipgloss.NewStyle().
			Background(GopherBlue).
			Width(m.width).
			Render(titleRendered + failedBadge)
	} else {
		title = HeaderStyle.Width(m.width).Render(titleText)
	}
	tabs := "\n" + RenderTabs(m.tab)

	var subtitle string
	var content string

	if m.loading {
		content = statusStyle.Render("Loading DAGs...")
		parts := []string{title, tabs, content}
		help := RenderHelp([]HelpBinding{{Key: "q", Desc: "quit"}})
		parts = append(parts, "", help)
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}

	if m.loadError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).PaddingLeft(1)
		content = errStyle.Render(m.loadError)
		parts := []string{title, tabs, "", content}
		helpBindings := []HelpBinding{{Key: "r", Desc: "retry"}, {Key: "q", Desc: "quit"}}
		help := RenderHelp(helpBindings)
		parts = append(parts, "", help)
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}

	// Environment selector overlay
	if m.envSelectMode {
		return m.envSelectView(title, tabs)
	}

	// Overlay views
	if m.overlay != overlayNone {
		return m.overlayView(title, tabs)
	}

	switch m.tab {
	case TabDAGs:
		content = m.dagList.View()
	case TabDAGRuns:
		subtitle = HelpStyle.PaddingLeft(1).Render(fmt.Sprintf("DAG: %s", m.dagRunList.DagID()))
		content = m.dagRunList.View()
	case TabTaskInstances:
		subtitle = HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("DAG: %s  ›  Run: %s", m.taskInstanceList.DagID(), m.taskInstanceList.RunID()))
		content = m.taskInstanceList.View()
	case TabLogs:
		tryInfo := ""
		if m.logView.MaxTry() > 1 {
			tryInfo = fmt.Sprintf("  Try: %d/%d", m.logView.TryNumber(), m.logView.MaxTry())
		}
		subtitle = HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("DAG: %s  ›  Run: %s  ›  Task: %s%s  [%s]",
				m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), tryInfo, m.logView.ScrollInfo()))
		content = m.logView.View()
	}

	primary, secondary := m.helpRows()
	help := RenderHelpRows(primary, secondary, m.width)

	parts := []string{title, tabs}
	if subtitle != "" {
		parts = append(parts, subtitle, "")
	}
	parts = append(parts, content)

	// Status area: filter input, search input, confirmation dialog, or status message
	if m.logView.IsSearchMode() {
		parts = append(parts, "", filterInputStyle.Render(fmt.Sprintf("Search: %s▌", m.logView.SearchQuery())))
	} else if m.filterMode {
		parts = append(parts, "", filterInputStyle.Render(fmt.Sprintf("Filter: %s▌", m.filterInput)))
	} else if m.confirmMarkState != "" {
		parts = append(parts, "", confirmStyle.Render(
			fmt.Sprintf("Mark DAG Run as %s? [y/N]", m.confirmMarkState)))
	} else if m.confirmClear {
		parts = append(parts, "", confirmStyle.Render(
			fmt.Sprintf("Clear task %q and re-run? [y/N]", m.clearTaskID)))
	} else if m.confirmClearAll {
		parts = append(parts, "", confirmStyle.Render(
			fmt.Sprintf("Clear ALL tasks in %s/%s? [y/N]", m.taskInstanceList.DagID(), m.taskInstanceList.RunID())))
	} else if m.triggerConfMode {
		parts = append(parts, "", filterInputStyle.Render(
			fmt.Sprintf("Conf JSON (empty=none): %s▌", m.triggerConfInput)))
	} else if m.confirmTrigger {
		parts = append(parts, "", confirmStyle.Render(
			fmt.Sprintf("Trigger DAG %q? [y/N]", m.confirmDagID)))
	} else if m.statusMsg != "" {
		parts = append(parts, "", statusStyle.Render(m.statusMsg))
	} else {
		parts = append(parts, "")
	}

	parts = append(parts, help)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m AppModel) overlayView(title, tabs string) string {
	var content string
	var overlayHelp []HelpBinding

	switch m.overlay {
	case overlayXCom:
		subtitle := HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("XCom: %s / %s / %s", m.xcomView.dagID, m.xcomView.runID, m.xcomView.taskID))
		content = lipgloss.JoinVertical(lipgloss.Left, subtitle, m.xcomView.View())
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "navigate"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayGraph:
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlaySource:
		subtitle := HelpStyle.PaddingLeft(1).Render(m.overlayTitle)
		content = lipgloss.JoinVertical(lipgloss.Left, subtitle, m.overlayVP.View())
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "g/G", Desc: "top/bottom"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayDiagnose:
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayImportErrors:
		subtitle := HelpStyle.PaddingLeft(1).Render(m.overlayTitle)
		content = lipgloss.JoinVertical(lipgloss.Left, subtitle, m.overlayVP.View())
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	}

	help := RenderHelp(overlayHelp)
	parts := []string{title, tabs, content, "", help}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m AppModel) envSelectView(title, tabs string) string {
	envNames := m.cfg.EnvNames()
	header := confirmStyle.Render("Switch Environment:")
	var rows []string
	for i, name := range envNames {
		env := m.cfg.Environments[name]
		display := name
		if env.Name != "" && env.Name != name {
			display = fmt.Sprintf("%s (%s)", name, env.Name)
		}
		if name == m.envName {
			display += " (current)"
		}
		if i == m.envSelectIdx {
			rows = append(rows, SelectedRowStyle.Render("  > "+display))
		} else {
			rows = append(rows, "    "+display)
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, append([]string{header}, rows...)...)
	help := RenderHelp([]HelpBinding{
		{Key: "↑/↓", Desc: "select"},
		{Key: "enter", Desc: "switch"},
		{Key: "esc", Desc: "cancel"},
	})
	parts := []string{title, tabs, content, "", help}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// helpRows returns primary (always shown) and secondary (detail) help bindings.
func (m AppModel) helpRows() (primary, secondary []HelpBinding) {
	if m.filterMode {
		return []HelpBinding{
			{Key: "enter", Desc: "apply"},
			{Key: "esc", Desc: "clear"},
		}, nil
	}
	if m.triggerConfMode {
		return []HelpBinding{
			{Key: "enter", Desc: "trigger"},
			{Key: "esc", Desc: "cancel"},
		}, nil
	}
	if m.confirmClear || m.confirmClearAll || m.confirmMarkState != "" || m.confirmTrigger {
		return []HelpBinding{
			{Key: "y", Desc: "confirm"},
			{Key: "n/esc", Desc: "cancel"},
		}, nil
	}

	switch m.tab {
	case TabLogs:
		if m.logView.IsSearchMode() {
			return []HelpBinding{
				{Key: "enter", Desc: "search"},
				{Key: "esc", Desc: "clear"},
			}, nil
		}
		primary := []HelpBinding{
			{Key: "↑↓", Desc: "scroll"},
			{Key: "g/G", Desc: "top/bottom"},
			{Key: "/", Desc: "search"},
			{Key: "n/N", Desc: "next/prev"},
			{Key: "r", Desc: "refresh"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
		if m.logView.MaxTry() > 1 {
			primary = append(primary, HelpBinding{Key: "[/]", Desc: "prev/next try"})
		}
		return primary, nil

	default:
		primary = []HelpBinding{
			{Key: "↑↓", Desc: "move"},
			{Key: "enter", Desc: "open"},
		}

		switch m.tab {
		case TabDAGs:
			primary = append(primary,
				HelpBinding{Key: "/", Desc: "filter"},
				HelpBinding{Key: "t", Desc: "trigger"},
				HelpBinding{Key: "p", Desc: "pause"},
				HelpBinding{Key: "r", Desc: "refresh"},
				HelpBinding{Key: "q", Desc: "quit"},
			)
			secondary = []HelpBinding{
				{Key: "space", Desc: "select"},
				{Key: "F", Desc: "fav"},
				{Key: "S", Desc: "source"},
				{Key: "G", Desc: "graph"},
				{Key: "d", Desc: "diagnose"},
				{Key: "I", Desc: "import err"},
			}
			if m.cfg != nil && len(m.cfg.Environments) > 1 {
				secondary = append(secondary, HelpBinding{Key: "E", Desc: "env"})
			}
		case TabDAGRuns:
			primary = append(primary,
				HelpBinding{Key: "s/f", Desc: "mark"},
				HelpBinding{Key: "r", Desc: "refresh"},
				HelpBinding{Key: "esc", Desc: "back"},
				HelpBinding{Key: "q", Desc: "quit"},
			)
		case TabTaskInstances:
			primary = append(primary,
				HelpBinding{Key: "c", Desc: "clear"},
				HelpBinding{Key: "C", Desc: "clear all"},
				HelpBinding{Key: "r", Desc: "refresh"},
				HelpBinding{Key: "esc", Desc: "back"},
				HelpBinding{Key: "q", Desc: "quit"},
			)
			secondary = []HelpBinding{
				{Key: "x", Desc: "xcom"},
			}
		}
		return primary, secondary
	}
}
