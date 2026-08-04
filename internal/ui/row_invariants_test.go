package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestRenderItemRowInvariantsAcrossLoginWidthTreeFocus is a standing regression
// guard for renderItemRow's structural contract, put in place so the three
// upcoming column additions (diffstat, ticket id, responsive ladder) land
// against a guard that already exists. It exists because the commit that
// introduced author-bounding (#88) shipped two defects that the whole suite
// passed: an unbounded author column made rows wider than their target width,
// and truncating the author before authorStyle hashed it split one person into
// two colors. Both were invisible because every fixture used a short login and
// nothing swept width alongside login length, nor checked that hue survives
// truncation.
func TestRenderItemRowInvariantsAcrossLoginWidthTreeFocus(t *testing.T) {
	// Login lengths: 0 (no author), a short human login, GitHub's realistic
	// "octocat-bot" and "github-actions[bot]" fixtures (both trip isBot, so
	// authorStyle takes the dimStyle path rather than the hash path — still
	// worth sweeping since defect 1's overflow didn't care which path it was),
	// and synthetic logins bracketing up to GitHub's 39-character maximum.
	logins := []string{"", "al", "octocat-bot", loginOfLength(14), "github-actions[bot]", loginOfLength(27), loginOfLength(39)}
	wantLens := []int{0, 2, 11, 14, 19, 27, 39}
	for i, l := range logins {
		if len(l) != wantLens[i] {
			t.Fatalf("fixture login %q has length %d, want %d (test would sweep the wrong dimension)", l, len(l), wantLens[i])
		}
	}

	// Three of these deliberately overflow the tree slot's 3-cell budget so the
	// clip path (ansi.Truncate) is exercised, not just the values that fit.
	trees := []string{"", " ", "│ ", "├─ ", "├── ", "重试", "╰──── "}

	states := []struct {
		name              string
		focused, selected bool
	}{
		{"unfocused", false, false},
		{"focused", true, false}, // rowBgWrap path: re-injects background after every SGR reset
		{"selected", false, true},
	}

	const (
		title = "sample title text without digits or hashes"
		num   = "#42"
		age   = "2d"
	)
	ci := ciGlyph("success")
	review := reviewDot("APPROVED")
	auto := autoMergeGlyph(true)

	// renderItemRow floors its working width at 24 and reserves 3 cells for the
	// tree slot, so 26 is the smallest width where exact-fill is a real
	// contract; below it the row is legitimately wider than w (covered by
	// TestDenseRowDegradesWithoutCrashAtNarrowWidths). Sweeping every integer up
	// to 200 is cheap since this is pure string composition.
	for w := 26; w <= 200; w++ {
		for _, login := range logins {
			wantSGR := extractLeadingSGRPrefix(authorStyle(login).Render("x"))

			for _, st := range states {
				opts := RowOpts{Width: w, Focused: st.focused, Selected: st.selected}

				// baseOffset pins invariant 4: the tree slot is a fixed-width
				// reservation, so the #number's column must not move regardless
				// of what's drawn into that slot.
				var baseOffset int
				for ti, tree := range trees {
					opts.Tree = tree
					row := renderItemRow(opts, accentStyle, num, title, login, age, ci, review, auto)

					if got := lipgloss.Width(row); got != w {
						t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s: row width %d, want exactly %d",
							login, len(login), w, tree, st.name, got, w)
					}
					if strings.Contains(row, "\n") {
						t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s: row is not a single line: %q",
							login, len(login), w, tree, st.name, row)
					}

					// Trap: strings.Index below returns a BYTE offset. Box-drawing
					// glyphs (│, ├, ─) are 3 bytes each and 重/试 are 3 bytes but 2
					// cells, so a byte offset fed straight into a "column" check
					// would be garbage. Strip ANSI first, then convert the byte
					// offset to a cell offset via lipgloss.Width on the prefix —
					// that's the only combination that's actually correct.
					plain := ansi.Strip(row)

					if !strings.Contains(plain, age) {
						t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s: age %q missing from row: %q",
							login, len(login), w, tree, st.name, age, plain)
					}

					idx := strings.Index(plain, num)
					if idx < 0 {
						t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s: %q not found in row: %q",
							login, len(login), w, tree, st.name, num, plain)
					}
					offset := lipgloss.Width(plain[:idx])
					if ti == 0 {
						baseOffset = offset
					} else if offset != baseOffset {
						t.Errorf("login=%q(len %d) w=%d tree=%q state=%s: #number at col %d, want %d (tree slot must stay a fixed 3 cells)",
							login, len(login), w, tree, st.name, offset, baseOffset)
					}

					// Invariant 5, the actual pin for defect 2: the hue must come
					// from the FULL login, not whatever truncate() left in the
					// row. Comparing the raw (unstripped) row against the SGR
					// prefix authorStyle(login) produces on the full login catches
					// a regression to hashing the truncated display text instead.
					if wantSGR != "" && !strings.Contains(row, wantSGR) {
						t.Errorf("login=%q(len %d) w=%d tree=%q state=%s: row missing author hue SGR %q for the full login (hue must not be derived from truncated display text)",
							login, len(login), w, tree, st.name, wantSGR)
					}
				}
			}
		}
	}
}

// loginOfLength returns a deterministic login string of exactly n characters.
// The charset avoids "bot" as a substring so these fixtures don't accidentally
// trip isBot and take the dimStyle path meant for the "octocat-bot" and
// "github-actions[bot]" fixtures above.
func loginOfLength(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789-"
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(charset)
	}
	return b.String()[:n]
}
