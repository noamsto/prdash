package preview

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// hideFormat is a Format template that emits nothing. StylePrimitive.Format is
// a Go template applied to the rendered token, and an EMPTY Format means "no
// template" (passthrough) — so suppressing a run needs a template that
// evaluates to nothing rather than "".
const hideFormat = "{{if false}}{{.text}}{{end}}"

// terminalStyle adapts a stock glamour style for a terminal PR dashboard:
//
//   - H1-H6 lose their literal "#" marker prefix and bold instead. Bot review
//     comments lead with a markdown heading (Cursor BugBot titles findings with
//     ###) and the stock configs print the marker verbatim.
//   - Images are suppressed entirely, alt text and URL both. Every image in a
//     review comment is a shields.io severity badge that cannot render here.
//   - A link's href stops being painted, but its OSC 8 wrapper is untouched, so
//     links stay clickable while a 100-character blob URL no longer wraps across
//     four lines. Table links are unaffected: inside a table glamour sets
//     LinkElement.SkipHref, so renderHrefPart never consults Link.Format.
//
// s is a copy, and Bold is replaced rather than written through, so the stock
// package-level configs are left intact.
func terminalStyle(s ansi.StyleConfig) ansi.StyleConfig {
	bold := true
	for _, h := range []*ansi.StyleBlock{&s.H1, &s.H2, &s.H3, &s.H4, &s.H5, &s.H6} {
		h.Prefix = ""
		h.Bold = &bold
	}
	s.ImageText.Format = hideFormat
	s.Image.Format = hideFormat
	s.Link.Format = hideFormat
	return s
}

// darkStyle/lightStyle are glamour's built-in chroma styles, minus the heading
// markers. We deliberately do NOT post-process rendered output (no
// pipe-stripping), so tables render intact.
var (
	darkStyle  = terminalStyle(styles.DarkStyleConfig)
	lightStyle = terminalStyle(styles.LightStyleConfig)
)

// activeStyle is what Render builds renderers from; SetMode swaps it.
var activeStyle = darkStyle
