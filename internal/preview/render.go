package preview

import "charm.land/glamour/v2"

// rendererByWidth memoizes term renderers per wrap width. NewTermRenderer parses
// the chroma style on every call, so building one per render frame lags the UI.
// Width changes only on resize, so the cache stays tiny. Only touched from the
// bubbletea View loop (single goroutine), so no locking is needed.
var rendererByWidth = map[int]*glamour.TermRenderer{}

// Render renders markdown to ANSI at the given wrap width. No pipe-stripping —
// tables and pipe-containing code render normally.
func Render(md string, width int) (string, error) {
	r, ok := rendererByWidth[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(darkStyle),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", err
		}
		rendererByWidth[width] = r
	}
	return r.Render(md)
}
