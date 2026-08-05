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
		"APPROVED":          reviewApprovedGlyph,
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
	row := renderItemRow(o, accentStyle, "#7", "hello world", "", "alice", "2d", "",
		ciGlyph("fail"), reviewDot("APPROVED"), autoMergeGlyph(true))
	if strings.Contains(row, "\n") {
		t.Fatalf("dense row must be one line: %q", row)
	}
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▌", "⚠", autoMergeGlyph(true)} {
		if !strings.Contains(row, want) {
			t.Fatalf("row missing %q: %q", want, row)
		}
	}
}

func TestRowIsAlwaysOneLine(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{
		Number: 3065, Title: "feat(infra): add deploy-time SpiceDB migrator",
		HeadRefName: "eng-7726-thing", State: "OPEN",
		Labels: []gh.Label{{Name: "complexity:6", Color: "fab387"}, {Name: "preview:full", Color: "a6adc8"}},
	}})
	s.prs[0].Author.Login = "asaf-s-factify"
	s.SetShown([]int{0})

	row := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if strings.Contains(row, "\n") {
		t.Fatalf("row spans multiple lines:\n%s", row)
	}
	if strings.Contains(row, "complexity:6") {
		t.Error("labels must not appear on the row — they belong to the preview")
	}
	if strings.Contains(row, "eng-7726-thing") {
		t.Error("head branch must not appear on the row — it belongs to the preview")
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
	if !strings.Contains(sel, "▌") {
		t.Fatalf("selected row should carry the ▌ bar: %q", sel)
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

func TestDraftOverridesCIGlyphNotTitle(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{
		Number: 3083, Title: "fix(services): canonicalize CLW JSON values",
		State: "OPEN", IsDraft: true,
		StatusCheckRollup: []gh.Check{{Name: "build", State: "SUCCESS", Conclusion: "SUCCESS"}},
	}})
	s.prs[0].Author.Login = "noamsto"
	s.SetShown([]int{0})

	row := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if strings.Contains(row, "[draft]") {
		t.Error("draft must not render as a trailing tag")
	}
	if !strings.Contains(row, draftGlyph) {
		t.Errorf("draft glyph missing from the gutter:\n%s", row)
	}
	// The draft glyph replaces the CI mark rather than joining it.
	if strings.Contains(row, ciGlyph("pass")) {
		t.Error("draft row still shows a CI glyph; it should be overridden")
	}
}

func TestTerminalStateOverridesDraftGlyph(t *testing.T) {
	// Draft is checked last in the switch so a merged or closed draft shows
	// the terminal glyph, not the draft glyph. This test guards against
	// accidental reordering of the switch cases.
	mrg, _ := time.Parse(time.RFC3339, "2026-07-12T00:00:00Z")
	cls, _ := time.Parse(time.RFC3339, "2026-07-01T00:00:00Z")

	// Test 1: draft + merged → merged glyph, no draft glyph
	s := NewPRSection("")
	s.SetPRs([]gh.PR{{
		Number: 1, Title: "merged draft", State: "MERGED", IsDraft: true,
		MergedAt: mrg, ClosedAt: mrg,
	}})
	s.prs[0].Author.Login = "alice"
	s.SetShown([]int{0})

	row := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if !strings.Contains(row, mergedGlyph) {
		t.Errorf("merged draft should show mergedGlyph %q: %s", mergedGlyph, row)
	}
	if strings.Contains(row, draftGlyph) {
		t.Errorf("merged draft must not show draftGlyph %q: %s", draftGlyph, row)
	}

	// Test 2: draft + closed → closed glyph, no draft glyph
	s = NewPRSection("")
	s.SetPRs([]gh.PR{{
		Number: 2, Title: "closed draft", State: "CLOSED", IsDraft: true,
		ClosedAt: cls,
	}})
	s.prs[0].Author.Login = "bob"
	s.SetShown([]int{0})

	row = s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if !strings.Contains(row, closedGlyph) {
		t.Errorf("closed draft should show closedGlyph %q: %s", closedGlyph, row)
	}
	if strings.Contains(row, draftGlyph) {
		t.Errorf("closed draft must not show draftGlyph %q: %s", draftGlyph, row)
	}
}

func TestDraftRowIsStyledDistinctly(t *testing.T) {
	args := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "", "alice", "2d", "", ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false))
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

func TestPRRowShowsAuthor(t *testing.T) {
	p := gh.PR{Number: 1, Title: "do the thing"}
	p.Author.Login = "alice"
	p.HeadRefName = "feat/x"
	s := NewPRSection("")
	s.SetPRs([]gh.PR{p})
	row := s.RenderRow(0, RowOpts{Width: 100, NumWidth: columnWidths(s)})
	if strings.Contains(row, "\n") {
		t.Fatalf("PR row must be a single line: %q", row)
	}
	if !strings.Contains(ansi.Strip(row), "alice") {
		t.Errorf("author must appear on the row: %q", ansi.Strip(row))
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
		return renderItemRow(o, accentStyle, "#1", "title", "", "", "2d", "", ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false))
	}
	if got := row(RowOpts{Width: 80, Focused: true}); !strings.Contains(got, set) {
		t.Fatalf("focused row should carry the cursor background: %q", got)
	}
	if got := row(RowOpts{Width: 80}); strings.Contains(got, set) {
		t.Fatalf("unfocused row must not carry a background: %q", got)
	}
	if got := row(RowOpts{Width: 80, Focused: true, Selected: true}); !strings.Contains(got, set) {
		t.Fatalf("focused+selected row should keep the cursor background: %q", got)
	}
	if got := row(RowOpts{Width: 80, Selected: true}); strings.Contains(got, set) {
		t.Fatalf("selected-but-unfocused row must not carry a background: %q", got)
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

// TestRenderRowSingleLineHasNoChips: default (single-line) rows drop labels
// entirely and stay one line at the full width.
func TestRenderRowSingleLineHasNoChips(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	for _, w := range []int{72, 96, 120, 160} {
		row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw})
		if strings.Contains(row, "\n") {
			t.Fatalf("w=%d single-line row must be one line: %q", w, row)
		}
		if strings.Contains(ansi.Strip(row), "bug") {
			t.Errorf("w=%d single-line row must not show chips: %q", w, ansi.Strip(row))
		}
		if got := lipgloss.Width(row); got != w {
			t.Errorf("w=%d single-line row width = %d, want %d", w, got, w)
		}
	}
}

func TestIssueRowIsSingleLine(t *testing.T) {
	s := NewIssueSection("is:open")
	s.SetIssues([]gh.Issue{{Number: 5, Title: "no labels here"}})
	row := s.RenderRow(0, RowOpts{Width: 100, NumWidth: 4})
	if strings.Contains(row, "\n") {
		t.Fatalf("issue row must be a single line: %q", row)
	}
}

func TestFocusedLabeledRowIsExactFillSingleLine(t *testing.T) {
	// Focused rows run through rowBgWrap, which re-injects the row background
	// after every SGR reset. Labels don't render on a single-line row, but the
	// fixture still exercises the focused-row-bg refill path at exact-fill widths.
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

// TestListRowCJKTitleExactFillSingleLine guards the exact-fill invariant for a
// wide-cell (CJK) title on a single-line row. Each CJK glyph is 2 cells, so a
// rune-count truncation (rather than cell-count) would let the title overflow
// the row — the bug this pins.
func TestListRowCJKTitleExactFillSingleLine(t *testing.T) {
	p := labeledPR()
	// 30 CJK glyphs = 60 display cells; long enough to need truncation at every
	// swept width.
	p.Title = strings.Repeat("重", 30)
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{p})
	nw := columnWidths(s)
	for _, w := range []int{72, 80, 96, 120, 160} {
		for _, foc := range []bool{false, true} {
			row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: foc})
			if strings.Contains(row, "\n") {
				t.Fatalf("w=%d foc=%v CJK row must be one line: %q", w, foc, row)
			}
			if got := lipgloss.Width(row); got != w {
				t.Errorf("w=%d foc=%v CJK row width = %d, want %d", w, foc, got, w)
			}
		}
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

// TestRowColumnsAlignAcrossStates pins the column grid: the #number must start
// at the same cell column for every combination of conflict flag, focus,
// selection, and review decision. A glyph whose rendered width disagrees with
// lipgloss (U+26A0, with or without a VS15 selector, is the classic offender)
// shifts one variant and fails here.
func TestRowColumnsAlignAcrossStates(t *testing.T) {
	cell := func(s, sub string) int {
		b := strings.Index(s, sub)
		if b < 0 {
			t.Fatalf("%q not found in %q", sub, s)
		}
		return lipgloss.Width(s[:b])
	}
	want := -1
	for _, rev := range []string{"APPROVED", "CHANGES_REQUESTED", "REVIEW_REQUIRED", ""} {
		for _, flag := range []string{"", failStyle.Render(warnGlyph)} {
			for _, focused := range []bool{false, true} {
				for _, selected := range []bool{false, true} {
					p := gh.PR{Number: 2959, Title: "t", State: "OPEN", HeadRefName: "br", ReviewDecision: rev}
					p.Author.Login = "alice"
					p.Labels = []gh.Label{{Name: "lbl", Color: "e08a2b"}}
					s := NewPRSection("")
					s.SetPRs([]gh.PR{p})
					row := ansi.Strip(s.RenderRow(0, RowOpts{Width: 90, NumWidth: 5, Flag: flag, Focused: focused, Selected: selected}))
					if strings.Contains(row, "\n") {
						t.Fatalf("rev=%q flag=%v focused=%v selected=%v: want 1 line, got %q", rev, flag != "", focused, selected, row)
					}
					numCol := cell(row, "#2959")
					if want == -1 {
						want = numCol
					} else if numCol != want {
						t.Errorf("rev=%q flag=%v focused=%v selected=%v: #number at col %d, want %d (column grid drifted)",
							rev, flag != "", focused, selected, numCol, want)
					}
				}
			}
		}
	}
}

// TestGutterSurvivesZeroWidthMarker: a marker that is a non-empty string but
// renders zero-width (a styled empty glyph const — how autoMergeGlyphRune once
// shipped) must still occupy its cell. Otherwise that row's #number drifts one
// cell out of the column grid.
func TestGutterSurvivesZeroWidthMarker(t *testing.T) {
	numCol := func(row string) int {
		line := strings.Split(ansi.Strip(row), "\n")[0]
		b := strings.Index(line, "#7")
		if b < 0 {
			t.Fatalf("#7 not found in %q", line)
		}
		return lipgloss.Width(line[:b])
	}
	base := renderItemRow(RowOpts{Width: 80, NumWidth: 3}, accentStyle, "#7", "t", "", "", "2d", "",
		ciGlyph("pass"), reviewDot(""), "")
	styledEmpty := renderItemRow(RowOpts{Width: 80, NumWidth: 3}, accentStyle, "#7", "t", "", "", "2d", "",
		ciGlyph("pass"), reviewDot(""), mergedStyle.Render(""))
	if lipgloss.Width(mergedStyle.Render("")) != 0 {
		t.Skip("styled empty string is not zero-width in this lipgloss build")
	}
	if got, want := numCol(styledEmpty), numCol(base); got != want {
		t.Errorf("zero-width auto marker collapsed the gutter: #number at col %d, want %d", got, want)
	}
}

// TestSelectedBarWinsOverFocusBar: the row has one bar cell and two states that
// want it. Selection takes it — it is what an action fires against — and focus
// falls back to the row background and bold title (TestFocusedRowGetsBackground).
func TestSelectedBarWinsOverFocusBar(t *testing.T) {
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "", "", "2d", "",
			ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false))
	}
	cases := []struct {
		name          string
		o             RowOpts
		want, notWant string
	}{
		{"selected", RowOpts{Width: 80, Selected: true}, "▌", "▎"},
		{"focused and selected", RowOpts{Width: 80, Focused: true, Selected: true}, "▌", "▎"},
		{"focused", RowOpts{Width: 80, Focused: true}, "▎", "▌"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := row(c.o)
			if !strings.Contains(got, c.want) {
				t.Errorf("want bar %q: %q", c.want, got)
			}
			if strings.Contains(got, c.notWant) {
				t.Errorf("must not carry bar %q: %q", c.notWant, got)
			}
		})
	}
	if got := row(RowOpts{Width: 80}); strings.Contains(got, "▌") || strings.Contains(got, "▎") {
		t.Errorf("a row that is neither focused nor selected must have no bar: %q", got)
	}
}

// TestSelectionDoesNotShiftColumnGrid: the bar cell is always exactly one cell
// wide, so toggling focus or selection must never move the #number. The old ●
// lived in a second, dedicated column — this pins the grid now that it is gone.
func TestSelectionDoesNotShiftColumnGrid(t *testing.T) {
	numCol := func(row string) int {
		line := strings.Split(ansi.Strip(row), "\n")[0]
		b := strings.Index(line, "#7")
		if b < 0 {
			t.Fatalf("#7 not found in %q", line)
		}
		return lipgloss.Width(line[:b])
	}
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#7", "t", "", "", "2d", "",
			ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false))
	}
	want := numCol(row(RowOpts{Width: 80, NumWidth: 3}))
	for _, o := range []RowOpts{
		{Width: 80, NumWidth: 3, Selected: true},
		{Width: 80, NumWidth: 3, Focused: true},
		{Width: 80, NumWidth: 3, Focused: true, Selected: true},
	} {
		if got := numCol(row(o)); got != want {
			t.Errorf("focused=%v selected=%v: #number at col %d, want %d",
				o.Focused, o.Selected, got, want)
		}
	}
}

func TestAuthorHueUsesFullLoginNotTruncated(t *testing.T) {
	// GitHub allows logins up to 39 characters. Verify that authorStyle hashes
	// the full login, not the truncated display text. A long login that gets
	// truncated in the row (due to narrow width) must still render with the hue
	// derived from the full login.

	// 39-character login (GitHub's max).
	fullLogin := "this-is-a-very-long-39-character-logins"
	if len(fullLogin) != 39 {
		t.Fatalf("test login must be 39 chars, got %d", len(fullLogin))
	}

	// Render with Width: 50, which will truncate the login to ~12 chars.
	o := RowOpts{Width: 50, NumWidth: 3}
	row := renderItemRow(o, accentStyle, "#123", "some title", "", fullLogin, "2d", "",
		ciGlyph("success"), reviewDot("APPROVED"), autoMergeGlyph(true))

	// Extract the SGR color code from authorStyle(fullLogin).
	// This is the Foreground color sequence that identifies this author.
	styledAuthor := authorStyle(fullLogin).Render("x")
	expectedCode := extractLeadingSGRPrefix(styledAuthor)

	if expectedCode == "" {
		t.Fatalf("failed to extract SGR code from authorStyle(%q)", fullLogin)
	}

	if !strings.Contains(row, expectedCode) {
		t.Errorf("row does not contain expected SGR code for author %q\n"+
			"row output:\n%q\n\n"+
			"expected SGR prefix:\n%q",
			fullLogin, row, expectedCode)
	}
}

// extractLeadingSGRPrefix extracts the leading ANSI escape sequence (SGR code)
// from a styled string. For example, from "\x1b[38;5;123mx" it returns "\x1b[38;5;123m".
func extractLeadingSGRPrefix(s string) string {
	// SGR sequences start with ESC[ and end with 'm'
	const esc = "\x1b["
	if !strings.HasPrefix(s, esc) {
		return ""
	}
	end := strings.Index(s[len(esc):], "m")
	if end == -1 {
		return ""
	}
	return s[:len(esc)+end+1]
}

func stripANSIForTest(s string) string { return ansi.Strip(s) }

// cellOffset finds sub's starting column in row, in display cells rather than
// bytes: row prefixes carry multi-byte glyphs (box-drawing runes are 3 bytes
// but 1 cell), so a byte index disagrees with the visual column.
func cellOffset(t *testing.T, row, sub string) int {
	t.Helper()
	plain := stripANSIForTest(row)
	i := strings.Index(plain, sub)
	if i < 0 {
		t.Fatalf("%q not found in %q", sub, plain)
	}
	return lipgloss.Width(plain[:i])
}

func TestDiffstatFormatting(t *testing.T) {
	for _, tc := range []struct {
		add, del int
		want     string
	}{
		{412, 18, "+412 -18"},
		{0, 0, "+0 -0"},
		{1600, 63, "+1.6k -63"},
		{1000, 999, "+1k -999"},
		{12345, 2000, "+12.3k -2k"},
	} {
		got := stripANSIForTest(diffstat(tc.add, tc.del))
		if got != tc.want {
			t.Errorf("diffstat(%d,%d) = %q, want %q", tc.add, tc.del, got, tc.want)
		}
	}
}

func TestDiffstatColumnWidthIsStableAcrossRows(t *testing.T) {
	s := NewPRSection("is:open")
	now := time.Now()
	s.SetPRs([]gh.PR{
		// Non-zero and distinct: the original bug fixture left UpdatedAt unset, so
		// ageString (section.go) returned "" for both rows and the age assertion
		// below could never fire. 5h/3d sit well inside ageString's hour/day
		// buckets, so neither flips to a different unit while the test runs, and
		// both fit the age field's 3-cell width so the suffix stays a fixed 5
		// cells (see diffColWidth below).
		{Number: 3087, Title: "big", State: "OPEN", Additions: 1600, Deletions: 63, UpdatedAt: now.Add(-5 * time.Hour)},
		{Number: 3084, Title: "small", State: "OPEN", Additions: 31, Deletions: 4, UpdatedAt: now.Add(-3 * 24 * time.Hour)},
	})
	s.prs[0].Author.Login = "noamsto"
	s.prs[1].Author.Login = "rubytify"
	s.SetShown([]int{0, 1})

	dw := diffstatWidth(s)
	a := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5, DiffWidth: dw})
	b := s.RenderRow(1, RowOpts{Width: 120, NumWidth: 5, DiffWidth: dw})
	if lipgloss.Width(a) != lipgloss.Width(b) {
		t.Fatalf("rows differ in width: %d vs %d", lipgloss.Width(a), lipgloss.Width(b))
	}
	// A same-cell check on the age column itself (e.g. cellOffset(a, "5h") vs
	// cellOffset(b, "3d")) cannot fail: the age suffix is a fixed 5 cells
	// ("  " + a right-aligned 3-char field) glued to the very end of the row,
	// and renderItemRow's trailing gap absorbs any diffstat-width error to hold
	// the row at the exact-fill width above regardless — confirmed by breaking
	// diffstatWidth (see commit message) and watching that check keep passing.
	// The assertion with teeth is on the diffstat column itself: the cells
	// between the end of the author login and that fixed suffix must equal dw
	// for every row, wide diffstat or narrow.
	diffColWidth := func(row, login string) int {
		authorEnd := cellOffset(t, row, login) + lipgloss.Width(login)
		return lipgloss.Width(stripANSIForTest(row)) - 5 - authorEnd - 2 // -5 age suffix, -2 "  " separator
	}
	if got := diffColWidth(a, "noamsto"); got != dw {
		t.Errorf("wide-diffstat row: diffstat column is %d cells, want dw=%d", got, dw)
	}
	if got := diffColWidth(b, "rubytify"); got != dw {
		t.Errorf("narrow-diffstat row: diffstat column is %d cells, want dw=%d", got, dw)
	}
}

// TestDiffstatWiderThanItsColumnStillHoldsWidth pins renderItemRow's clamp of
// the diffstat to DiffWidth. renderList can't produce this pairing — one pass
// sizes DiffWidth from every shown row — so the case is constructed directly:
// without the clamp the diffstat renders at natural width, inflates rightW past
// its budget, and the row runs 6 cells past w.
func TestDiffstatWiderThanItsColumnStillHoldsWidth(t *testing.T) {
	diff := diffstat(412, 18)
	if got := lipgloss.Width(diff); got != 8 {
		t.Fatalf("fixture diffstat is %d cells, want 8 (test would not exceed the column below)", got)
	}
	for dw := 1; dw < 8; dw++ {
		for w := 30; w <= 120; w++ {
			o := RowOpts{Width: w, NumWidth: 5, DiffWidth: dw}
			row := renderItemRow(o, accentStyle, "#3087", "a title long enough to be truncated", "",
				"noamsto-dev", "2d", diff, ciGlyph("success"), reviewDot("APPROVED"), autoMergeGlyph(true))
			if got := lipgloss.Width(row); got != w {
				t.Fatalf("DiffWidth=%d w=%d: row width %d, want exactly %d", dw, w, got, w)
			}
		}
	}
}

func TestTicketColumnSitsAfterTitleAndBlanksDontShiftAge(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{
		{Number: 3087, Title: "has a ticket", State: "OPEN", HeadRefName: "eng-7726-x"},
		{Number: 3065, Title: "has none", State: "OPEN", HeadRefName: "agents/spicedb-rel-migrate-88ee"},
	})
	// Both PRs tie on actionability rank, so groupByAuthor breaks the tie
	// alphabetically: the login assigned to prs[0] must sort first, or its
	// group — and RenderRow(0) — lands on prs[1] instead.
	s.prs[0].Author.Login = "asaf-s-factify"
	s.prs[1].Author.Login = "noamsto"
	s.SetShown([]int{0, 1})

	tw := ticketWidth(s)
	if tw < len("ENG-7726") {
		t.Fatalf("ticketWidth = %d, too narrow for ENG-7726", tw)
	}
	withID := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})
	without := s.RenderRow(1, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})

	if !strings.Contains(stripANSIForTest(withID), "ENG-7726") {
		t.Errorf("ticket id missing:\n%s", withID)
	}
	// The id follows the title, so the number stays hard against the gutter.
	plain := stripANSIForTest(withID)
	if strings.Index(plain, "ENG-7726") < strings.Index(plain, "has a ticket") {
		t.Error("ticket id renders before the title; it must follow it")
	}
	if lipgloss.Width(withID) != lipgloss.Width(without) {
		t.Error("a blank ticket changes the row width")
	}
}

// TestTicketColumnWidthIsStableAcrossRows pins the ticket column's own cell
// width rather than its offset from the row's right edge: exact-fill means
// the trailing gap absorbs any error in that width and holds every row at
// Width regardless, so a right-anchored check alone can't catch a wrong
// TicketWidth (confirmed by breaking ticketWidth to return a per-row width
// and watching this check, not the total-width one, fail).
func TestTicketColumnWidthIsStableAcrossRows(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{
		{Number: 3087, Title: "short id", State: "OPEN", HeadRefName: "fix/213-x"},
		{Number: 3065, Title: "long id", State: "OPEN", HeadRefName: "eng-7726-y"},
	})
	// Same tie-break as above: prs[0]'s login must sort first alphabetically.
	s.prs[0].Author.Login = "asaf-s-factify"
	s.prs[1].Author.Login = "noamsto"
	s.SetShown([]int{0, 1})

	tw := ticketWidth(s)
	a := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})
	b := s.RenderRow(1, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})

	// The ticket field is left-aligned: ticket text, then pad to TicketWidth,
	// then the "  " separator, then the author. So the cells between where the
	// ticket text starts and where the author starts, minus that separator, is
	// the column's own width — independent of the row's total width.
	ticketColWidth := func(row, ticket, login string) int {
		return cellOffset(t, row, login) - cellOffset(t, row, ticket) - 2
	}
	if got := ticketColWidth(a, "#213", "asaf-s-factify"); got != tw {
		t.Errorf("short-id row: ticket column is %d cells, want tw=%d", got, tw)
	}
	if got := ticketColWidth(b, "ENG-7726", "noamsto"); got != tw {
		t.Errorf("long-id row: ticket column is %d cells, want tw=%d", got, tw)
	}
}
