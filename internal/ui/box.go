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

// boxBody renders content inside a rounded left/right/bottom border of OUTER
// width w and OUTER height h; the top edge is drawn separately by the caller so
// a label or tab bar can be set into it. Content is clipped to the interior.
func boxBody(content string, w, h int) string {
	rb := lipgloss.RoundedBorder()
	return lipgloss.NewStyle().
		Border(rb, false, true, true, true).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(w).Height(h - 1).MaxWidth(w).MaxHeight(h - 1).
		Render(clipLines(content, h-2))
}

// boxTop builds the rounded top edge of OUTER width w with a pre-rendered
// segment (carrying its own colors, display width segW) set into it just past
// the left corner, padding the remainder with the border rule.
func boxTop(segment string, segW, w int) string {
	rb := lipgloss.RoundedBorder()
	rule := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	rest := w - 3 - segW
	if rest < 0 {
		rest = 0
	}
	return rule.Render(rb.TopLeft+rb.Top) + segment +
		rule.Render(strings.Repeat(rb.Top, rest)+rb.TopRight)
}

// titledBox wraps content in a rounded border of OUTER size w × h, with title
// set into the top edge. lipgloss has no native border title, so the top line
// is hand-built and prepended to a top-less bordered body.
func titledBox(content string, w, h int, title string) string {
	return titledBoxTinted(content, w, h, title, accentStyle)
}

// titledBoxTinted is titledBox with the title painted in a caller-chosen style,
// so the PR and Issue boards can tint their box titles in distinct accents.
func titledBoxTinted(content string, w, h int, title string, tint lipgloss.Style) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	label := " " + truncate(title, w-4) + " "
	if lipgloss.Width(label) > w-3 { // cap the label so the top line stays exactly w wide
		label = truncate(label, w-3)
	}
	return boxTop(tint.Render(label), lipgloss.Width(label), w) + "\n" + boxBody(content, w, h)
}

// tabbedBox is a titledBox whose top edge carries a tab bar instead of a single
// title: the active tab is an accent pill, the rest dim — the same accent chrome
// the board's boxes use, so the expanded view frames its content to match.
func tabbedBox(content string, w, h int, tabs []string, active int) string {
	if w < 4 {
		w = 4
	}
	if h < 2 {
		h = 2
	}
	seg := tabSegment(tabs, active)
	return boxTop(seg, lipgloss.Width(seg), w) + "\n" + boxBody(content, w, h)
}

// tabSegment renders the tab labels as pill-padded names notched into the border
// rule: one rule tick flanks each side and joins adjacent tabs, so the labels
// sit on the top edge rather than floating above it.
func tabSegment(tabs []string, active int) string {
	tick := sepStyle.Render(lipgloss.RoundedBorder().Top)
	parts := make([]string, len(tabs))
	for i, t := range tabs {
		st := tabInactiveStyle
		if i == active {
			st = tabActiveStyle
		}
		parts[i] = st.Render(t)
	}
	return tick + strings.Join(parts, tick) + tick
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
