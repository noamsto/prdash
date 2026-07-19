package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

func TestJumpTabIndex(t *testing.T) {
	cases := map[string]int{"reviews": 1, "checks": 2, "diff": 3, "conversation": 0, "": 0}
	for jump, want := range cases {
		if got := jumpTabIndex(jump); got != want {
			t.Errorf("jumpTabIndex(%q) = %d, want %d", jump, got, want)
		}
	}
}

func TestRenderChecksListsByName(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "SUCCESS", Name: "build"},
	}}
	out := renderChecks(pr, 60, 0)
	if !strings.Contains(out, "lint") || !strings.Contains(out, "build") {
		t.Fatalf("checks not listed by name: %q", out)
	}
}

func TestRenderChecksMarksCursor(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "SUCCESS", Name: "build"},
	}}
	lines := strings.Split(strings.TrimRight(renderChecks(pr, 60, 1), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 check lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "▎") || strings.Contains(lines[0], "▎") {
		t.Fatalf("cursor gutter should mark only the hovered check:\n%q", lines)
	}
}

func TestRenderDiffstatTotals(t *testing.T) {
	d := gh.PRDetail{Files: []gh.DiffFile{
		{Path: "a.go", Additions: 10, Deletions: 2},
		{Path: "b.go", Additions: 1, Deletions: 1},
	}}
	out := ansi.Strip(renderDiffstat(d, 60))
	if !strings.Contains(out, "2 files") || !strings.Contains(out, "a.go") {
		t.Fatalf("diffstat missing totals/files: %q", out)
	}
}

func TestRenderReviewsEmpty(t *testing.T) {
	if !strings.Contains(renderReviews(gh.PRDetail{}, 60), "No reviews") {
		t.Fatal("empty reviews should say so")
	}
}

func TestDiscussionColumnCapsAndCenters(t *testing.T) {
	renderWidth := 0
	out := renderDiscussionColumn(160, func(w int) string {
		renderWidth = w
		return strings.Repeat("x", w)
	})
	if renderWidth != discussionMaxWidth {
		t.Fatalf("render width = %d, want cap %d", renderWidth, discussionMaxWidth)
	}
	wantGutter := (160 - discussionMaxWidth) / 2
	if !strings.HasPrefix(out, strings.Repeat(" ", wantGutter)) {
		t.Fatalf("discussion column is not centered with %d-cell gutter: %q", wantGutter, out)
	}
}

func TestExpandedFooterNeverOffersPan(t *testing.T) {
	// The Diff tab is a width-fitted diffstat, so no tab horizontally pans.
	m := Model{}
	for _, tab := range []int{0, 1, 2, 3} {
		m.expandedTab = tab
		if got := m.expandedFooter(); strings.Contains(got, "pan") {
			t.Fatalf("tab %d should not offer pan: %q", tab, got)
		}
	}
}

func TestTabSegmentMarksActive(t *testing.T) {
	out := tabSegment(expandedTabs, 2)
	if !strings.Contains(ansi.Strip(out), "Checks") {
		t.Fatalf("active tab missing from segment: %q", out)
	}
	for _, name := range expandedTabs {
		if !strings.Contains(ansi.Strip(out), name) {
			t.Fatalf("tab %q missing from segment: %q", name, out)
		}
	}
	// The active tab carries the accent-pill background; the raw (styled) output
	// must therefore differ from the plain names, or nothing marks the current tab.
	if out == strings.Join(expandedTabs, "") {
		t.Fatalf("active tab not styled distinctly: %q", out)
	}
}

func TestEnterExpandedDeepLinks(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}}}})
	// detail with BLOCKED so the card is "checks failing" → JumpTab "checks" (index 2)
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}

	m.enterExpanded()
	if !m.expanded {
		t.Fatal("enterExpanded should set expanded")
	}
	if m.expandedTab != 2 {
		t.Fatalf("deep-link to Checks tab expected (2), got %d", m.expandedTab)
	}
	// sanity: the triage card for this PR really is checks-failing
	if triage.Compute(gh.PR{StatusCheckRollup: []gh.Check{{State: "FAILURE"}}}, gh.PRDetail{MergeStateStatus: "BLOCKED"}).JumpTab != "checks" {
		t.Fatal("precondition: expected checks JumpTab")
	}
}

func TestChecksTabCursorNavigates(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"}, {State: "SUCCESS", Name: "build"},
	}}})
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}
	m.enterExpanded() // deep-links to the Checks tab (index 2)
	if m.expandedTab != 2 {
		t.Fatalf("precondition: expected Checks tab, got %d", m.expandedTab)
	}
	updated, _ := m.updateExpanded(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(Model)
	if m.checkCursor != 1 {
		t.Fatalf("j on Checks tab should advance checkCursor to 1, got %d", m.checkCursor)
	}
}

func TestRerunExternalCheckReportsNoJob(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	// A StatusContext-style check: no /job/ in DetailsUrl → not job-rerunnable.
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Context: "buildkite", DetailsUrl: "https://buildkite.com/x/42"},
	}}})
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}
	m.enterExpanded()
	updated, _ := m.updateExpanded(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(Model)
	if m.actionStatus == nil || m.actionStatus.err == nil {
		t.Fatal("rerunning an external check should settle to a failed status")
	}
	if !strings.Contains(m.actionStatus.fail, "external") {
		t.Fatalf("status should explain the external-check case, got %q", m.actionStatus.fail)
	}
}

func TestExpandOnEmptyListDoesNotEnterOrPanic(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	updated, _ := m.Update(prsFetchedMsg{prs: []gh.PR{}})
	m = updated.(Model)
	m.renderList()

	m.enterExpanded()
	if m.expanded {
		t.Fatal("enterExpanded should be a no-op with no PRs")
	}
	_ = m.View() // must not panic
}

func TestRefetchToEmptyCollapsesExpanded(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.width, m.height = 100, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.enterExpanded()
	if !m.expanded {
		t.Fatal("precondition: should be expanded with a PR")
	}
	updated, _ := m.Update(prsFetchedMsg{prs: []gh.PR{}})
	m = updated.(Model)
	if m.expanded {
		t.Fatal("a refetch emptying the list should collapse the expanded view")
	}
	_ = m.View() // must not panic
}

func TestExpandedViewShowsTabStrip(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	out := m.expandedView()
	if !strings.Contains(out, "Conversation") || !strings.Contains(out, "Checks") {
		t.Fatalf("expanded view should show the tab strip: %q", out)
	}
	if !strings.Contains(out, "#7") {
		t.Fatalf("expanded view should show the PR number: %q", out)
	}
}

func TestExpandedBoxWidthCapsEveryTab(t *testing.T) {
	m := Model{width: 200}
	for _, tab := range []int{0, 1, 2, 3} { // Conversation, Reviews, Checks, Diff
		m.expandedTab = tab
		if got := m.expandedBoxWidth(); got != discussionMaxWidth+6 {
			t.Fatalf("tab %d width = %d, want capped %d", tab, got, discussionMaxWidth+6)
		}
	}
	m.width = 80 // narrow terminal: never wider than the terminal
	if got := m.expandedBoxWidth(); got != 80 {
		t.Fatalf("narrow width = %d, want 80", got)
	}
}

func manyComments(n int) []gh.Comment {
	cs := make([]gh.Comment, n)
	for i := range cs {
		cs[i].Author.Login = "octocat"
		cs[i].Body = fmt.Sprintf("comment number %d", i)
	}
	return cs
}

func TestConversationOpensAtMostRecent(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 12 // short enough that the timeline overflows
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{Comments: manyComments(40)}

	m.enterExpanded()
	if m.expandedTab != 0 {
		t.Fatalf("precondition: Conversation tab expected, got %d", m.expandedTab)
	}
	if m.vp.YOffset() == 0 {
		t.Fatal("precondition: content should overflow so the offset can be non-zero")
	}
	if !m.vp.AtBottom() {
		t.Fatal("Conversation should open scrolled to the most recent comment")
	}
}

func TestReflowKeepsScrollPosition(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 12
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{Comments: manyComments(40)}
	m.enterExpanded()

	m.vp.ScrollUp(3) // reader scrolls back through the history
	want := m.vp.YOffset()

	updated, _ := m.Update(prDetailMsg{number: 7, detail: gh.PRDetail{Comments: manyComments(40)}})
	m = updated.(Model)
	if got := m.vp.YOffset(); got != want {
		t.Fatalf("refresh moved the scroll: got %d, want %d", got, want)
	}
}

func TestExpandedLeftOnFirstTabExits(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.width, m.height = 120, 40
	m.expanded = true
	m.expandedTab = 0

	u, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = u.(Model)
	if m.expanded {
		t.Fatal("h on the first tab should exit the expanded view")
	}
}

func TestExpandedLeftOnLaterTabMovesLeft(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.width, m.height = 120, 40
	m.expanded = true
	m.expandedTab = 2

	u, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = u.(Model)
	if !m.expanded || m.expandedTab != 1 {
		t.Fatalf("h on a later tab should move left; expanded=%v tab=%d", m.expanded, m.expandedTab)
	}
}

func TestExpandedTwoColStaysWithinBounds(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	// sortPRs may reorder sweepPRs, so pin the detail to whichever PR actually
	// lands at cursor row 0 — otherwise the rail's reviewer/diffstat branch
	// (the zero-PRDetail path) goes unexercised.
	ps, _ := m.section.(*PRSection)
	n0 := ps.prAt(0).Number
	m.detail[n0] = gh.PRDetail{
		ReviewRequests: []gh.ReviewRequest{{Login: "octocat"}},
		Files:          []gh.DiffFile{{Path: "internal/ui/expanded.go", Additions: 40, Deletions: 5}},
	}
	m.loaded = true
	const w, h = 180, 45
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = u.(Model)
	m.enterExpanded()

	l := computeExpandedLayout(w, h, true)
	if !l.TwoCol {
		t.Fatalf("expected two-col at %dx%d", w, h)
	}
	for i, ln := range strings.Split(m.expandedView(), "\n") {
		if lw := lipgloss.Width(ln); lw > w {
			t.Errorf("two-col line %d width %d exceeds terminal width %d", i, lw, w)
		}
	}
	if fh := lipgloss.Height(m.expandedView()); fh > h {
		t.Errorf("two-col frame height %d exceeds terminal height %d", fh, h)
	}
}

func TestExpandedSurvivesResizeAcrossTwoColBoundary(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	m.detail[7] = gh.PRDetail{Files: []gh.DiffFile{{Path: "a.go", Additions: 1, Deletions: 1}}}
	m.loaded = true
	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = u.(Model)
	m.enterExpanded()

	for _, sz := range [][2]int{{180, 45}, {100, 30}, {160, 24}, {90, 40}, {200, 50}} {
		w, hh := sz[0], sz[1]
		u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: hh})
		m = u.(Model)
		for i, ln := range strings.Split(m.expandedView(), "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("%dx%d: line %d width %d exceeds %d", w, hh, i, lw, w)
			}
		}
		if fh := lipgloss.Height(m.expandedView()); fh > hh {
			t.Errorf("%dx%d: frame height %d exceeds %d", w, hh, fh, hh)
		}
	}
}

// TestExpandedTwoColRailFrameStaysWithinBounds stresses the two-col frame with a
// near-full rail — many requested reviewers plus a chip-budget-overflowing label
// set — on short, wide terminals. It pins the integration-level invariant that
// the composed frame never exceeds w×h: the lines[:RailH] logical cap, the
// rail↔content JoinHorizontal height match, and per-line truncation all holding
// together. (It does not by itself falsify the renderExpandedRail MaxWidth/
// MaxHeight clamp, which is unreachable defense-in-depth given renderChips's
// maxW guarantee — the chip width contract is pinned by TestRenderChipsNeverExceedsMaxW.)
func TestExpandedTwoColRailFrameStaysWithinBounds(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	m.loaded = true
	ps, _ := m.section.(*PRSection)
	n0 := ps.prAt(0).Number
	var reviewers []gh.ReviewRequest
	for i := range 20 {
		reviewers = append(reviewers, gh.ReviewRequest{Login: fmt.Sprintf("reviewer-%02d", i)})
	}
	pr := ps.prAt(0)
	pr.Labels = []gh.Label{
		{Name: "bug", Color: "d73a4a"}, {Name: "enhancement", Color: "a2eeef"},
		{Name: "needs-triage", Color: "fbca04"}, {Name: "backend", Color: "0e8a16"},
		{Name: "priority-high", Color: "5319e7"}, {Name: "documentation", Color: "0075ca"},
	}
	m.setPRs([]gh.PR{pr})
	m.detail[n0] = gh.PRDetail{ReviewRequests: reviewers, Files: []gh.DiffFile{{Path: "x.go", Additions: 9, Deletions: 9}}}

	// Short, wide terminals: two-col engages but RailH is small, so any wrap shows.
	for _, sz := range [][2]int{{150, 12}, {144, 10}, {200, 14}, {160, 20}} {
		w, hh := sz[0], sz[1]
		u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: hh})
		m = u.(Model)
		m.enterExpanded()
		view := m.expandedView()
		for i, ln := range strings.Split(view, "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("%dx%d: line %d width %d exceeds %d", w, hh, i, lw, w)
			}
		}
		if fh := lipgloss.Height(view); fh > hh {
			t.Errorf("%dx%d: rail-stressed frame height %d exceeds %d", w, hh, fh, hh)
		}
	}
}
