package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

func TestListEnterExpands(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !updated.(Model).expanded {
		t.Fatal("enter should expand from the list")
	}
}

func TestExpandedBackExitsOnFirstTab(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded() // tab 0
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if updated.(Model).expanded {
		t.Fatal("h on tab 0 should exit the expanded view, not wrap")
	}
}

func TestExpandedBackDecrementsTab(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	m.expandedTab = 2
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m2 := updated.(Model)
	if !m2.expanded || m2.expandedTab != 1 {
		t.Fatalf("h on tab 2 should go to tab 1, got expanded=%v tab=%d", m2.expanded, m2.expandedTab)
	}
}

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

func TestChecksTabCursorMoves(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "a"}, {State: "SUCCESS", Name: "b"},
	}}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	m.expandedTab = 2
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := updated.(Model).checkCursor; got != 1 {
		t.Fatalf("j on Checks tab should advance checkCursor to 1, got %d", got)
	}
}

func TestRerunHoveredJobIssuesCmd(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "a", DetailsUrl: "https://github.com/o/r/actions/runs/1/job/99"},
	}}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	m.expandedTab = 2
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r on a job-bearing check should issue a rerun cmd")
	}
	if !strings.Contains(updated.(Model).notice, "rerun queued") {
		t.Errorf("expected a queued notice, got %q", updated.(Model).notice)
	}
}

func TestRerunHoveredExternalNoCmd(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Context: "ci/external"}, // StatusContext: no detailsUrl
	}}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	m.expandedTab = 2
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd != nil {
		t.Fatal("external check has no job to rerun")
	}
	if !strings.Contains(updated.(Model).notice, "no rerun") {
		t.Errorf("expected an external-check hint, got %q", updated.(Model).notice)
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

func TestTabStripMarksActive(t *testing.T) {
	out := tabStrip(2)
	for _, name := range expandedTabs {
		if !strings.Contains(out, name) {
			t.Fatalf("tab %q missing from strip: %q", name, out)
		}
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
