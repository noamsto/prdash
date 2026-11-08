package preview

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
)

// ampSentinel stands in for an `&` that could start a control character
// reference; Render restores it to `&` after glamour returns.
const ampSentinel = "\uE000"

var ampToken = regexp.MustCompile(`&(?:#[0-9]+;|#[xX][0-9a-fA-F]+;|[a-zA-Z])?`)

// neutralizeAmp replaces every `&` that is not provably harmless with
// ampSentinel. Glamour decodes character references after the sanitize
// boundary and concatenates inline nodes before decoding, so any `&` that can
// start a numeric reference must not reach it.
func neutralizeAmp(md string) string {
	return ampToken.ReplaceAllStringFunc(md, func(tok string) string {
		if len(tok) == 2 && isASCIILetter(tok[1]) {
			return tok
		}
		if dec := html.UnescapeString(tok); dec != tok && !strings.ContainsFunc(dec, unicode.IsControl) {
			return tok
		}
		return ampSentinel + tok[1:]
	})
}

func isASCIILetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

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
			glamour.WithStyles(activeStyle),
			glamour.WithWordWrap(width),
			// Table links otherwise defer to a footnote list, which reads
			// Link.Format and so prints the label instead of the suppressed URL.
			glamour.WithInlineTableLinks(true),
		)
		if err != nil {
			return "", err
		}
		rendererByWidth[width] = r
	}
	renderMisses++
	out, err := r.Render(neutralizeAmp(md))
	if err != nil {
		return "", err
	}
	out = strings.ReplaceAll(out, ampSentinel, "&")
	outputByKey[key] = out
	return out, nil
}

// SetMode swaps the active glamour style and flushes the memoized renderers and
// output, so the next Render rebuilds against the new palette. Same single
// goroutine as Render (the bubbletea View loop), so no lock.
func SetMode(mode string) {
	if mode == "light" {
		activeStyle = lightStyle
	} else {
		activeStyle = darkStyle
	}
	rendererByWidth = map[int]*glamour.TermRenderer{}
	outputByKey = map[string]string{}
}
