package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

// sweepPRs is a board with every glyph column populated (failing CI, changes
// requested, a draft, a wide PR number) plus a wide-rune (CJK) title, so the
// dense-row layout math is exercised at its worst case — including the
// lipgloss.Width-vs-len accounting that a byte-length shortcut would get wrong —
// not just a short ASCII happy-path row.
func sweepPRs() []gh.PR {
	mk := func(n int, title, login, review string, ci []gh.Check, draft bool) gh.PR {
		p := gh.PR{Number: n, Title: title, ReviewDecision: review, StatusCheckRollup: ci, IsDraft: draft}
		p.Author.Login = login
		return p
	}
	failing := []gh.Check{{State: "FAILURE", Name: "test (ubuntu-latest)"}}
	return []gh.PR{
		mk(7, "Add retry logic to the flaky uploader path with backoff", "al", "APPROVED", failing, false),
		mk(4321, "Rework the whole layout engine so the board scans vertically", "octocat-bot", "CHANGES_REQUESTED", nil, false),
		mk(88, "Draft: spike a new caching layer", "carol", "REVIEW_REQUIRED", nil, true),
		// Short wide-rune title (each CJK glyph is 2 cells): the title never needs
		// truncation at the swept widths, so exact-fill must still hold — which only
		// works if the gutter/gap math measures display cells, not byte length.
		mk(512, "重试 retry", "dana", "APPROVED", nil, false),
	}
}

// TestDenseRowFillsWidthAcrossWidthSweep locks the core board invariant: every
// dense row is one line and fills exactly the target width, across a sweep of
// widths (not the single 80-col point the existing single-line test checks). An
// off-by-one in the gutter/gap math or a truncation regression would break the
// exact-fill and misalign the columns. The sweep floor (40) is the realistic
// minimum list width; renderItemRow deliberately floors truncation below 24 cols
// (a degenerate terminal), so exact-fill is not promised there — see
// TestDenseRowDegradesWithoutCrashAtNarrowWidths for that regime.
func TestDenseRowFillsWidthAcrossWidthSweep(t *testing.T) {
	prs := sweepPRs()
	ps := NewPRSection("is:open")
	ps.SetPRs(prs)
	if got := ps.Len(); got != len(prs) {
		t.Fatalf("fixture lost rows: section Len() = %d, want %d (test would pass vacuously)", got, len(prs))
	}
	nw := columnWidths(ps)
	flag := failStyle.Render("⚠")

	for _, w := range []int{40, 52, 64, 80, 100, 120, 160, 200} {
		for i := 0; i < ps.Len(); i++ {
			for _, focused := range []bool{false, true} {
				row := ps.RenderRow(i, RowOpts{Width: w, NumWidth: nw, Focused: focused, Flag: flag})
				if strings.Contains(row, "\n") {
					t.Fatalf("w=%d row %d focused=%v is not a single line: %q", w, i, focused, row)
				}
				if got := lipgloss.Width(row); got != w {
					t.Errorf("w=%d row %d focused=%v: rendered width %d, want exactly %d (columns won't fill/align)", w, i, focused, got, w)
				}
			}
		}
	}
}

// TestDenseRowDegradesWithoutCrashAtNarrowWidths documents the narrow-terminal
// contract: below the exact-fill regime the row is not width-bounded (a known
// degenerate edge in renderItemRow's floor), but it must still render as a single
// non-empty line rather than panic or emit a multi-line blob.
func TestDenseRowDegradesWithoutCrashAtNarrowWidths(t *testing.T) {
	prs := sweepPRs()
	ps := NewPRSection("is:open")
	ps.SetPRs(prs)
	if got := ps.Len(); got != len(prs) {
		t.Fatalf("fixture lost rows: section Len() = %d, want %d", got, len(prs))
	}
	nw := columnWidths(ps)

	// renderItemRow floors its working width at 24, so 1 and 24 bracket the
	// whole sub-floor regime — intermediate points render identically.
	for _, w := range []int{1, 24} {
		for i := 0; i < ps.Len(); i++ {
			row := ps.RenderRow(i, RowOpts{Width: w, NumWidth: nw})
			if strings.Contains(row, "\n") {
				t.Fatalf("w=%d row %d must stay a single line even when degenerate: %q", w, i, row)
			}
			if lipgloss.Width(row) == 0 {
				t.Fatalf("w=%d row %d rendered empty", w, i)
			}
		}
	}
}

// TestDenseRowFillsWidthWithLabels is TestDenseRowFillsWidthAcrossWidthSweep's
// labeled-row counterpart: with chips in the flexible middle, exact-fill must
// still hold at every swept width, focused and unfocused. sweepPRs is left
// untouched (other tests rely on it carrying no labels).
func TestDenseRowFillsWidthWithLabels(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(ps)
	for _, w := range []int{40, 52, 64, 80, 100, 120, 160, 200} {
		for _, focused := range []bool{false, true} {
			row := ps.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: focused})
			if strings.Contains(row, "\n") {
				t.Fatalf("w=%d focused=%v labeled row not single line: %q", w, focused, row)
			}
			if got := lipgloss.Width(row); got != w {
				t.Errorf("w=%d focused=%v labeled row width %d, want %d", w, focused, got, w)
			}
		}
	}
}

// TestDenseRowColumnsAlignAcrossNumberWidths guards the aligned-columns AC: rows
// whose PR numbers differ in digit count must still start their title at the same
// visual column (the number cell is right-padded to a shared width). Checked at
// both a narrow and a roomy width so the NumWidth padding is exercised together
// with the title-truncation squeeze.
func TestDenseRowColumnsAlignAcrossNumberWidths(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{
		{Number: 7, Title: "AAA short-number row"},
		{Number: 4321, Title: "BBB wide-number row"},
	})
	nw := columnWidths(ps)

	titleStart := func(t *testing.T, row, fragment string) int {
		t.Helper()
		plain := ansi.Strip(row)
		idx := strings.Index(plain, fragment)
		if idx < 0 {
			t.Fatalf("fragment %q not found in row %q", fragment, plain)
		}
		return lipgloss.Width(plain[:idx])
	}

	for _, w := range []int{44, 80} {
		// SetPRs may reorder rows, so locate each by its unique title fragment
		// rather than assuming a row index.
		startA, startB, foundA, foundB := -1, -1, false, false
		for i := 0; i < ps.Len(); i++ {
			row := ps.RenderRow(i, RowOpts{Width: w, NumWidth: nw})
			plain := ansi.Strip(row)
			switch {
			case strings.Contains(plain, "AAA"):
				startA, foundA = titleStart(t, row, "AAA"), true
			case strings.Contains(plain, "BBB"):
				startB, foundB = titleStart(t, row, "BBB"), true
			}
		}
		if !foundA || !foundB {
			t.Fatalf("w=%d: did not render both rows (foundA=%v foundB=%v)", w, foundA, foundB)
		}
		if startA != startB {
			t.Errorf("w=%d: title columns misaligned: short-number title starts at col %d, wide-number at col %d", w, startA, startB)
		}
	}
}

// TestBoardFitsWidthAndSurvivesResize drives a WindowSizeMsg resize sequence
// through one model (the path Update takes on a real terminal resize) and asserts
// after every resize that the panes never overflow the terminal width and the
// full frame never exceeds its height. This is the only test that exercises the
// resize handler; it spans the side-card threshold (119/120) and narrow widths.
func TestBoardFitsWidthAndSurvivesResize(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	if got := m.section.Len(); got != len(sweepPRs()) {
		t.Fatalf("board is empty: section Len() = %d, want %d (bounds checks would pass vacuously)", got, len(sweepPRs()))
	}
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BEHIND"}
	m.detail[4321] = gh.PRDetail{MergeStateStatus: "DIRTY"}
	m.loaded = true

	sizes := [][2]int{{200, 40}, {160, 50}, {121, 30}, {120, 24}, {119, 45}, {90, 30}, {70, 40}, {200, 16}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		m = u.(Model)

		for i, ln := range strings.Split(m.renderMain(), "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("%dx%d: renderMain line %d width %d exceeds terminal width %d", w, h, i, lw, w)
			}
		}
		if fh := lipgloss.Height(m.render()); fh > h {
			t.Errorf("%dx%d: full render height %d exceeds terminal height %d", w, h, fh, h)
		}
		if want := w >= sideThreshold; computeLayout(w, h).ShowSide != want {
			t.Errorf("%dx%d: ShowSide = %v, want %v (side threshold is %d)", w, h, !want, want, sideThreshold)
		}
	}
}

// TestFooterToggleNeverOverflowsAcrossResizeSweep locks the no-off-by-one
// acceptance criterion: sweeping heights straddling footerMinHeight (and a
// couple of widths straddling footerMinWidth), the board, expanded, and log
// views must never render more lines than the terminal is tall, and the
// board must never render wider than the terminal, regardless of which side
// of the footer-visibility threshold the size falls on.
func TestFooterToggleNeverOverflowsAcrossResizeSweep(t *testing.T) {
	widths := []int{40, footerMinWidth - 1, footerMinWidth, footerMinWidth + 1, 160}
	heights := []int{10, footerMinHeight - 1, footerMinHeight, footerMinHeight + 1, 50}

	for _, w := range widths {
		for _, h := range heights {
			t.Run(fmt.Sprintf("w=%d,h=%d", w, h), func(t *testing.T) {
				m := NewModel("/repo", "is:open", nil)
				m.SetRepo("r")
				m.width, m.height = w, h
				m.setPRs(sweepPRs())

				board := m.board()
				if lines := strings.Count(board, "\n") + 1; lines > h {
					t.Errorf("board: %d lines exceeds height %d", lines, h)
				}
				if width := ansi.StringWidth(strings.SplitN(board, "\n", 2)[0]); width > w {
					t.Errorf("board header line width %d exceeds terminal width %d", width, w)
				}

				m.enterExpanded()
				expanded := m.expandedView()
				if lines := strings.Count(expanded, "\n") + 1; lines > h {
					t.Errorf("expanded: %d lines exceeds height %d", lines, h)
				}

				u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
				m2 := u.(Model)
				withLegend := m2.render()
				if lines := strings.Count(withLegend, "\n") + 1; lines > h {
					t.Errorf("expanded+legend: %d lines exceeds height %d", lines, h)
				}
				if width := lipgloss.Width(withLegend); width > w {
					t.Errorf("expanded+legend: width %d exceeds terminal width %d", width, w)
				}
			})
		}
	}
}
