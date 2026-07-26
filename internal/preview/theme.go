package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// noHeadingMarkers strips glamour's literal "#"-marker prefixes from H1-H6 and
// bolds the heading instead. Bot review comments lead with a markdown heading
// (Cursor BugBot titles its findings with ###), and the stock configs print the
// marker verbatim into the pane.
//
// s is a copy, and Bold is replaced rather than written through, so the stock
// package-level configs are left intact.
func noHeadingMarkers(s ansi.StyleConfig) ansi.StyleConfig {
	bold := true
	for _, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix = ""
		h.Bold = &bold
	}
	return s
}

// darkStyle/lightStyle are glamour's built-in chroma styles, minus the heading
// markers. We deliberately do NOT post-process rendered output (no
// pipe-stripping), so tables render intact.
var (
	darkStyle  = noHeadingMarkers(styles.DarkStyleConfig)
	lightStyle = noHeadingMarkers(styles.LightStyleConfig)
)

// activeStyle is what Render builds renderers from; SetMode swaps it.
var activeStyle = darkStyle
