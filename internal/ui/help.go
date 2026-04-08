package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var helpKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69")) // Google Blue
var helpDescStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
var helpSep = helpDescStyle.Render(" │ ")

type HelpBinding struct {
	Key  string
	Desc string
}

// RenderHelp renders a single row of help bindings.
func RenderHelp(bindings []HelpBinding) string {
	return renderHelpRow(bindings)
}

// RenderHelpRows renders help bindings as one or two rows.
// Row 1: primary bindings. Row 2: secondary bindings (if any).
// If the terminal is narrow, each row wraps automatically.
func RenderHelpRows(primary, secondary []HelpBinding, width int) string {
	row1 := renderHelpWrapped(primary, width)
	if len(secondary) == 0 {
		return row1
	}
	row2 := renderHelpWrapped(secondary, width)
	return row1 + "\n" + row2
}

func renderHelpRow(bindings []HelpBinding) string {
	var parts []string
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.Key)+" "+helpDescStyle.Render(b.Desc))
	}
	return strings.Join(parts, helpSep)
}

// renderHelpWrapped renders bindings with wrapping at the given width.
func renderHelpWrapped(bindings []HelpBinding, width int) string {
	if width <= 0 {
		return renderHelpRow(bindings)
	}

	var lines []string
	var currentParts []string
	currentLen := 0
	sepLen := 3 // " │ "

	for i, b := range bindings {
		part := helpKeyStyle.Render(b.Key) + " " + helpDescStyle.Render(b.Desc)
		// Estimate visible length (key + space + desc)
		visLen := len(b.Key) + 1 + len(b.Desc)

		addLen := visLen
		if len(currentParts) > 0 {
			addLen += sepLen
		}

		if currentLen+addLen > width && len(currentParts) > 0 {
			lines = append(lines, strings.Join(currentParts, helpSep))
			currentParts = nil
			currentLen = 0
			_ = i // reset
		}

		currentParts = append(currentParts, part)
		if currentLen > 0 {
			currentLen += sepLen
		}
		currentLen += visLen
	}

	if len(currentParts) > 0 {
		lines = append(lines, strings.Join(currentParts, helpSep))
	}

	return strings.Join(lines, "\n")
}
