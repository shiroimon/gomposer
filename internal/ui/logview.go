package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var logHighlightStyle = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("214")).Foreground(lipgloss.Color("0"))

type LogViewModel struct {
	dagID      string
	runID      string
	taskID     string
	tryNumber  int // current try number being viewed
	maxTry     int // max try number for this task
	viewport   viewport.Model
	ready      bool
	rawContent string
	// search
	searchMode  bool
	searchQuery string
	matchLines  []int // 0-based line indices with matches
	matchIdx    int   // current match index
}

func NewLogViewModel(dagID, runID, taskID, content string, tryNumber, maxTry, width, height int) LogViewModel {
	vp := viewport.New(width, height)
	vp.SetContent(content)
	return LogViewModel{
		dagID:      dagID,
		runID:      runID,
		taskID:     taskID,
		tryNumber:  tryNumber,
		maxTry:     maxTry,
		viewport:   vp,
		ready:      true,
		rawContent: content,
	}
}

func (m *LogViewModel) SetSize(w, h int) {
	m.viewport.Width = w
	m.viewport.Height = h
}

func (m LogViewModel) Update(msg tea.Msg) (LogViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.searchMode {
			switch msg.String() {
			case "enter":
				m.searchMode = false
				m.applySearch()
				return m, nil
			case "esc":
				m.searchMode = false
				m.searchQuery = ""
				m.clearSearch()
				return m, nil
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
				return m, nil
			case "ctrl+c":
				return m, tea.Quit
			default:
				if len(msg.String()) == 1 {
					m.searchQuery += msg.String()
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "G":
			m.viewport.GotoBottom()
			return m, nil
		case "g":
			m.viewport.GotoTop()
			return m, nil
		case "/":
			m.searchMode = true
			return m, nil
		case "n":
			m.nextMatch()
			return m, nil
		case "N":
			m.prevMatch()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *LogViewModel) applySearch() {
	if m.searchQuery == "" {
		m.clearSearch()
		return
	}
	lower := strings.ToLower(m.searchQuery)
	lines := strings.Split(m.rawContent, "\n")
	m.matchLines = nil
	var highlighted []string
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), lower) {
			m.matchLines = append(m.matchLines, i)
			highlighted = append(highlighted, highlightLine(line, m.searchQuery))
		} else {
			highlighted = append(highlighted, line)
		}
	}
	m.viewport.SetContent(strings.Join(highlighted, "\n"))
	m.matchIdx = 0
	if len(m.matchLines) > 0 {
		m.viewport.SetYOffset(m.matchLines[0])
	}
}

func (m *LogViewModel) clearSearch() {
	m.matchLines = nil
	m.matchIdx = 0
	m.viewport.SetContent(m.rawContent)
}

func (m *LogViewModel) nextMatch() {
	if len(m.matchLines) == 0 {
		return
	}
	m.matchIdx = (m.matchIdx + 1) % len(m.matchLines)
	m.viewport.SetYOffset(m.matchLines[m.matchIdx])
}

func (m *LogViewModel) prevMatch() {
	if len(m.matchLines) == 0 {
		return
	}
	m.matchIdx--
	if m.matchIdx < 0 {
		m.matchIdx = len(m.matchLines) - 1
	}
	m.viewport.SetYOffset(m.matchLines[m.matchIdx])
}

func highlightLine(line, query string) string {
	lower := strings.ToLower(line)
	lowerQ := strings.ToLower(query)
	idx := strings.Index(lower, lowerQ)
	if idx < 0 {
		return line
	}
	// Highlight all occurrences
	var result strings.Builder
	for {
		idx = strings.Index(strings.ToLower(line), lowerQ)
		if idx < 0 {
			result.WriteString(line)
			break
		}
		result.WriteString(line[:idx])
		result.WriteString(logHighlightStyle.Render(line[idx : idx+len(query)]))
		line = line[idx+len(query):]
	}
	return result.String()
}

func (m *LogViewModel) View() string {
	return m.viewport.View()
}

func (m *LogViewModel) DagID() string     { return m.dagID }
func (m *LogViewModel) RunID() string     { return m.runID }
func (m *LogViewModel) TaskID() string    { return m.taskID }
func (m *LogViewModel) TryNumber() int    { return m.tryNumber }
func (m *LogViewModel) MaxTry() int       { return m.maxTry }

func (m *LogViewModel) SetContent(content string) {
	m.rawContent = content
	m.viewport.SetContent(content)
	m.clearSearch()
}

func (m *LogViewModel) ScrollInfo() string {
	info := fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))
	if m.searchQuery != "" && len(m.matchLines) > 0 {
		info += fmt.Sprintf(" | search: %q (%d/%d)", m.searchQuery, m.matchIdx+1, len(m.matchLines))
	} else if m.searchQuery != "" {
		info += fmt.Sprintf(" | search: %q (no matches)", m.searchQuery)
	}
	return info
}

func (m *LogViewModel) IsSearchMode() bool {
	return m.searchMode
}

func (m *LogViewModel) SearchQuery() string {
	return m.searchQuery
}
