package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

func TestReviewDot(t *testing.T) {
	cases := map[string]string{
		"APPROVED":          "✓",
		"CHANGES_REQUESTED": "✗",
		"REVIEW_REQUIRED":   "●",
		"":                  "·",
	}
	for decision, want := range cases {
		if got := reviewDot(decision); !strings.Contains(got, want) {
			t.Errorf("reviewDot(%q) = %q, want to contain %q", decision, got, want)
		}
	}
}

func TestRenderItemRowIsSingleLine(t *testing.T) {
	o := RowOpts{Width: 80, Focused: true, Selected: true, Flag: failStyle.Render("⚠")}
	row := renderItemRow(o, accentStyle, "#7", "hello world", "alice", "2d",
		ciGlyph("fail"), reviewDot("APPROVED"), autoMergeGlyph(true), nil)
	if strings.Contains(row, "\n") {
		t.Fatalf("dense row must be one line: %q", row)
	}
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▎", "●", "⚠", autoMergeGlyph(true)} {
		if !strings.Contains(row, want) {
			t.Fatalf("row missing %q: %q", want, row)
		}
	}
}

func TestAutoMergeGlyphRendersWhenEnabled(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "hi", HeadRefName: "feat/x", State: "OPEN",
		AutoMergeRequest: &gh.AutoMergeRequest{MergeMethod: "SQUASH"}}})

	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, autoMergeGlyph(true)) {
		t.Fatalf("row missing auto-merge glyph: %q", row)
	}
}

func TestAutoMergeGlyphAbsentWhenDisabled(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "hi", HeadRefName: "feat/x"}})

	row := s.RenderRow(0, RowOpts{Width: 80})
	if strings.Contains(row, autoMergeGlyph(true)) {
		t.Fatalf("row should not show auto-merge glyph: %q", row)
	}
}

func TestPRSectionRenderRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "hello world", HeadRefName: "feat/x"}})

	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, "#7") || !strings.Contains(row, "hello world") {
		t.Fatalf("row missing number/title: %q", row)
	}

	sel := s.RenderRow(0, RowOpts{Width: 80, Selected: true})
	if !strings.Contains(sel, "●") {
		t.Fatalf("selected row should carry the ● marker: %q", sel)
	}
}

func TestSetPRsSortsByActionability(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{
		{Number: 1, IsDraft: true},
		{Number: 2, ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}},
		{Number: 3, ReviewDecision: "CHANGES_REQUESTED"},
		{Number: 4, StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}},
		{Number: 5, StatusCheckRollup: []gh.Check{{Conclusion: "IN_PROGRESS"}}},
		{Number: 6, ReviewDecision: "REVIEW_REQUIRED"},
	})
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	// ready(2) → changes(3) → fail(4) → running(5) → waiting(6) → draft(1)
	want := []int{2, 3, 4, 5, 6, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("sort order = %v, want %v", got, want)
	}
}

func TestDraftRowIsStyledDistinctly(t *testing.T) {
	args := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "alice", "2d", ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false), nil)
	}
	plain := args(RowOpts{Width: 80})
	draft := args(RowOpts{Width: 80, Draft: true})
	if plain == draft {
		t.Fatal("a draft row must render distinctly (dimmed) from a normal row")
	}
}

func TestPRSectionMarksDraftRow(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 1, Title: "wip", IsDraft: true}})
	normal := NewPRSection("")
	normal.SetPRs([]gh.PR{{Number: 1, Title: "wip"}})
	if s.RenderRow(0, RowOpts{Width: 80}) == normal.RenderRow(0, RowOpts{Width: 80}) {
		t.Fatal("PRSection.RenderRow should style a draft PR distinctly")
	}
}

func TestDraftRowShowsDraftTag(t *testing.T) {
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "alice", "2d", ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false), nil)
	}
	if got := row(RowOpts{Width: 80, Draft: true}); !strings.Contains(got, "[draft]") {
		t.Fatalf("draft row should carry a [draft] tag: %q", got)
	}
	if got := row(RowOpts{Width: 80}); strings.Contains(got, "[draft]") {
		t.Fatalf("non-draft row must not carry a [draft] tag: %q", got)
	}
}

func TestPadNumRightAligns(t *testing.T) {
	if got := padNum("#7", 5); got != "   #7" {
		t.Fatalf("padNum(#7,5) = %q, want %q", got, "   #7")
	}
	if got := padNum("#1234", 3); got != "#1234" { // never truncates below content
		t.Fatalf("padNum(#1234,3) = %q, want %q", got, "#1234")
	}
}

func TestColumnWidthsUsesWidestNumber(t *testing.T) {
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{Number: 7}, {Number: 1234}})
	if got := columnWidths(s); got != len("#1234") {
		t.Fatalf("columnWidths = %d, want %d", got, len("#1234"))
	}
}

func TestPRRankApprovedFailingIsNotReady(t *testing.T) {
	approvedFailing := gh.PR{ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}}
	approvedPassing := gh.PR{ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	if got := prRank(approvedFailing); got != rankFail {
		t.Errorf("approved+failing should rank as failing (%d), got %d", rankFail, got)
	}
	if got := prRank(approvedPassing); got != rankReady {
		t.Errorf("approved+passing should rank as ready (%d), got %d", rankReady, got)
	}
}

func TestGroupByAuthorMergedOrdersByNewestMerge(t *testing.T) {
	ts := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	a1 := gh.PR{Number: 1, State: "MERGED", MergedAt: ts("2026-07-05T00:00:00Z")}
	a1.Author.Login = "alice"
	a2 := gh.PR{Number: 2, State: "MERGED", MergedAt: ts("2026-07-03T00:00:00Z")}
	a2.Author.Login = "alice"
	b1 := gh.PR{Number: 3, State: "MERGED", MergedAt: ts("2026-07-10T00:00:00Z")}
	b1.Author.Login = "bob"

	s := NewPRSection("")
	s.SetState("merged")
	s.SetForceGroup(true) // the non-mine "all" view groups by author
	s.SetPRs([]gh.PR{a1, a2, b1})

	// bob leads (newest merge 07-10 beats alice's newest 07-05); within alice's
	// group, newest-first: #1 (07-05) then #2 (07-03).
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	if want := []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("merged group order = %v, want %v (groups by newest event, not rank)", got, want)
	}
}

func TestSetShownOrderedGroupsByAuthorWhenMultiple(t *testing.T) {
	a := gh.PR{Number: 1, ReviewDecision: "REVIEW_REQUIRED"} // alice, rank waiting
	a.Author.Login = "alice"
	b := gh.PR{Number: 2, ReviewDecision: "APPROVED", // bob, rank ready
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	b.Author.Login = "bob"
	a2 := gh.PR{Number: 3, ReviewDecision: "CHANGES_REQUESTED"} // alice, rank changes
	a2.Author.Login = "alice"

	s := NewPRSection("")
	s.SetPRs([]gh.PR{a, b, a2})

	if !s.grouped {
		t.Fatal("two distinct authors should switch the section to grouped mode")
	}
	// bob's group leads (its best rank, ready=0, beats alice's best, changes=1).
	// within alice's group, changes(#3) precedes waiting(#1).
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	want := []int{2, 3, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped display order = %v, want %v", got, want)
	}
}

func TestSetShownOrderedFlatWhenSingleAuthor(t *testing.T) {
	p1 := gh.PR{Number: 1, ReviewDecision: "APPROVED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	p1.Author.Login = "alice"
	p2 := gh.PR{Number: 2, ReviewDecision: "REVIEW_REQUIRED"}
	p2.Author.Login = "alice"

	s := NewPRSection("")
	s.SetPRs([]gh.PR{p2, p1}) // unsorted input

	if s.grouped {
		t.Fatal("a single distinct author must stay flat (not grouped)")
	}
	// flat actionability order: ready(#1) before waiting(#2)
	if s.prAt(0).Number != 1 || s.prAt(1).Number != 2 {
		t.Fatalf("flat order = [%d %d], want [1 2]", s.prAt(0).Number, s.prAt(1).Number)
	}
}

func TestPRRowOmitsInlineAuthor(t *testing.T) {
	p := gh.PR{Number: 1, Title: "do the thing"}
	p.Author.Login = "alice"
	s := NewPRSection("")
	s.SetPRs([]gh.PR{p})
	if row := s.RenderRow(0, RowOpts{Width: 80}); strings.Contains(row, "alice") {
		t.Fatalf("PR row must not render the author inline (it lives in the header): %q", row)
	}
}

func TestMergedPRShowsMergeMarkNotCI(t *testing.T) {
	// A merged PR with a passing rollup must show the merge mark, not the ✓.
	p := gh.PR{Number: 9, Title: "landed", State: "MERGED",
		StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}
	s := NewPRSection("")
	s.SetPRs([]gh.PR{p})
	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, mergedGlyph) {
		t.Fatalf("merged PR row should carry the merge mark %q: %q", mergedGlyph, row)
	}
	if strings.Contains(row, "✓") {
		t.Fatalf("merged PR row should not show the CI pass glyph: %q", row)
	}
}

func TestClosedPRShowsDimClosedMarkNotCI(t *testing.T) {
	// A closed (unmerged) PR whose last CI run failed must show the closed mark,
	// not a red CI ✗, and not the merge mark.
	p := gh.PR{Number: 9, Title: "abandoned", State: "CLOSED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	s := NewPRSection("")
	s.SetState("closed")
	s.SetPRs([]gh.PR{p})
	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, closedMark()) {
		t.Fatalf("closed PR row should carry the dim closed mark: %q", row)
	}
	if strings.Contains(row, mergedGlyph) {
		t.Fatalf("closed PR must not show the merge mark: %q", row)
	}
}

func TestMergedPRInClosedViewRendersFromOwnState(t *testing.T) {
	mrg, _ := time.Parse(time.RFC3339, "2026-07-12T00:00:00Z")
	cls, _ := time.Parse(time.RFC3339, "2026-07-01T00:00:00Z") // deliberately != MergedAt
	p := gh.PR{Number: 5, Title: "landed", State: "MERGED", MergedAt: mrg, ClosedAt: cls}
	s := NewPRSection("")
	s.SetState("closed") // view is "closed", but the row is a merged PR
	s.SetPRs([]gh.PR{p})
	row := s.RenderRow(0, RowOpts{Width: 80})
	if !strings.Contains(row, mergedGlyph) {
		t.Fatalf("merged PR must show the merge mark even in the closed view: %q", row)
	}
	if want := ageString(mrg); !strings.Contains(row, want) {
		t.Fatalf("age must come from MergedAt (%q), not ClosedAt: %q", want, row)
	}
}

func TestRowTimeReflectsPRState(t *testing.T) {
	upd, _ := time.Parse(time.RFC3339, "2026-07-01T00:00:00Z")
	mrg, _ := time.Parse(time.RFC3339, "2026-07-12T00:00:00Z") // ~1d before "now"-ish
	merged := gh.PR{Number: 1, Title: "landed", State: "MERGED", UpdatedAt: upd, MergedAt: mrg}
	open := gh.PR{Number: 2, Title: "wip", State: "OPEN", UpdatedAt: mrg}

	s := NewPRSection("")
	s.SetState("merged")
	s.SetPRs([]gh.PR{merged})
	mergedRow := s.RenderRow(0, RowOpts{Width: 80})

	so := NewPRSection("")
	so.SetState("open")
	so.SetPRs([]gh.PR{open})
	openRow := so.RenderRow(0, RowOpts{Width: 80})

	// Both events are the same instant (mrg), so both rows show the same age string;
	// the merged row must derive it from MergedAt, not its (much older) UpdatedAt.
	if want := ageString(mrg); !strings.Contains(mergedRow, want) {
		t.Fatalf("merged row age should come from MergedAt (%q): %q", want, mergedRow)
	}
	if want := ageString(mrg); !strings.Contains(openRow, want) {
		t.Fatalf("open row age should come from UpdatedAt (%q): %q", want, openRow)
	}
}

func TestIssueRowKeepsInlineAuthor(t *testing.T) {
	is := gh.Issue{Number: 1, Title: "bug"}
	is.Author.Login = "carol"
	s := NewIssueSection("")
	s.SetIssues([]gh.Issue{is})
	if row := s.RenderRow(0, RowOpts{Width: 80}); !strings.Contains(row, "carol") {
		t.Fatalf("issue row should still show its author: %q", row)
	}
}

func TestGroupHeaderShowsAuthorAndRule(t *testing.T) {
	h := groupHeader("alice", 40)
	if !strings.Contains(h, "alice") {
		t.Fatalf("group header should name the author: %q", h)
	}
	if !strings.Contains(h, "─") {
		t.Fatalf("group header should draw a rule: %q", h)
	}
	if strings.Contains(h, "\n") {
		t.Fatalf("group header must be a single line: %q", h)
	}
}

func TestFocusedRowGetsBackground(t *testing.T) {
	// the exact bg-open sequence lipgloss emits for RowBg under the active profile
	probe := lipgloss.NewStyle().Background(lipgloss.Color(theme.RowBg)).Render("X")
	set := probe[:strings.Index(probe, "X")]
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "", "2d", ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false), nil)
	}
	if got := row(RowOpts{Width: 80, Focused: true}); !strings.Contains(got, set) {
		t.Fatalf("focused row should carry the cursor background: %q", got)
	}
	if got := row(RowOpts{Width: 80}); strings.Contains(got, set) {
		t.Fatalf("unfocused row must not carry a background: %q", got)
	}
}

func TestSetHideDraftsExcludesDrafts(t *testing.T) {
	d := gh.PR{Number: 1, IsDraft: true}
	d.Author.Login = "alice"
	r := gh.PR{Number: 2}
	r.Author.Login = "alice"
	s := NewPRSection("")
	s.SetPRs([]gh.PR{d, r})
	if s.Len() != 2 {
		t.Fatalf("both PRs shown before hiding drafts, got %d", s.Len())
	}
	s.SetHideDrafts(true)
	s.SetShown([]int{0, 1}) // re-evaluate the shown set with the flag on
	if s.Len() != 1 {
		t.Fatalf("draft should be excluded, got %d", s.Len())
	}
	if s.prAt(0).Number != 2 {
		t.Fatalf("remaining row should be the non-draft #2, got #%d", s.prAt(0).Number)
	}
}

func TestSetPRsMergedSortsByMergeTime(t *testing.T) {
	mk := func(num int, merged string) gh.PR {
		ts, _ := time.Parse(time.RFC3339, merged)
		return gh.PR{Number: num, State: "MERGED", MergedAt: ts,
			// deliberately varied CI/review so rank order would differ from time order
			ReviewDecision: "APPROVED", StatusCheckRollup: []gh.Check{{Conclusion: "FAILURE"}}}
	}
	s := NewPRSection("")
	s.SetState("merged")
	s.SetPRs([]gh.PR{
		mk(1, "2026-07-10T09:00:00Z"),
		mk(2, "2026-07-12T09:00:00Z"),
		mk(3, "2026-07-11T09:00:00Z"),
	})
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	if want := []int{2, 3, 1}; !slices.Equal(got, want) {
		t.Fatalf("merged sort = %v, want newest-merge-first %v", got, want)
	}
}

func TestSetPRsClosedSortsByCloseTime(t *testing.T) {
	mk := func(num int, closed string) gh.PR {
		ts, _ := time.Parse(time.RFC3339, closed)
		return gh.PR{Number: num, State: "CLOSED", ClosedAt: ts}
	}
	s := NewPRSection("")
	s.SetState("closed")
	s.SetPRs([]gh.PR{
		mk(1, "2026-07-10T09:00:00Z"),
		mk(2, "2026-07-12T09:00:00Z"),
	})
	if s.prAt(0).Number != 2 {
		t.Fatalf("closed sort should lead with newest close #2, got #%d", s.prAt(0).Number)
	}
}

func TestSetForceFlatSkipsGrouping(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetState("open")
	s.SetCategorized([]gh.PR{
		{Number: 1, Author: author("a")},
		{Number: 2, Author: author("b")},
	}, map[int]string{1: "Mine", 2: "Others"}, []string{"Mine", "Others"})
	s.SetForceFlat(true)
	s.SetShown([]int{1, 0}) // fuzzy rank: #2 before #1
	if s.grouped {
		t.Fatal("grouped should be false under SetForceFlat")
	}
	if s.prAt(0).Number != 2 || s.prAt(1).Number != 1 {
		t.Fatalf("order not preserved: %d,%d", s.prAt(0).Number, s.prAt(1).Number)
	}
}

// labeledPR carries several chips including one with an empty color (exercises
// labelChip's fallback) and enough labels to force a "+N" overflow at a bounded
// budget.
func labeledPR() gh.PR {
	p := gh.PR{Number: 42, Title: "wire up the responsive rail"}
	p.Author.Login = "al"
	p.Labels = []gh.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "ui", Color: ""}, // empty color → labelChip fallback path
		{Name: "backend", Color: "0e8a16"},
		{Name: "needs-review", Color: "fbca04"},
		{Name: "priority", Color: "5319e7"},
	}
	return p
}

func TestListRowChipsAppearOnWideRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	row := s.RenderRow(0, RowOpts{Width: 160, NumWidth: nw})
	if strings.Contains(row, "\n") {
		t.Fatalf("row must be one line: %q", row)
	}
	if lipgloss.Width(row) != 160 {
		t.Errorf("wide labeled row width = %d, want 160", lipgloss.Width(row))
	}
	plain := ansi.Strip(row)
	if !strings.Contains(plain, "bug") {
		t.Fatalf("expected a chip label on a wide row: %q", plain)
	}
}

func TestListRowChipsForceOverflowPlusN(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	// Wide enough to show chips, tight enough that the bounded budget cannot fit
	// all five labels → a dim "+N" overflow must appear.
	row := s.RenderRow(0, RowOpts{Width: 96, NumWidth: nw})
	if lipgloss.Width(row) != 96 {
		t.Errorf("labeled row width = %d, want 96", lipgloss.Width(row))
	}
	if plain := ansi.Strip(row); !strings.Contains(plain, "+") {
		t.Fatalf("expected a +N overflow marker: %q", plain)
	}
}

func TestListRowChipsAbsentOnNarrowRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	row := s.RenderRow(0, RowOpts{Width: 60, NumWidth: nw})
	if lipgloss.Width(row) != 60 {
		t.Errorf("narrow row width = %d, want 60 (exact-fill must hold with no chips)", lipgloss.Width(row))
	}
	if plain := ansi.Strip(row); !strings.Contains(plain, "wire up") {
		t.Fatalf("title must survive intact when chips are dropped: %q", plain)
	}
}

func TestFocusedLabeledRowIsExactFillSingleLine(t *testing.T) {
	// Focused rows run through rowBgWrap, which re-injects the row background
	// after every SGR reset, while each chip carries its own labelChip Background.
	// This guards against a per-chip-bg vs row-bg refill bug that a width-only
	// check on an unfocused row would miss.
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	for _, w := range []int{96, 120, 160, 200} {
		row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: true})
		if strings.Contains(row, "\n") {
			t.Fatalf("w=%d focused labeled row must be one line: %q", w, row)
		}
		if got := lipgloss.Width(row); got != w {
			t.Errorf("w=%d focused labeled row width = %d, want %d", w, got, w)
		}
	}
}

// TestListRowCJKTitleWithChipsExactFill guards the exact-fill invariant for a
// wide-cell (CJK) title once chips shrink the title budget. Each CJK glyph is 2
// cells, so a rune-count truncation (rather than cell-count) would let the title
// overflow the row — the bug this pins.
func TestListRowCJKTitleWithChipsExactFill(t *testing.T) {
	p := labeledPR()
	// 30 CJK glyphs = 60 display cells; long enough to need truncation once the
	// chip budget is carved out at every swept width.
	p.Title = strings.Repeat("重", 30)
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{p})
	nw := columnWidths(s)
	for _, w := range []int{72, 80, 96, 120, 160} {
		for _, foc := range []bool{false, true} {
			row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: foc})
			if strings.Contains(row, "\n") {
				t.Fatalf("w=%d foc=%v CJK+chips row must be one line: %q", w, foc, row)
			}
			if got := lipgloss.Width(row); got != w {
				t.Errorf("w=%d foc=%v CJK+chips row width = %d, want %d", w, foc, got, w)
			}
		}
	}
}

// TestListRowChipsTransitionAtMinWidth pins the chip appear/disappear boundary
// at chipRowMinWidth so the transition is as tightly held as the two-col cutoff.
func TestListRowChipsTransitionAtMinWidth(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	below := ansi.Strip(s.RenderRow(0, RowOpts{Width: chipRowMinWidth - 1, NumWidth: nw}))
	if strings.Contains(below, "bug") {
		t.Errorf("no chips expected just below chipRowMinWidth: %q", below)
	}
	// A generous width above the threshold reliably shows chips.
	above := ansi.Strip(s.RenderRow(0, RowOpts{Width: chipRowMinWidth + 48, NumWidth: nw}))
	if !strings.Contains(above, "bug") {
		t.Errorf("chips expected above chipRowMinWidth: %q", above)
	}
}

// TestRenderChipsNeverExceedsMaxW brute-forces the width contract: the rendered
// chip string (including any "+N" suffix) must never exceed maxW. The expanded
// rail clamps a frame to this width, so an overshoot would wrap and overflow.
func TestRenderChipsNeverExceedsMaxW(t *testing.T) {
	labels := labeledPR().Labels
	for maxW := 3; maxW <= 60; maxW++ {
		if got := lipgloss.Width(renderChips(labels, maxW)); got > maxW {
			t.Errorf("renderChips width %d exceeds maxW %d", got, maxW)
		}
	}
}
