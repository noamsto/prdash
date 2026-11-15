package preview

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
)

// ampSentinel picks the rune that stands in for an `&` that could start a
// control character reference. It prefers punctuation so emphasis flanking
// matches a real `&`, and must be one column wide like `&`: the range stops
// before U+16FE2, a wide punctuation rune. It must be absent from md and from
// md with references decoded, or restoring it would rewrite real input.
func ampSentinel(md string) rune {
	decoded := html.UnescapeString(md)
	absent := func(r rune) bool {
		return !strings.ContainsRune(md, r) && !strings.ContainsRune(decoded, r)
	}
	for r := rune(0x10000); r <= 0x16FDF; r++ {
		if unicode.IsPunct(r) && absent(r) {
			return r
		}
	}
	for r := rune(0xE000); r <= 0xF8FF; r++ {
		if absent(r) {
			return r
		}
	}
	return 0xE000
}

var ampToken = regexp.MustCompile(`&(?:#[0-9]+;|#[xX][0-9a-fA-F]+;|[a-zA-Z0-9=\-.%+,/$:@])?`)

// neutralizeAmp replaces every `&` that is not provably harmless with
// sentinel. Glamour decodes character references after the sanitize
// boundary and concatenates inline nodes before decoding, so any `&` that can
// start a numeric reference must not reach it. An `&` before a letter, digit
// or `=-.%+,/$:@` stays: those are not inline markup, so nothing can be removed
// between them and a following `#` to splice a reference together.
func neutralizeAmp(md string, sentinel rune) string {
	return ampToken.ReplaceAllStringFunc(md, func(tok string) string {
		if len(tok) == 2 {
			return tok
		}
		if dec := html.UnescapeString(tok); dec != tok && !strings.ContainsFunc(dec, unicode.IsControl) {
			return tok
		}
		return string(sentinel) + tok[1:]
	})
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
	sentinel := ampSentinel(md)
	out, err := r.Render(neutralizeAmp(md, sentinel))
	if err != nil {
		return "", err
	}
	out = strings.ReplaceAll(out, string(sentinel), "&")
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
