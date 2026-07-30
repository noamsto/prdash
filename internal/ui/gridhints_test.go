package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// gridHintsRef is the pre-refactor gridHints, kept verbatim so the tests below
// can assert byte-equality differentially rather than against frozen goldens —
// which keeps them working when the theme changes.
func gridHintsRef(hints []keyHint, width int, alignKeys bool) []string {
	if len(hints) == 0 {
		return nil
	}
	keyW := 0
	if alignKeys {
		for _, h := range hints {
			if w := lipgloss.Width(h.key); w > keyW {
				keyW = w
			}
		}
	}
	render := func(h keyHint) string {
		key := accentStyle.Render(h.key)
		if pad := keyW - lipgloss.Width(h.key); pad > 0 {
			key += strings.Repeat(" ", pad)
		}
		return key + statusBarStyle.Render(" "+h.label)
	}
	const gutter = 3
	cellW := 0
	for _, h := range hints {
		if w := lipgloss.Width(render(h)); w > cellW {
			cellW = w
		}
	}
	cellW += gutter
	cols := max(1, (width+gutter)/cellW)
	var lines []string
	for i := 0; i < len(hints); i += cols {
		var b strings.Builder
		for j := i; j < i+cols && j < len(hints); j++ {
			s := render(hints[j])
			b.WriteString(s)
			if j < i+cols-1 && j < len(hints)-1 {
				b.WriteString(strings.Repeat(" ", cellW-lipgloss.Width(s)))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

// panelContentRowsRef is the pre-refactor panelContentRows: it builds both grids
// only to count their lines.
func panelContentRowsRef(innerW int) int {
	lw, rw := panelSplit(innerW)
	return max(1+len(gridHintsRef(navHintsFor("pr"), lw, false)),
		1+len(gridHintsRef(defaultActionHints(), rw, true)))
}

// hintSets covers the real hint lists plus the width edge cases the arithmetic
// must survive: a zero-width key, a nerd-font glyph, wide (CJK) text, and an
// emoji whose display width exceeds its rune count.
func hintSets() map[string][]keyHint {
	return map[string][]keyHint{
		"nav-pr":    navHintsFor("pr"),
		"nav-issue": navHintsFor("issue"),
		"actions":   defaultActionHints(),
		"single":    {{"x", "one"}},
		"empty-key": {{"", "no key"}, {"a", "has key"}},
		"wide": {
			{"\uF461", "nerd glyph"},
			{"⇥", "wide arrow"},
			{"a", "日本語のラベル"},
			{"b", "emoji ✅ label"},
			{"⌘⇧", "multi glyph"},
		},
	}
}

func TestGridHintsMatchesReferenceAcrossWidths(t *testing.T) {
	for name, hints := range hintSets() {
		for _, alignKeys := range []bool{false, true} {
			for width := 4; width <= 160; width++ {
				want := gridHintsRef(hints, width, alignKeys)
				got := gridHints(hints, width, alignKeys)
				if len(got) != len(want) {
					t.Fatalf("%s alignKeys=%v width=%d: %d lines, want %d",
						name, alignKeys, width, len(got), len(want))
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("%s alignKeys=%v width=%d line %d:\n got %q\nwant %q",
							name, alignKeys, width, i, got[i], want[i])
					}
				}
			}
		}
	}
}

func TestGridHintsEmptyReturnsNil(t *testing.T) {
	if got := gridHints(nil, 80, true); got != nil {
		t.Errorf("gridHints(nil) = %v, want nil", got)
	}
	if got := gridHints([]keyHint{}, 80, false); got != nil {
		t.Errorf("gridHints(empty) = %v, want nil", got)
	}
}

func TestPanelContentRowsMatchesReferenceAcrossWidths(t *testing.T) {
	for innerW := 10; innerW <= 200; innerW++ {
		want := panelContentRowsRef(innerW)
		if got := panelContentRows(innerW); got != want {
			t.Errorf("panelContentRows(%d) = %d, want %d", innerW, got, want)
		}
	}
}

// gridCase is one benchmark point. The benchmarks pair old against new in a
// single process so they share machine state — whole-frame ns/op compared across
// separate runs drifts too much with load to be usable.
type gridCase struct {
	name      string
	hints     []keyHint
	width     int
	alignKeys bool
}

// benchGridCases sweeps width because the column count, and so how much padding
// each row does, varies with it — a single width could flatter either side.
func benchGridCases() []gridCase {
	nav, acts := navHintsFor("pr"), defaultActionHints()
	var cases []gridCase
	for _, w := range []int{40, 88, 160} {
		cases = append(cases,
			gridCase{fmt.Sprintf("nav/w%d", w), nav, w, false},
			gridCase{fmt.Sprintf("actions/w%d", w), acts, w, true},
		)
	}
	return cases
}

func BenchmarkGridHintsRef(b *testing.B) {
	for _, c := range benchGridCases() {
		b.Run(c.name, func(b *testing.B) {
			for range b.N {
				_ = gridHintsRef(c.hints, c.width, c.alignKeys)
			}
		})
	}
}

func BenchmarkGridHints(b *testing.B) {
	for _, c := range benchGridCases() {
		b.Run(c.name, func(b *testing.B) {
			for range b.N {
				_ = gridHints(c.hints, c.width, c.alignKeys)
			}
		})
	}
}

func BenchmarkPanelContentRowsRef(b *testing.B) {
	for _, w := range []int{80, 180} {
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			for range b.N {
				_ = panelContentRowsRef(w)
			}
		})
	}
}

func BenchmarkPanelContentRows(b *testing.B) {
	for _, w := range []int{80, 180} {
		b.Run(fmt.Sprintf("w%d", w), func(b *testing.B) {
			for range b.N {
				_ = panelContentRows(w)
			}
		})
	}
}

// TestHintCellWidthIsStyleIndependent pins gridLayout's premise: ANSI carries
// zero display width, so a cell's width follows from the plain key and label. If
// this fails the arithmetic is wrong and panel columns will misalign.
func TestHintCellWidthIsStyleIndependent(t *testing.T) {
	for name, hints := range hintSets() {
		for _, alignKeys := range []bool{false, true} {
			keyW := 0
			if alignKeys {
				for _, h := range hints {
					keyW = max(keyW, lipgloss.Width(h.key))
				}
			}
			for _, h := range hints {
				styled := accentStyle.Render(h.key)
				if pad := keyW - lipgloss.Width(h.key); pad > 0 {
					styled += strings.Repeat(" ", pad)
				}
				styled += statusBarStyle.Render(" " + h.label)

				want := lipgloss.Width(styled)
				got := max(keyW, lipgloss.Width(h.key)) + 1 + lipgloss.Width(h.label)
				if got != want {
					t.Errorf("%s alignKeys=%v %s: arithmetic width %d, measured %d",
						name, alignKeys, fmt.Sprintf("%q/%q", h.key, h.label), got, want)
				}
			}
		}
	}
}
