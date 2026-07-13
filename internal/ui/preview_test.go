package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

func TestRenderPreviewBodyShowsOlderMarker(t *testing.T) {
	items := make([]preview.Item, 5)
	for i := range items {
		items[i] = preview.Item{Author: "a", Body: "msg", At: time.Unix(int64(i), 0), Kind: preview.KindComment}
	}
	out := renderTimeline(items, 3, 80, false) // n=3, not expanded
	if !strings.Contains(out, "earlier") {
		t.Fatalf("expected older marker: %q", out)
	}
}

func TestDiscussionHeaderUsesRemainingWidth(t *testing.T) {
	meta := metaLine("alice", "", time.Time{})
	out := ansi.Strip(discussionHeader(meta, 40))
	if !strings.HasPrefix(out, "@alice ") || !strings.Contains(out, "─") {
		t.Fatalf("discussion header should combine metadata and divider: %q", out)
	}
	if got := lipgloss.Width(out); got != 40 {
		t.Fatalf("discussion header width = %d, want 40: %q", got, out)
	}
}

func TestRenderTimelineEmptyState(t *testing.T) {
	out := ansi.Strip(renderTimeline(nil, 0, 80, true))
	if out != "No conversation yet." {
		t.Fatalf("empty conversation state = %q", out)
	}
}

func TestRenderTimelineKeepsCommentsCompact(t *testing.T) {
	items := []preview.Item{
		{Author: "alice", Body: "First comment."},
		{Author: "bob", Body: "Second comment."},
	}
	out := ansi.Strip(renderTimeline(items, len(items), 80, true))
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("conversation should not accumulate multiple blank rows:\n%q", out)
	}
}

func TestIdentityHeader(t *testing.T) {
	pr := gh.PR{Number: 309, Title: "Add retry logic", HeadRefName: "feat/309-retry"}
	pr.Author.Login = "bob"
	out := identityHeader(pr)
	for _, want := range []string{"#309", "Add retry logic", "bob", "feat/309-retry"} {
		if !strings.Contains(out, want) {
			t.Fatalf("identity header missing %q: %q", want, out)
		}
	}
}

func TestReviewersLine(t *testing.T) {
	got := reviewersLine(nil)
	if !strings.Contains(got, "no reviewers") {
		t.Fatalf("empty reviewers should warn: %q", got)
	}
	got = reviewersLine([]gh.ReviewRequest{{Login: "alice"}, {Login: "bob"}})
	if !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Fatalf("should list reviewers: %q", got)
	}
}

func TestReviewLineNamesWho(t *testing.T) {
	mk := func(state string) gh.PRDetail {
		var r gh.Review
		r.Author.Login = "alice"
		r.State = state
		return gh.PRDetail{LatestReviews: []gh.Review{r}}
	}
	if got := reviewLine(mk("CHANGES_REQUESTED")); !strings.Contains(got, "changes requested by @alice") {
		t.Fatalf("should name who requested changes: %q", got)
	}
	if got := reviewLine(mk("APPROVED")); !strings.Contains(got, "approved by @alice") {
		t.Fatalf("should name who approved: %q", got)
	}
	if got := reviewLine(mk("COMMENTED")); !strings.Contains(got, "commented by @alice") {
		t.Fatalf("should name who commented: %q", got)
	}
	if got := reviewLine(mk("DISMISSED")); !strings.Contains(got, "dismissed: @alice") {
		t.Fatalf("should name whose review was dismissed: %q", got)
	}
	if got := reviewLine(gh.PRDetail{ReviewRequests: []gh.ReviewRequest{{Login: "bob"}}}); !strings.Contains(got, "bob") {
		t.Fatalf("with no reviews, should fall back to pending reviewers: %q", got)
	}
}

func TestReviewLineShowsEveryState(t *testing.T) {
	rv := func(login, state string) gh.Review {
		var r gh.Review
		r.Author.Login = login
		r.State = state
		return r
	}
	d := gh.PRDetail{LatestReviews: []gh.Review{
		rv("alice", "CHANGES_REQUESTED"),
		rv("bob", "APPROVED"),
		rv("carol", "COMMENTED"),
		rv("dave", "DISMISSED"),
	}}
	got := reviewLine(d)
	for _, want := range []string{
		"changes requested by @alice",
		"approved by @bob",
		"commented by @carol",
		"dismissed: @dave",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("review line should surface every state, missing %q: %q", want, got)
		}
	}
}

func TestFlagGlyph(t *testing.T) {
	if flagGlyph(gh.PRDetail{MergeStateStatus: "CLEAN"}, false) != "" {
		t.Fatal("uncached detail must render no flag")
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{MergeStateStatus: "DIRTY"}, true), "⚠") {
		t.Fatal("DIRTY should show the conflict flag")
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{MergeStateStatus: "BEHIND"}, true), "⚠") {
		t.Fatal("BEHIND should show the behind flag")
	}
	if flagGlyph(gh.PRDetail{MergeStateStatus: "CLEAN"}, true) != "" {
		t.Fatal("CLEAN should show no flag")
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{Mergeable: "CONFLICTING"}, true), "⚠") {
		t.Fatal("CONFLICTING should show the conflict flag")
	}
}

func TestSectionRule(t *testing.T) {
	r := sectionRule("blocker", 30)
	if !strings.Contains(r, "BLOCKER") || !strings.Contains(r, "─") {
		t.Fatalf("section rule should show the uppercased label and a rule: %q", r)
	}
	if strings.Contains(r, "\n") {
		t.Fatalf("section rule is one line: %q", r)
	}
}

func TestPrefetchNumbers(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5}})
	fresh := map[int]bool{2: true} // #2 already refreshed this session

	got := prefetchNumbers(ps, 0, fresh, 3)
	want := []int{1, 3, 4} // skips fresh #2, capped at window=3
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	all := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}
	if n := prefetchNumbers(ps, 0, all, 3); n != nil {
		t.Fatalf("all fresh should yield nil, got %v", n)
	}
}

func TestRenderMainBordersListPane(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30 // narrow: single bordered list pane
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.renderList()
	out := m.renderMain()
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Fatalf("renderMain should wrap the list in a rounded border: %q", out)
	}
	if !strings.Contains(out, "· 1") {
		t.Fatalf("list pane should be titled: %q", out)
	}
}

func TestPreviewChecksSectionShownOnlyWhenBlockerMasksCI(t *testing.T) {
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	render := func(d gh.PRDetail) string {
		m := NewModel("/repo", "is:open", nil)
		m.width, m.height = 150, 40
		p := gh.PR{Number: 1, Title: "x", StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}}}
		p.Author.Login = "a"
		m.setPRs([]gh.PR{p})
		m.detail[1] = d
		m.renderList()
		return ansi.ReplaceAllString(m.previewPane(), "")
	}
	// Blocker IS checks-failing → no redundant standalone "checks" section.
	if got := render(gh.PRDetail{MergeStateStatus: "BLOCKED"}); strings.Contains(got, "\nCHECKS ─") {
		t.Fatalf("checks section should be suppressed when the blocker is CI:\n%s", got)
	}
	// Blocker is a conflict that masks failing CI → checks section surfaces it.
	if got := render(gh.PRDetail{MergeStateStatus: "DIRTY"}); !strings.Contains(got, "\nCHECKS ─") {
		t.Fatalf("checks section should show when a conflict masks failing CI:\n%s", got)
	}
}

func TestPreviewPrefillsBeforeDetailLoads(t *testing.T) {
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40
	p := gh.PR{Number: 1, Title: "x", StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}}}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.renderList() // no m.detail[1]: detail not yet fetched
	out := ansi.ReplaceAllString(m.previewPane(), "")
	if strings.Contains(out, "Loading preview…") {
		t.Fatalf("uncached preview should pre-fill from list data, not bare loading:\n%s", out)
	}
	if !strings.Contains(out, "1 check failing") {
		t.Fatalf("pre-fill should surface the failing-checks blocker:\n%s", out)
	}
	if !strings.Contains(out, "loading details…") {
		t.Fatalf("pre-fill should still flag that detail is loading:\n%s", out)
	}
}

func TestPreviewWidthSubtractsBorder(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40 // wide: side pane shows
	l := computeLayout(150, 40)
	if got := m.previewWidth(); got != l.SideWidth-2 {
		t.Fatalf("previewWidth = %d, want SideWidth-2 = %d", got, l.SideWidth-2)
	}
}

func TestRenderMainWideLayoutFitsAndBordersBoth(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 140, 30 // wide: list + side pane
	p := gh.PR{Number: 1, Title: "hello"}
	p.Author.Login = "al"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()
	out := m.renderMain()
	if n := strings.Count(out, "╭"); n < 2 {
		t.Fatalf("wide layout should border both panes (got %d top-left corners)", n)
	}
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d width %d exceeds terminal width %d", i, w, m.width)
		}
	}
}

func TestPreviewScrollClampsAndResets(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 150, 6 // tiny height → preview content overflows the pane
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()

	over := lipgloss.Height(m.previewPane()) - (computeLayout(150, 6).ContentHeight - 2)
	if over <= 0 {
		t.Fatalf("fixture must overflow the pane for this test; over=%d", over)
	}

	m.previewScrollBy(-5) // can't scroll above the top
	if m.previewOffset != 0 {
		t.Fatalf("scroll up at top should clamp to 0, got %d", m.previewOffset)
	}
	m.previewScrollBy(9999) // can't scroll the last line above the top
	if m.previewOffset != over {
		t.Fatalf("scroll down should clamp to over=%d, got %d", over, m.previewOffset)
	}
	m.moveCursor(0) // focus change resets the preview scroll
	if m.previewOffset != 0 {
		t.Fatalf("moving the cursor should reset preview scroll, got %d", m.previewOffset)
	}
}

func TestPreviewScrollNoOpWhenContentFits(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 150, 60 // tall pane → short preview fits, nothing to scroll
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()
	m.previewScrollBy(1) // must not blank the preview by scrolling past the end
	if m.previewOffset != 0 {
		t.Fatalf("scrolling when content fits must stay at 0, got %d", m.previewOffset)
	}
}

func TestIssuePreviewRendersBody(t *testing.T) {
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.width, m.height = 120, 40
	sec := NewIssueSection("is:open")
	sec.SetIssues([]gh.Issue{{Number: 5, Title: "Crash on save", Author: struct {
		Login string `json:"login"`
	}{Login: "octocat"}}})
	m.section = sec
	m.issueDetail[5] = gh.IssueDetail{Body: "Steps to reproduce"}

	pane := ansi.ReplaceAllString(m.previewPane(), "")
	if !strings.Contains(pane, "#5") || !strings.Contains(pane, "Steps to reproduce") {
		t.Errorf("issue preview missing number/body:\n%s", pane)
	}
}

func TestIssueDetailMsgStores(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	out, _ := m.Update(issueDetailMsg{number: 5, detail: gh.IssueDetail{Body: "b"}})
	got := out.(Model)
	if got.issueDetail[5].Body != "b" || !got.issueFresh[5] {
		t.Error("issue detail not stored / not marked fresh")
	}
}
