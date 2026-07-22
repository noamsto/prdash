package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderTabBar draws a single-line tab strip: the active tab as a filled
// accent badge, the rest as dim padded names. Tabs are packed greedily into w
// cells — the same idiom as renderChips — rather than truncated as a joined
// string: cutting an already-styled cell mid-way risks slicing an ANSI escape
// in half and bleeding color into the rest of the line. A pane too narrow for
// every tab simply drops the trailing ones.
func renderTabBar(tabs []string, active, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0
	for i, name := range tabs {
		st := tabInactiveStyle
		if i == active {
			st = tabActiveStyle
		}
		cell := st.Render(name)
		cw := lipgloss.Width(cell)
		if used+cw > w {
			break
		}
		b.WriteString(cell)
		used += cw
	}
	return b.String()
}
