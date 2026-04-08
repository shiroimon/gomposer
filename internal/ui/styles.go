package ui

import "github.com/charmbracelet/lipgloss"

// ColorTheme defines the accent color for an environment.
type ColorTheme struct {
	Name    string
	Accent  lipgloss.Color // primary accent (header, active tab bg, selected row bg)
	AccentFg lipgloss.Color // foreground on accent background
}

// Google Cloud Composer palette:
//   Google Blue  #4285F4 → 256-color "69" (#5f87ff)
//   Light Blue   #669DF6 → 256-color "111" (#87afff)
//   Pale Blue    #AECBFA → 256-color "153" (#afd7ff)

var themeMap = map[string]ColorTheme{
	"red":     {Name: "red", Accent: lipgloss.Color("196"), AccentFg: lipgloss.Color("15")},
	"blue":    {Name: "blue", Accent: lipgloss.Color("69"), AccentFg: lipgloss.Color("15")},
	"green":   {Name: "green", Accent: lipgloss.Color("42"), AccentFg: lipgloss.Color("0")},
	"yellow":  {Name: "yellow", Accent: lipgloss.Color("214"), AccentFg: lipgloss.Color("0")},
	"default": {Name: "default", Accent: lipgloss.Color("69"), AccentFg: lipgloss.Color("15")},
}

func GetTheme(name string) ColorTheme {
	if t, ok := themeMap[name]; ok {
		return t
	}
	return themeMap["default"]
}

// currentTheme is the active color theme — set at startup and on env switch.
var currentTheme = themeMap["default"]

func SetTheme(name string) {
	currentTheme = GetTheme(name)
	rebuildStyles()
}

var (
	HeaderStyle      lipgloss.Style
	ActiveTabStyle   lipgloss.Style
	InactiveTabStyle lipgloss.Style
	TabBarStyle      lipgloss.Style
	HelpStyle        lipgloss.Style
	SelectedRowStyle lipgloss.Style
	NormalRowStyle   lipgloss.Style
	ChangedRowStyle  lipgloss.Style
	TableHeaderStyle lipgloss.Style
)

func init() {
	rebuildStyles()
}

// GopherBlue is the official Go mascot color (#00ADD8 → 256-color 38).
var GopherBlue = lipgloss.Color("38")

func rebuildStyles() {
	HeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(GopherBlue).
		PaddingLeft(1)

	ActiveTabStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(currentTheme.AccentFg).
		Background(currentTheme.Accent).
		Padding(0, 2)

	InactiveTabStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("111")). // light blue — Cloud Composer palette
		Padding(0, 2)

	TabBarStyle = lipgloss.NewStyle().
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		MarginBottom(1)

	HelpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	SelectedRowStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color("153")) // Composer pale blue #AECBFA

	NormalRowStyle = lipgloss.NewStyle()

	ChangedRowStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("58"))

	TableHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252")).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))
}

func StateStyle(state string) lipgloss.Style {
	switch state {
	case "success":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // green
	case "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("69"))  // Google Blue
	case "skipped":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	case "upstream_failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")) // dark orange
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // gray
	}
}

func PausedStyle(paused bool) lipgloss.Style {
	if paused {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // green
}
