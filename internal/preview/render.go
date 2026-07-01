package preview

import (
	"fmt"

	"charm.land/glamour/v2"
)

// rendererByWidth memoizes term renderers per wrap width. NewTermRenderer parses
// the chroma style on every call, so building one per render frame lags the UI.
// Width changes only on resize, so the cache stays tiny. Only touched from the
// bubbletea View loop (single goroutine), so no locking is needed.
var rendererByWidth = map[int]*glamour.TermRenderer{}

// outputByKey memoizes the rendered ANSI per (width, body). The bubbletea View
// loop re-renders the preview on every keystroke, and glamour's markdown→ANSI
// pass (chroma highlighting + wrapping) is the dominant per-frame cost; a body
// is immutable once fetched, so the same (width, body) always maps to the same
// output. Same goroutine as rendererByWidth, so no locking.
var outputByKey = map[string]string{}

// renderMisses counts glamour renders that were not served from outputByKey.
// Test-only observability for the memoization.
var renderMisses int

// Render renders markdown to ANSI at the given wrap width. No pipe-stripping —
// tables and pipe-containing code render normally.
func Render(md string, width int) (string, error) {
	key := fmt.Sprintf("%d\x00%s", width, md)
	if out, ok := outputByKey[key]; ok {
		return out, nil
	}
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
	renderMisses++
	out, err := r.Render(md)
	if err != nil {
		return "", err
	}
	outputByKey[key] = out
	return out, nil
}
