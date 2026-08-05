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

	// One value per width path the tree slot has: absent, a value that fits, and
	// one that overflows its 3-cell budget so the clip path (ansi.Truncate) is
	// exercised. Every tree value renders to exactly 3 cells, so more variety
	// multiplies the sweep without widening what it covers.
	trees := []string{"", "│ ", "重试"}

	states := []struct {
		name              string
		focused, selected bool
	}{
		{"unfocused", false, false},
		{"focused", true, false}, // rowBgWrap path: re-injects background after every SGR reset
		{"selected", false, true},
	}

	// Number length moves leftW, which every downstream budget subtracts from w,
	// so an off-by-one in that arithmetic can hide at one length and bite at
	// another. Both carry the NumWidth columnWidths would pick for a shown set
	// whose widest number is this one.
	nums := []struct {
		num   string
		width int
	}{
		{"#123", 4},
		{"#123456", 7},
	}

	// Age dimension: ageString's days branch is unbounded, and the merged and
	// closed views age from MergedAt/ClosedAt, so a repo with years of history
	// renders 4- and 5-character ages routinely. The row reserves this column, so
	// a reservation narrower than the rendered value over-commits every budget
	// carved out of it.
	ages := []string{"2d", "100d", "1000d"}

	const title = "sample title text without digits or hashes"
	ci := ciGlyph("success")
	review := reviewDot("APPROVED")
	auto := autoMergeGlyph(true)

	// Diffstat dimension: absent (pre-Task-5 behavior), a small value, and a
	// large abbreviated one. small/large share one column width (the wider of
	// the two) so the sweep also exercises the pad-when-smaller path a real
	// shown set hits once diffstatWidth picks the widest row.
	diffs := []struct {
		name  string
		diff  string
		width int
	}{
		{"absent", "", 0},
		{"small", diffstat(31, 4), 0},
		{"large", diffstat(12300, 2000), 0},
	}
	dw := 0
	for _, d := range diffs[1:] {
		dw = max(dw, lipgloss.Width(d.diff))
	}
	for i := range diffs {
		if diffs[i].name != "absent" {
			diffs[i].width = dw
		}
	}

	// Ticket dimension: absent (pre-Task-6 behavior), a short GitHub-issue-style
	// id, and a long Linear-style one — same absent/small/large shape as diffs
	// above, and for the same reason: short/long share one column width (the
	// wider of the two) so the sweep also exercises the pad-when-shorter path.
	tickets := []struct {
		name   string
		ticket string
		width  int
	}{
		{"absent", "", 0},
		{"short", "#213", 0},
		{"long", "ENG-7726", 0},
	}
	tw := 0
	for _, tc := range tickets[1:] {
		tw = max(tw, len(tc.ticket))
	}
	for i := range tickets {
		if tickets[i].name != "absent" {
			tickets[i].width = tw
		}
	}

	// Exact-fill is only a contract once the row's unsheddable columns fit in w;
	// below that the row is legitimately wider (covered by
	// TestDenseRowDegradesWithoutCrashAtNarrowWidths). That floor moves with the
	// number column and the age, both of which land outside slack — so take it
	// from the render rather than re-deriving the arithmetic here: at a width of 1
	// every optional column drops out and the row comes back exactly its floor.
	floor := make([][]int, len(nums))
	for ni, nc := range nums {
		floor[ni] = make([]int, len(ages))
		for ai, age := range ages {
			floor[ni][ai] = lipgloss.Width(renderItemRow(RowOpts{Width: 1, NumWidth: nc.width},
				accentStyle, nc.num, title, "", "", age, "", ci, review, auto))
		}
	}

	// renderItemRow floors its own working width at 24, so no smaller w means
	// anything. Sweeping every integer up to 200 is cheap: this is pure string
	// composition.
	for w := 24; w <= 200; w++ {
		for _, login := range logins {
			wantSGR := extractLeadingSGRPrefix(authorStyle(login).Render("x"))

			for _, st := range states {
				for _, dc := range diffs {
					// Ticket nests inside diffs, not beside it: section.go reserves the
					// diffstat before the ticket out of the same slack, so this is the
					// pairing that actually exercises that ordering (and, at the
					// narrowest widths, the ticket's drop-out path).
					for _, tc := range tickets {
						// Landed appends a " landed" tag inside the title's room; it is a
						// dimension because that tag is drawn unconditionally, so a room
						// calculation that ignores it overflows the row.
						for _, landed := range []bool{false, true} {
							for ai, age := range ages {
								for ni, nc := range nums {
									if w < floor[ni][ai] {
										continue
									}
									num := nc.num
									opts := RowOpts{
										Width:       w,
										NumWidth:    nc.width,
										Focused:     st.focused,
										Selected:    st.selected,
										Landed:      landed,
										DiffWidth:   dc.width,
										TicketWidth: tc.width,
									}
									// baseOffset pins invariant 4: the tree slot is a fixed-width
									// reservation, so the #number's column must not move regardless
									// of what's drawn into that slot.
									var baseOffset int
									for ti, tree := range trees {
										opts.Tree = tree
										row := renderItemRow(opts, accentStyle, num, title, tc.ticket, login, age, dc.diff, ci, review, auto)

										if got := lipgloss.Width(row); got != w {
											t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v num=%q age=%s: row width %d, want exactly %d",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, num, age, got, w)
										}
										if strings.Contains(row, "\n") {
											t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v num=%q age=%s: row is not a single line: %q",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, num, age, row)
										}

										// Trap: strings.Index below returns a BYTE offset. Box-drawing
										// glyphs (│, ├, ─) are 3 bytes each and 重/试 are 3 bytes but 2
										// cells, so a byte offset fed straight into a "column" check
										// would be garbage. Strip ANSI first, then convert the byte
										// offset to a cell offset via lipgloss.Width on the prefix —
										// that's the only combination that's actually correct.
										plain := ansi.Strip(row)

										if !strings.Contains(plain, age) {
											t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v num=%q: age %q missing from row: %q",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, num, age, plain)
										}

										idx := strings.Index(plain, num)
										if idx < 0 {
											t.Fatalf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v age=%s: %q not found in row: %q",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, age, num, plain)
										}
										offset := lipgloss.Width(plain[:idx])
										if ti == 0 {
											baseOffset = offset
										} else if offset != baseOffset {
											t.Errorf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v num=%q age=%s: #number at col %d, want %d (tree slot must stay a fixed 3 cells)",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, num, age, offset, baseOffset)
										}

										// Invariant 5, the actual pin for defect 2: the hue must come
										// from the FULL login, not whatever truncate() left in the
										// row. Comparing the raw (unstripped) row against the SGR
										// prefix authorStyle(login) produces on the full login catches
										// a regression to hashing the truncated display text instead.
										if wantSGR != "" && !strings.Contains(row, wantSGR) {
											t.Errorf("login=%q(len %d) w=%d tree=%q state=%s diff=%s ticket=%s landed=%v num=%q age=%s: row missing author hue SGR %q for the full login (hue must not be derived from truncated display text)",
												login, len(login), w, tree, st.name, dc.name, tc.name, landed, num, age, wantSGR)
										}
									}
								}
							}
						}
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
