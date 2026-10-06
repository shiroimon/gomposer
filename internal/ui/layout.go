package ui

import "github.com/charmbracelet/lipgloss"

// scrollOffset returns the first visible row so that cursor stays inside a
// window of height rows, moving the window only when the cursor leaves it.
func scrollOffset(offset, cursor, n, height int) int {
	if height <= 0 || n <= height {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+height {
		offset = cursor - height + 1
	}
	if maxOffset := n - height; offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// visibleRange returns the [start, end) rows to render. height <= 0 means unlimited.
func visibleRange(offset, cursor, n, height int) (start, end int) {
	if height <= 0 {
		return 0, n
	}
	start = scrollOffset(offset, cursor, n, height)
	end = start + height
	if end > n {
		end = n
	}
	return start, end
}

// fitBody clips body to exactly height lines and width columns so the chrome
// around it never moves. Long lines would otherwise wrap and push the header off-screen.
func fitBody(body string, width, height int) string {
	if height <= 0 {
		return body
	}
	s := lipgloss.NewStyle().Height(height).MaxHeight(height)
	if width > 0 {
		s = s.MaxWidth(width)
	}
	return s.Render(body)
}

// tableHeaderLines is the height of a rendered table header, including its underline border.
func tableHeaderLines() int {
	return lipgloss.Height(TableHeaderStyle.Render(" "))
}
