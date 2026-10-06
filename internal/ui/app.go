package ui

import (
	"encoding/json"
	"fmt"
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
	overlayKeys
	overlayImportErrors
	overlayHistory
	overlayProblems
	overlayRunInfo
	overlayClearPreview
)

// clearDialog holds a pending clear: options are picked first, then a dry run shows what would be cleared.
type clearDialog struct {
	taskID  string // "" clears every task in the run
	opts    model.ClearOptions
	preview []string // task IDs from the dry run; nil until previewed
}

func (c *clearDialog) taskIDs() []string {
	if c.taskID == "" {
		return nil
	}
	return []string{c.taskID}
}

var statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).PaddingLeft(1)
var errorStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).PaddingLeft(1)

var confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).PaddingLeft(1)
var filterInputStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")).PaddingLeft(1)

// renderStatus labels the status line so it is not mistaken for part of the view above it.
// Errors are recognised by the prefixes the handlers already use.
func renderStatus(msg string) string {
	for _, p := range []string{"Error: ", "Refresh failed: ", "Invalid JSON: ", "(Cloud Logging"} {
		if strings.HasPrefix(msg, p) {
			return errorStatusStyle.Render("error: " + strings.TrimPrefix(msg, "Error: "))
		}
	}
	return statusStyle.Render("info: " + msg)
}

// clearStatusMsg is sent after a delay to clear the status message.
type clearStatusMsg struct{}

// dagsRefreshedMsg carries a background DAG list reload.
type dagsRefreshedMsg struct {
	dags    []model.DAG
	err     error
	envName string
	manual  bool
}

// runsLoadedMsg carries a DAG's runs fetched in the background, with false-success detection done.
// selectRunID moves the cursor to that run once it arrives.
type runsLoadedMsg struct {
	dagID        string
	runs         []model.DAGRun
	total        int
	falseSuccess map[string]bool
	selectRunID  string
}

// runsRefreshedMsg carries a background reload of the Runs tab.
type runsRefreshedMsg struct {
	dagID string
	runs  []model.DAGRun
	total int
}

// tasksRefreshedMsg carries a background reload of the Task Instances tab.
type tasksRefreshedMsg struct {
	dagID, runID string
	tasks        []model.TaskInstance
}

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
	clear            *clearDialog
	confirmMarkState string // "success" or "failed" — DAG Run manual mark
	markRunID        string
	confirmTaskState string // "success" or "failed" — task instance manual mark
	markTaskID       string
	refreshInterval  time.Duration // 0 = auto-refresh disabled
	filterMode       bool
	filterInput      string
	loading          bool
	loadingRuns      bool   // DAG runs are being fetched in the background
	loadError        string // API error message to display
	// Environment management
	cfg           *config.Config // nil in mock mode
	envName       string         // current environment key
	envSelectMode bool           // environment picker dialog
	envSelectIdx  int
	// Overlay views
	overlay        overlayMode
	xcomView       XComViewModel
	overlayVP      viewport.Model // used for graph, source, keys views
	overlayContent string
	overlayTitle   string
	history        TaskHistory
	historyWidth   int // width the history overlay was last rendered for
	historyCursor  int
	dagsFetching   bool  // a background DAG list fetch is in flight
	historyRows    []int // first content line of each history row
	problems       ProblemListModel
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
	next, cmd := m.update(msg)
	if am, ok := next.(AppModel); ok {
		am.layout()
		return am, cmd
	}
	return next, cmd
}

func (m AppModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		return m, nil

	case dagsRefreshedMsg:
		m.dagsFetching = false
		if msg.envName != m.envName {
			return m, nil // finished after an environment switch
		}
		if msg.err != nil {
			if msg.manual || m.tab == TabDAGs {
				m.statusMsg = fmt.Sprintf("Refresh failed: %v", msg.err)
				return m, clearStatusAfter(5 * time.Second)
			}
			return m, nil
		}
		m.dagList.UpdateDAGs(msg.dags)
		if msg.manual || m.tab == TabDAGs {
			m.statusMsg = "DAG list refreshed"
			return m, clearStatusAfter(5 * time.Second)
		}
		return m, nil

	case runsLoadedMsg:
		// Drop it if another DAG was opened meanwhile.
		if m.dagRunList.DagID() != msg.dagID {
			return m, nil
		}
		m.loadingRuns = false
		m.dagRunList = NewDAGRunListModel(msg.dagID, msg.runs, msg.total)
		m.dagRunList.SetSize(m.width)
		m.dagRunList.SetFalseSuccess(msg.falseSuccess)
		if msg.selectRunID != "" {
			m.dagRunList.SelectRun(msg.selectRunID)
		}
		return m, nil

	case runsRefreshedMsg:
		if m.loadingRuns || m.dagRunList.DagID() != msg.dagID {
			return m, nil
		}
		m.dagRunList.UpdateRuns(msg.runs)
		m.dagRunList.UpdateTotal(msg.total)
		return m, nil

	case tasksRefreshedMsg:
		if m.taskInstanceList.DagID() != msg.dagID || m.taskInstanceList.RunID() != msg.runID {
			return m, nil
		}
		m.taskInstanceList.UpdateTasks(msg.tasks)
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case autoRefreshMsg:
		// The DAG list refreshes on every tab so the title's failed count stays current.
		if m.tab == TabDAGs {
			cmd := tea.Batch(m.refresh(), m.autoRefreshTick())
			return m, cmd
		}
		cmd := tea.Batch(m.fetchDAGs(false), m.refresh(), m.autoRefreshTick())
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dagList.SetSize(msg.Width)
		m.dagRunList.SetSize(msg.Width)
		m.taskInstanceList.SetSize(msg.Width)
		m.problems.SetSize(msg.Width)
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
				return m, clearStatusAfter(5 * time.Second)
			case "esc":
				m.filterMode = false
				m.filterInput = ""
				m.dagList.ClearFilter()
				m.statusMsg = "Filter cleared"
				return m, clearStatusAfter(5 * time.Second)
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
					return m, tea.Batch(loadDAGs, clearStatusAfter(5*time.Second))
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
				}
				cmd := tea.Batch(m.fetchDAGs(false), clearStatusAfter(5*time.Second))
				return m, cmd
			case "esc":
				m.triggerConfMode = false
				m.triggerConfInput = ""
				m.confirmDagID = ""
				m.statusMsg = "Trigger cancelled"
				return m, clearStatusAfter(5 * time.Second)
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
				return m, clearStatusAfter(5 * time.Second)
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
					runs, total := m.ds.ListDAGRuns(dagID)
					m.dagRunList.UpdateRuns(runs)
					m.dagRunList.UpdateTotal(total)
				}
				return m, clearStatusAfter(5 * time.Second)
			default:
				m.confirmMarkState = ""
				m.markRunID = ""
				m.statusMsg = "Mark cancelled"
				return m, clearStatusAfter(5 * time.Second)
			}
		}

		// Handle task instance mark state confirmation
		if m.confirmTaskState != "" {
			state, taskID := m.confirmTaskState, m.markTaskID
			m.confirmTaskState, m.markTaskID = "", ""
			if msg.String() != "y" && msg.String() != "Y" {
				m.statusMsg = "Mark cancelled"
				return m, clearStatusAfter(5 * time.Second)
			}
			dagID, runID := m.taskInstanceList.DagID(), m.taskInstanceList.RunID()
			if err := m.ds.SetTaskInstanceState(dagID, runID, taskID, state); err != nil {
				m.statusMsg = fmt.Sprintf("Error: %v", err)
			} else {
				m.statusMsg = fmt.Sprintf("Task %q marked as %s", taskID, state)
				m.taskInstanceList.UpdateTasks(m.ds.ListTaskInstances(dagID, runID))
			}
			return m, clearStatusAfter(5 * time.Second)
		}

		// Handle clear options (the dry-run preview is an overlay, handled below)
		if m.clear != nil && m.overlay != overlayClearPreview {
			switch msg.String() {
			case "d":
				if m.clear.taskID != "" {
					m.clear.opts.IncludeDownstream = !m.clear.opts.IncludeDownstream
				}
			case "u":
				if m.clear.taskID != "" {
					m.clear.opts.IncludeUpstream = !m.clear.opts.IncludeUpstream
				}
			case "o":
				m.clear.opts.OnlyFailed = !m.clear.opts.OnlyFailed
			case "enter":
				return m, m.previewClear()
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "n", "N", "q":
				m.clear = nil
				m.statusMsg = "Clear cancelled"
				return m, clearStatusAfter(5 * time.Second)
			}
			return m, nil
		}

		// Handle overlay views (XCom, Graph, Source, Keys)
		if m.overlay != overlayNone {
			switch m.overlay {
			case overlayClearPreview:
				switch msg.String() {
				case "y", "Y":
					return m, m.runClear()
				case "up", "k", "down", "j", "pgup", "pgdown":
					var cmd tea.Cmd
					m.overlayVP, cmd = m.overlayVP.Update(msg)
					return m, cmd
				case "ctrl+c":
					return m, tea.Quit
				default:
					m.clear = nil
					m.overlay = overlayNone
					m.statusMsg = "Clear cancelled"
					return m, clearStatusAfter(5 * time.Second)
				}
			case overlayProblems:
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				case "esc", "backspace":
					m.overlay = overlayNone
				case "up", "k":
					m.problems.CursorUp()
				case "down", "j":
					m.problems.CursorDown()
				case "r":
					return m, m.openProblems()
				case "o":
					if t, ok := m.problems.Selected(); ok {
						return m, m.openInAirflow(t.DagID, t.RunID, t.TaskID, "logs")
					}
				case "enter":
					if t, ok := m.problems.Selected(); ok {
						m.overlay = overlayNone
						cmd := m.openRuns(t.DagID, t.RunID)
						m.openTasks(t.DagID, t.RunID)
						m.taskInstanceList.SelectTask(t.TaskID)
						return m, cmd
					}
				}
				return m, nil
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
			default: // graph, source, keys, history — scrollable viewport
				switch msg.String() {
				case "q", "ctrl+c":
					return m, tea.Quit
				case "esc", "backspace":
					m.overlay = overlayNone
					return m, nil
				case "L":
					if m.overlay == overlayHistory && m.history.ExplorerURL != "" {
						return m, m.copyURL(m.history.ExplorerURL)
					}
				case "up", "k", "down", "j":
					if m.overlay == overlayHistory && len(m.history.Rows) > 0 {
						if msg.String() == "up" || msg.String() == "k" {
							m.historyCursor = max(m.historyCursor-1, 0)
						} else {
							m.historyCursor = min(m.historyCursor+1, len(m.history.Rows)-1)
						}
						m.renderHistory()
						m.scrollHistoryToCursor()
						return m, nil
					}
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
				cmd := m.refresh()
				return m, cmd
			case "o":
				return m, m.openInAirflow(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), "logs")
			case "L":
				project := m.gcpProject()
				if project == "" {
					m.statusMsg = missingProjectMsg(m.envName)
					return m, clearStatusAfter(5 * time.Second)
				}
				execDate := ExecutionDateFromRunID(m.logView.RunID())
				start := time.Now().AddDate(0, 0, -7)
				if t, err := time.Parse(time.RFC3339Nano, execDate); err == nil {
					start = t
				}
				filter := TaskLogFilter(m.logView.DagID(), m.logView.TaskID(), execDate, m.logView.TryNumber())
				return m, m.copyURL(LogsExplorerURL(project, filter, start, time.Now()))
			case "[":
				if m.logView.TryNumber() > 1 {
					newTry := m.logView.TryNumber() - 1
					log := m.ds.GetTaskLog(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					if IsLogEmpty(log) {
						log = m.fetchCloudLogs(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					}
					m.logView = NewLogViewModel(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), log, newTry, m.logView.MaxTry(), m.width, m.contentHeight())
					m.statusMsg = fmt.Sprintf("Try %d/%d", newTry, m.logView.MaxTry())
					return m, clearStatusAfter(5 * time.Second)
				}
				return m, nil
			case "]":
				if m.logView.TryNumber() < m.logView.MaxTry() {
					newTry := m.logView.TryNumber() + 1
					log := m.ds.GetTaskLog(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					if IsLogEmpty(log) {
						log = m.fetchCloudLogs(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), newTry)
					}
					m.logView = NewLogViewModel(m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), log, newTry, m.logView.MaxTry(), m.width, m.contentHeight())
					m.statusMsg = fmt.Sprintf("Try %d/%d", newTry, m.logView.MaxTry())
					return m, clearStatusAfter(5 * time.Second)
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
					return m, m.openRuns(dag.ID, "")
				}
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.openTasks(run.DagID, run.RunID)
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					log := m.ds.GetTaskLog(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					if IsLogEmpty(log) {
						log = m.fetchCloudLogs(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					}
					vpHeight := m.contentHeight()
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
					if errCount > 0 {
						m.statusMsg = fmt.Sprintf("Toggled %d DAGs (%d errors)", len(selected), errCount)
					} else {
						m.statusMsg = fmt.Sprintf("Toggled %d DAGs", len(selected))
					}
					cmd := tea.Batch(m.fetchDAGs(false), clearStatusAfter(5*time.Second))
					return m, cmd
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
					}
					cmd := tea.Batch(m.fetchDAGs(false), clearStatusAfter(5*time.Second))
					return m, cmd
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
					return m, m.openRuns(dag.ID, "")
				}
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.openTasks(run.DagID, run.RunID)
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					log := m.ds.GetTaskLog(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					if IsLogEmpty(log) {
						log = m.fetchCloudLogs(task.DagID, task.RunID, task.TaskID, task.TryNumber)
					}
					vpHeight := m.contentHeight()
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

		case "s", "f":
			state := map[string]string{"s": "success", "f": "failed"}[msg.String()]
			switch m.tab {
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.confirmMarkState = state
					m.markRunID = run.RunID
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					m.confirmTaskState = state
					m.markTaskID = task.TaskID
				}
			}

		case "c":
			if m.tab == TabTaskInstances {
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					// Clearing only the root-cause task leaves its downstream upstream_failed, so downstream starts on.
					m.clear = &clearDialog{taskID: task.TaskID, opts: model.ClearOptions{IncludeDownstream: true}}
				}
			}

		case "C":
			if m.tab == TabTaskInstances {
				m.clear = &clearDialog{}
			}

		case "i":
			if m.tab == TabDAGRuns {
				if run, ok := m.dagRunList.SelectedRun(); ok {
					m.showText(overlayRunInfo, fmt.Sprintf("Run: %s › %s", run.DagID, run.RunID), RunInfo(run))
				}
			}

		case "!":
			return m, m.openProblems()

		case "o":
			switch m.tab {
			case TabDAGs:
				if dag, ok := m.dagList.SelectedDAG(); ok {
					return m, m.openInAirflow(dag.ID, "", "", "graph")
				}
			case TabDAGRuns:
				if run, ok := m.dagRunList.SelectedRun(); ok {
					return m, m.openInAirflow(run.DagID, run.RunID, "", "graph")
				}
			case TabTaskInstances:
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					return m, m.openInAirflow(task.DagID, task.RunID, task.TaskID, "")
				}
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

		case "H":
			if m.tab == TabTaskInstances {
				if task, ok := m.taskInstanceList.SelectedTask(); ok {
					m.history = FetchTaskHistory(m.gcpProject(), m.envName, task.DagID, task.TaskID, time.Now())
					m.historyCursor = 0
					m.overlayTitle = fmt.Sprintf("History: %s › %s", task.DagID, task.TaskID)
					m.overlayVP = viewport.New(m.width, m.contentHeight())
					m.overlay = overlayHistory
					m.renderHistory()
				}
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
						return m, clearStatusAfter(5 * time.Second)
					}
					source, err := m.ds.GetDAGSource(detail.FileToken)
					if err != nil {
						m.statusMsg = fmt.Sprintf("Error: %v", err)
						return m, clearStatusAfter(5 * time.Second)
					}
					m.overlayTitle = fmt.Sprintf("Source: %s (%s)", dag.ID, detail.FileLoc)
					m.overlayContent = source
					vpH := m.contentHeight()
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
						return m, clearStatusAfter(5 * time.Second)
					}
					content := RenderDAGGraph(detail)
					m.overlayTitle = fmt.Sprintf("Graph: %s", dag.ID)
					m.overlayContent = content
					vpH := m.contentHeight()
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
						fmt.Fprintf(&sb, "File: %s\n", e.Filename)
						fmt.Fprintf(&sb, "Time: %s\n\n", e.Timestamp.Format("2006-01-02 15:04:05"))
						sb.WriteString(e.StackTrace)
						sb.WriteString("\n")
					}
					content = sb.String()
				}
				m.overlayTitle = fmt.Sprintf("Import Errors (%d)", len(errors))
				m.overlayContent = content
				vpH := m.contentHeight()
				if vpH < 1 {
					vpH = 1
				}
				m.overlayVP = viewport.New(m.width, vpH)
				m.overlayVP.SetContent(content)
				m.overlay = overlayImportErrors
			}

		case "?":
			if m.tab == TabDAGs {
				m.showText(overlayKeys, "Keys: DAGs", renderKeyList(append(dagHelp(m.cfg), dagMoreHelp...)))
			}

		case "r":
			cmd := m.refresh()
			return m, cmd

		case "esc", "backspace":
			switch m.tab {
			case TabDAGs:
				if m.dagList.IsFiltered() {
					m.dagList.ClearFilter()
					m.statusMsg = "Filter cleared"
					return m, clearStatusAfter(5 * time.Second)
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

// gcpProject returns the Cloud Logging project of the current environment, or "" if unset.
func (m *AppModel) gcpProject() string {
	if m.cfg == nil {
		return ""
	}
	return m.cfg.Environments[m.envName].GCPProject
}

func (m *AppModel) fetchCloudLogs(dagID, runID, taskID string, tryNumber int) string {
	return FetchCloudLogs(m.gcpProject(), m.envName, dagID, runID, taskID, tryNumber)
}

func (m *AppModel) copyURL(u string) tea.Cmd {
	if err := copyToClipboard(u); err != nil {
		m.statusMsg = u
	} else {
		m.statusMsg = "Copied Logs Explorer URL"
	}
	return clearStatusAfter(5 * time.Second)
}

// openRuns switches to the Runs tab and loads it off the UI loop: false-success detection is one request per run.
// selectRunID, if set, is selected once the runs arrive.
func (m *AppModel) openRuns(dagID, selectRunID string) tea.Cmd {
	m.dagRunList = NewDAGRunListModel(dagID, nil, 0)
	m.dagRunList.SetSize(m.width)
	m.loadingRuns = true
	m.tab = TabDAGRuns
	ds := m.ds
	return func() tea.Msg {
		runs, total := ds.ListDAGRuns(dagID)
		l := NewDAGRunListModel(dagID, runs, total)
		l.DetectFalseSuccess(ds.ListTaskInstances)
		return runsLoadedMsg{dagID: dagID, runs: runs, total: total, falseSuccess: l.falseSuccess, selectRunID: selectRunID}
	}
}

func (m *AppModel) openTasks(dagID, runID string) {
	tasks := m.ds.ListTaskInstances(dagID, runID)
	m.taskInstanceList = NewTaskInstanceListModel(dagID, runID, tasks)
	m.taskInstanceList.SetSize(m.width)
	m.tab = TabTaskInstances
}

// showText opens a scrollable read-only overlay.
func (m *AppModel) showText(mode overlayMode, title, content string) {
	m.overlayTitle = title
	m.overlayContent = content
	m.overlayVP = viewport.New(m.width, m.contentHeight())
	m.overlayVP.SetContent(content)
	m.overlay = mode
}

func (m *AppModel) openProblems() tea.Cmd {
	since := time.Now().Add(-problemWindow)
	tasks, err := m.ds.ListProblemTaskInstances(since)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Error: %v", err)
		return clearStatusAfter(5 * time.Second)
	}
	m.problems = NewProblemListModel(tasks, since)
	m.problems.SetSize(m.width)
	m.overlay = overlayProblems
	return nil
}

func (m *AppModel) previewClear() tea.Cmd {
	dagID, runID := m.taskInstanceList.DagID(), m.taskInstanceList.RunID()
	opts := m.clear.opts
	opts.DryRun = true
	ids, err := m.ds.ClearTaskInstances(dagID, runID, m.clear.taskIDs(), opts)
	if err != nil {
		m.clear = nil
		m.statusMsg = fmt.Sprintf("Error: %v", err)
		return clearStatusAfter(5 * time.Second)
	}
	if len(ids) == 0 {
		m.statusMsg = "Nothing to clear with these options"
		return clearStatusAfter(5 * time.Second)
	}
	states := map[string]string{}
	for _, t := range m.taskInstanceList.tasks {
		states[t.TaskID] = t.State
	}
	var sb strings.Builder
	for _, id := range ids {
		state := states[id]
		if state == "" {
			state = "-"
		}
		fmt.Fprintf(&sb, "  %s  %s\n", StateStyle(state).Render(fmt.Sprintf("%-16s", state)), id)
	}
	m.clear.preview = ids
	m.showText(overlayClearPreview, fmt.Sprintf("Clear preview: %s › %s — %d task instance(s) will be reset", dagID, runID, len(ids)), sb.String())
	return nil
}

func (m *AppModel) runClear() tea.Cmd {
	dagID, runID := m.taskInstanceList.DagID(), m.taskInstanceList.RunID()
	c := m.clear
	m.clear = nil
	m.overlay = overlayNone
	ids, err := m.ds.ClearTaskInstances(dagID, runID, c.taskIDs(), c.opts)
	if err != nil {
		m.statusMsg = fmt.Sprintf("Error: %v", err)
	} else {
		m.statusMsg = fmt.Sprintf("Cleared %d task instance(s) in %s/%s", len(ids), dagID, runID)
		m.taskInstanceList.UpdateTasks(m.ds.ListTaskInstances(dagID, runID))
	}
	return clearStatusAfter(5 * time.Second)
}

func (m *AppModel) openInAirflow(dagID, runID, taskID, tab string) tea.Cmd {
	base := ""
	if m.cfg != nil {
		base = m.cfg.Environments[m.envName].WebserverURL
	}
	if base == "" {
		m.statusMsg = "Error: no webserver_url for this environment"
		return clearStatusAfter(5 * time.Second)
	}
	u := AirflowGridURL(base, dagID, runID, taskID, tab)
	if err := openBrowser(u); err != nil {
		m.statusMsg = u
	} else {
		m.statusMsg = "Opened in Airflow: " + dagID
	}
	return clearStatusAfter(5 * time.Second)
}

func (m *AppModel) refresh() tea.Cmd {
	switch m.tab {
	case TabDAGs:
		return m.fetchDAGs(true)
	case TabDAGRuns:
		if m.loadingRuns {
			return nil
		}
		ds, dagID := m.ds, m.dagRunList.DagID()
		m.statusMsg = fmt.Sprintf("DAG Runs refreshed for %s", dagID)
		return tea.Batch(func() tea.Msg {
			runs, total := ds.ListDAGRuns(dagID)
			return runsRefreshedMsg{dagID: dagID, runs: runs, total: total}
		}, clearStatusAfter(5*time.Second))
	case TabTaskInstances:
		ds, dagID, runID := m.ds, m.taskInstanceList.DagID(), m.taskInstanceList.RunID()
		m.statusMsg = fmt.Sprintf("Task Instances refreshed for %s/%s", dagID, runID)
		return tea.Batch(func() tea.Msg {
			return tasksRefreshedMsg{dagID: dagID, runID: runID, tasks: ds.ListTaskInstances(dagID, runID)}
		}, clearStatusAfter(5*time.Second))
	case TabLogs:
		m.statusMsg = "Refreshed"
	}
	return clearStatusAfter(5 * time.Second)
}

// fetchDAGs reloads the DAG list off the UI goroutine; it is one request per DAG, too slow to block on.
// manual reports the result in the status line. Returns nil while a previous fetch is still running.
func (m *AppModel) fetchDAGs(manual bool) tea.Cmd {
	if m.dagsFetching {
		return nil
	}
	m.dagsFetching = true
	ds, envName := m.ds, m.envName
	return func() tea.Msg {
		dags, err := ds.ListDAGs()
		return dagsRefreshedMsg{dags: dags, err: err, envName: envName, manual: manual}
	}
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
	title := m.renderTitle()
	tabs := "\n" + RenderTabs(m.tab)

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

	top, bottom := m.chrome()
	body := fitBody(m.body(), m.width, m.bodyHeight(top, bottom))
	view := lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
	if m.height > 0 {
		// In a pane shorter than the chrome, drop the bottom so the terminal does not scroll the title away.
		view = lipgloss.NewStyle().MaxHeight(m.height).Render(view)
	}
	return view
}

func (m AppModel) renderTitle() string {
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
	return title
}

// chrome renders what stays pinned regardless of body length:
// title, tabs and subtitle on top; status line and help at the bottom.
func (m AppModel) chrome() (top, bottom string) {
	title := m.renderTitle()
	tabs := "\n" + RenderTabs(m.tab)

	if m.overlay != overlayNone {
		_, overlayHelp := m.overlayParts()
		return lipgloss.JoinVertical(lipgloss.Left, title, tabs),
			lipgloss.JoinVertical(lipgloss.Left, "", m.statusLine(), RenderHelpRows(overlayHelp, nil, m.width))
	}

	parts := []string{title, tabs}
	if subtitle := m.subtitle(); subtitle != "" {
		parts = append(parts, subtitle, "")
	}
	primary, secondary := m.helpRows()
	return lipgloss.JoinVertical(lipgloss.Left, parts...),
		lipgloss.JoinVertical(lipgloss.Left, "", m.statusLine(), RenderHelpRows(primary, secondary, m.width))
}

// bodyHeight is the number of lines left between the pinned top and bottom. 0 means the size is unknown yet.
func (m AppModel) bodyHeight(top, bottom string) int {
	if m.height <= 0 {
		return 0
	}
	h := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	if h < 1 {
		h = 1
	}
	return h
}

func (m AppModel) contentHeight() int {
	top, bottom := m.chrome()
	if h := m.bodyHeight(top, bottom); h > 0 {
		return h
	}
	return 1
}

// layout resizes every scrollable view to the current body height.
func (m *AppModel) layout() {
	if m.height <= 0 {
		return
	}
	h := m.contentHeight()
	m.dagList.SetHeight(h)
	m.dagRunList.SetHeight(h)
	m.taskInstanceList.SetHeight(h)
	m.logView.SetSize(m.width, h)

	vpH := h
	if subtitle := m.overlaySubtitle(); subtitle != "" {
		vpH -= lipgloss.Height(subtitle)
	}
	if vpH < 1 {
		vpH = 1
	}
	m.xcomView.SetHeight(vpH)
	m.problems.SetHeight(vpH)
	m.overlayVP.Width = m.width
	m.overlayVP.Height = vpH
	if m.overlay == overlayHistory && m.historyWidth != m.width {
		m.renderHistory()
		m.scrollHistoryToCursor()
	}
}

func (m *AppModel) renderHistory() {
	m.historyWidth = m.width
	m.overlayContent, m.historyRows = m.history.Render(m.width, m.historyCursor)
	m.overlayVP.SetContent(m.overlayContent)
}

// scrollHistoryToCursor moves the viewport just enough to show the whole cursor row, which may span wrapped lines.
func (m *AppModel) scrollHistoryToCursor() {
	if m.historyCursor >= len(m.historyRows) {
		return
	}
	start := m.historyRows[m.historyCursor]
	end := strings.Count(m.overlayContent, "\n")
	if m.historyCursor+1 < len(m.historyRows) {
		end = m.historyRows[m.historyCursor+1]
	}
	switch {
	case start < m.overlayVP.YOffset:
		m.overlayVP.SetYOffset(start)
	case end > m.overlayVP.YOffset+m.overlayVP.Height:
		m.overlayVP.SetYOffset(min(end-m.overlayVP.Height, start))
	}
}

func (m AppModel) body() string {
	if m.overlay != overlayNone {
		content, _ := m.overlayParts()
		return content
	}
	switch m.tab {
	case TabDAGs:
		return m.dagList.View()
	case TabDAGRuns:
		if m.loadingRuns {
			return statusStyle.Render("Loading runs...")
		}
		return m.dagRunList.View()
	case TabTaskInstances:
		return m.taskInstanceList.View()
	case TabLogs:
		return m.logView.View()
	}
	return ""
}

func (m AppModel) subtitle() string {
	switch m.tab {
	case TabDAGs:
		if f := m.dagList.FilterSummary(); f != "" {
			return HelpStyle.PaddingLeft(1).Render(f)
		}
	case TabDAGRuns:
		return HelpStyle.PaddingLeft(1).Render(fmt.Sprintf("DAG ID: %s  ·  %s", m.dagRunList.DagID(), m.dagRunList.CountLabel()))
	case TabTaskInstances:
		return HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("DAG ID: %s  ›  Run ID: %s", m.taskInstanceList.DagID(), m.taskInstanceList.RunID()))
	case TabLogs:
		tryInfo := ""
		if m.logView.MaxTry() > 1 {
			tryInfo = fmt.Sprintf("  Try: %d/%d", m.logView.TryNumber(), m.logView.MaxTry())
		}
		return HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("DAG ID: %s  ›  Run ID: %s  ›  Task ID: %s%s  [%s]",
				m.logView.DagID(), m.logView.RunID(), m.logView.TaskID(), tryInfo, m.logView.ScrollInfo()))
	}

	return ""
}

func (m AppModel) statusLine() string {
	if m.logView.IsSearchMode() {
		return filterInputStyle.Render(fmt.Sprintf("Search: %s▌", m.logView.SearchQuery()))
	} else if m.filterMode {
		return filterInputStyle.Render(fmt.Sprintf("Filter: %s▌", m.filterInput))
	} else if m.confirmMarkState != "" {
		return confirmStyle.Render(
			fmt.Sprintf("Mark DAG Run as %s? [y/N]", m.confirmMarkState))
	} else if m.confirmTaskState != "" {
		return confirmStyle.Render(
			fmt.Sprintf("Mark task %q as %s? [y/N]", m.markTaskID, m.confirmTaskState))
	} else if m.clear != nil && m.clear.preview != nil {
		return confirmStyle.Render(fmt.Sprintf("Clear these %d task instance(s)? [y/N]", len(m.clear.preview)))
	} else if m.clear != nil {
		return confirmStyle.Render(m.clearOptionsLine())
	} else if m.triggerConfMode {
		return filterInputStyle.Render(
			fmt.Sprintf("Conf JSON (empty=none): %s▌", m.triggerConfInput))
	} else if m.confirmTrigger {
		return confirmStyle.Render(
			fmt.Sprintf("Trigger DAG %q? [y/N]", m.confirmDagID))
	} else if m.statusMsg != "" {
		return renderStatus(m.statusMsg)
	} else {
		return ""
	}

}

func (m AppModel) clearOptionsLine() string {
	mark := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	o := m.clear.opts
	if m.clear.taskID == "" {
		return fmt.Sprintf("Clear ALL tasks in %s/%s  [o]nly failed:%s  — enter to preview",
			m.taskInstanceList.DagID(), m.taskInstanceList.RunID(), mark(o.OnlyFailed))
	}
	return fmt.Sprintf("Clear %q  [d]ownstream:%s [u]pstream:%s [o]nly failed:%s  — enter to preview",
		m.clear.taskID, mark(o.IncludeDownstream), mark(o.IncludeUpstream), mark(o.OnlyFailed))
}

func (m AppModel) overlayParts() (content string, overlayHelp []HelpBinding) {
	switch m.overlay {
	case overlayXCom:
		content = m.xcomView.View()
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
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "g/G", Desc: "top/bottom"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayHistory:
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "move"},
			{Key: "L", Desc: "copy Logs Explorer URL"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayImportErrors, overlayRunInfo, overlayKeys:
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "esc", Desc: "back"},
			{Key: "q", Desc: "quit"},
		}
	case overlayClearPreview:
		content = m.overlayVP.View()
		overlayHelp = []HelpBinding{
			{Key: "y", Desc: "clear"},
			{Key: "↑/↓", Desc: "scroll"},
			{Key: "esc", Desc: "cancel"},
		}
	case overlayProblems:
		content = m.problems.View()
		// Same keys as the tabs' global row, so reuse it to keep wording and order identical.
		overlayHelp = globalHelp
	}

	if subtitle := m.overlaySubtitle(); subtitle != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, subtitle, content)
	}
	return content, overlayHelp
}

// overlaySubtitle is the dim heading above an overlay's body; empty for overlays without one.
func (m AppModel) overlaySubtitle() string {
	switch m.overlay {
	case overlayXCom:
		return HelpStyle.PaddingLeft(1).Render(
			fmt.Sprintf("XCom: %s / %s / %s", m.xcomView.dagID, m.xcomView.runID, m.xcomView.taskID))
	case overlaySource, overlayImportErrors, overlayRunInfo, overlayClearPreview, overlayKeys:
		return HelpStyle.PaddingLeft(1).Render(m.overlayTitle)
	case overlayProblems:
		return HelpStyle.PaddingLeft(1).Render(fmt.Sprintf("Problems since %s  ·  %s",
			m.problems.since.Local().Format("2006-01-02 15:04"), m.problems.Summary()))
	case overlayHistory:
		lines := m.overlayTitle
		if m.history.Summary != "" {
			lines += "\n" + m.history.Summary
		}
		heading := HelpStyle.PaddingLeft(1).Render(lines) + "\n"
		if header := m.history.Header(); header != "" {
			heading += "\n" + header
		}
		return heading
	}
	return ""
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

// globalHelp is pinned as the second help row on every tab so the shared keys never move.
var globalHelp = []HelpBinding{
	{Key: "↑↓", Desc: "move"},
	{Key: "enter", Desc: "open"},
	{Key: "r", Desc: "refresh"},
	{Key: "esc", Desc: "back"},
	{Key: "q", Desc: "quit"},
	{Key: "o", Desc: "open in Airflow"},
}

// helpRows returns tab-specific bindings and the global row; modal prompts return only their own keys.
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
	if m.clear != nil && m.clear.preview == nil {
		if m.clear.taskID == "" {
			return []HelpBinding{{Key: "o", Desc: "only failed"}, {Key: "enter", Desc: "preview"}, {Key: "esc", Desc: "cancel"}}, nil
		}
		return []HelpBinding{
			{Key: "d/u/o", Desc: "toggle"},
			{Key: "enter", Desc: "preview"},
			{Key: "esc", Desc: "cancel"},
		}, nil
	}
	if m.confirmTaskState != "" || m.confirmMarkState != "" || m.confirmTrigger {
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
		primary = []HelpBinding{
			{Key: "g/G", Desc: "top/bottom"},
			{Key: "/", Desc: "search"},
			{Key: "n/N", Desc: "next/prev"},
			{Key: "L", Desc: "logs URL"},
		}
		if m.logView.MaxTry() > 1 {
			primary = append(primary, HelpBinding{Key: "[/]", Desc: "prev/next try"})
		}
	case TabDAGs:
		primary = append(dagHelp(m.cfg), HelpBinding{Key: "?", Desc: "more"})
	case TabDAGRuns:
		primary = []HelpBinding{
			{Key: "s/f", Desc: "mark"},
			{Key: "i", Desc: "info/conf"},
			{Key: "!", Desc: "problems"},
		}
	case TabTaskInstances:
		primary = []HelpBinding{
			{Key: "c", Desc: "clear"},
			{Key: "C", Desc: "clear all"},
			{Key: "s/f", Desc: "mark"},
			{Key: "x", Desc: "xcom"},
			{Key: "H", Desc: "history"},
			{Key: "!", Desc: "problems"},
		}
	}
	return primary, globalHelp
}

// dagHelp is the DAGs tab's help row; rarely used keys live in dagMoreHelp and show only under "?".
func dagHelp(cfg *config.Config) []HelpBinding {
	b := []HelpBinding{
		{Key: "/", Desc: "filter"},
		{Key: "t", Desc: "trigger"},
		{Key: "p", Desc: "pause"},
		{Key: "F", Desc: "fav"},
		{Key: "!", Desc: "problems"},
		{Key: "I", Desc: "import err"},
	}
	if cfg != nil && len(cfg.Environments) > 1 {
		b = append(b, HelpBinding{Key: "E", Desc: "env"})
	}
	return b
}

var dagMoreHelp = []HelpBinding{
	{Key: "space", Desc: "select (for bulk pause)"},
	{Key: "S", Desc: "source"},
	{Key: "G", Desc: "graph"},
}

// renderKeyList lays out bindings one per line with the keys aligned, followed by the global keys.
func renderKeyList(tab []HelpBinding) string {
	all := append(append([]HelpBinding{}, tab...), globalHelp...)
	w := 0
	for _, b := range all {
		w = max(w, lipgloss.Width(b.Key))
	}
	var sb strings.Builder
	for i, b := range all {
		if i == len(tab) {
			sb.WriteString("\n")
		}
		pad := strings.Repeat(" ", w-lipgloss.Width(b.Key))
		sb.WriteString("  " + helpKeyStyle.Render(b.Key) + pad + "  " + b.Desc + "\n")
	}
	return sb.String()
}
