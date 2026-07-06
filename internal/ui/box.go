package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// indentLines prefixes every line of s with n spaces.
func indentLines(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// clipLines keeps at most the first n lines of s.
func clipLines(s string, n int) string {
	if n < 0 {
		n = 0
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// dropLines removes the first n lines of s (for scrolling).
func dropLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return ""
	}
	return strings.Join(lines[n:], "\n")
}

// titledBox wraps content in a rounded border of OUTER size w × h, with title
// set into the top edge. lipgloss has no native border title, so the body is
// rendered with left/right/bottom borders only and a hand-built top line is
// prepended. Content is clipped to the interior so it never overflows the box.
func titledBox(content string, w, h int, title string) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	rb := lipgloss.RoundedBorder()
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	body := lipgloss.NewStyle().
		Border(rb, false, true, true, true).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(w).Height(h - 1).MaxWidth(w).MaxHeight(h - 1).
		Render(clipLines(content, h-2))
	label := " " + truncate(title, w-4) + " "
	if lipgloss.Width(label) > w-3 { // cap the label so the top line stays exactly w wide
		label = truncate(label, w-3)
	}
	rest := w - 3 - lipgloss.Width(label)
	top := rule.Render(rb.TopLeft+rb.Top) +
		accentStyle.Render(label) +
		rule.Render(strings.Repeat(rb.Top, rest)+rb.TopRight)
	return top + "\n" + body
}

// overlayTop composites panel horizontally centered over base, anchored to a
// fixed row near the top so overlays of differing height don't jump vertically
// as their content changes. Tall panels are pulled up only as far as needed to
// stay on screen. Layer.Draw ignores its own x/y, so the positioning has to go
// through a Compositor, which draws each layer at its absolute bounds.
func overlayTop(base, panel string, w, h int) string {
	pw, ph := lipgloss.Width(panel), lipgloss.Height(panel)
	px, py := (w-pw)/2, h/5
	if py+ph > h {
		py = h - ph
	}
	if px < 0 {
		px = 0
	}
	if py < 0 {
		py = 0
	}
	canvas := lipgloss.NewCanvas(w, h)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(panel).X(px).Y(py).Z(1),
	))
	return canvas.Render()
}
