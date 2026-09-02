package ui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	out := identityHeader(pr, 80)
	for _, want := range []string{"#309", "Add retry logic", "bob", "feat/309-retry"} {
		if !strings.Contains(out, want) {
			t.Fatalf("identity header missing %q: %q", want, out)
		}
	}
}

func TestIdentityHeaderCarriesBaseArrowAndLabels(t *testing.T) {
	pr := gh.PR{
		Number: 3087, Title: "fix(services): raise provenance contention",
		HeadRefName: "eng-7726-same-value", BaseRefName: "main",
		UpdatedAt: time.Now().Add(-2 * time.Hour),
		Labels:    []gh.Label{{Name: "complexity:7", Color: "fab387"}},
	}
	pr.Author.Login = "noamsto"

	got := stripANSIForTest(identityHeader(pr, 80))
	if !strings.Contains(got, "main") || !strings.Contains(got, "eng-7726-same-value") {
		t.Errorf("base and head must both appear:\n%s", got)
	}
	if !strings.Contains(got, "←") {
		t.Errorf("base <- head arrow missing:\n%s", got)
	}
	if !strings.Contains(got, "complexity:7") {
		t.Errorf("label chips must appear in the identity block:\n%s", got)
	}
}

// hasReviewer reports whether the roster has a line naming this login with this
// status label. Spacing between them varies with alignment padding, so it checks
// co-occurrence on one line rather than an exact substring.
func hasReviewer(roster, login, label string) bool {
	for _, line := range strings.Split(roster, "\n") {
		if strings.Contains(line, login) && strings.Contains(line, label) {
			return true
		}
	}
	return false
}

func TestReviewRosterEmpty(t *testing.T) {
	if got := reviewRoster(gh.PRDetail{}); !strings.Contains(got, "no reviewers") {
		t.Fatalf("no reviewers or requests should warn: %q", got)
	}
}

func TestReviewRosterNamesWho(t *testing.T) {
	mk := func(state string) gh.PRDetail {
		var r gh.Review
		r.Author.Login = "alice"
		r.State = state
		return gh.PRDetail{LatestReviews: []gh.Review{r}}
	}
	for state, label := range map[string]string{
		"CHANGES_REQUESTED": "changes requested",
		"APPROVED":          "approved",
		"COMMENTED":         "commented",
		"DISMISSED":         "dismissed",
	} {
		if got := reviewRoster(mk(state)); !hasReviewer(got, "@alice", label) {
			t.Fatalf("state %s: want @alice %q in %q", state, label, got)
		}
	}
}

// A requested reviewer who has not reviewed shows as pending — even when others
// have already reviewed, which the old fallback-only path hid.
func TestReviewRosterShowsPendingAlongsideReviews(t *testing.T) {
	var bot gh.Review
	bot.Author.Login = "app/cursor"
	bot.State = "COMMENTED"
	d := gh.PRDetail{
		LatestReviews:  []gh.Review{bot},
		ReviewRequests: []gh.ReviewRequest{{Login: "carol"}},
	}
	got := reviewRoster(d)
	if !hasReviewer(got, "@carol", "pending") {
		t.Fatalf("pending reviewer must show alongside a bot comment: %q", got)
	}
	if !hasReviewer(got, "@app/cursor", "commented") {
		t.Fatalf("the comment must still show: %q", got)
	}
}

// A re-requested reviewer with a stale prior review counts as pending, not by
// the old review's state.
func TestReviewRosterReRequestOverridesStaleReview(t *testing.T) {
	var stale gh.Review
	stale.Author.Login = "bob"
	stale.State = "APPROVED"
	d := gh.PRDetail{
		LatestReviews:  []gh.Review{stale},
		ReviewRequests: []gh.ReviewRequest{{Login: "bob"}},
	}
	got := reviewRoster(d)
	if strings.Contains(got, "approved") {
		t.Fatalf("re-requested reviewer must not show the stale approval: %q", got)
	}
	if !hasReviewer(got, "@bob", "pending") {
		t.Fatalf("re-requested reviewer must show pending: %q", got)
	}
}

func TestReviewRosterShowsEveryState(t *testing.T) {
	rv := func(login, state string) gh.Review {
		var r gh.Review
		r.Author.Login = login
		r.State = state
		return r
	}
	d := gh.PRDetail{
		LatestReviews: []gh.Review{
			rv("alice", "CHANGES_REQUESTED"),
			rv("bob", "APPROVED"),
			rv("carol", "COMMENTED"),
			rv("dave", "DISMISSED"),
		},
		ReviewRequests: []gh.ReviewRequest{{Login: "erin"}},
	}
	got := reviewRoster(d)
	for _, want := range []struct{ login, label string }{
		{"@alice", "changes requested"},
		{"@bob", "approved"},
		{"@carol", "commented"},
		{"@dave", "dismissed"},
		{"@erin", "pending"},
	} {
		if !hasReviewer(got, want.login, want.label) {
			t.Fatalf("roster should surface every state, missing %s %q: %q", want.login, want.label, got)
		}
	}
	// most-actionable first: changes requested precedes pending precedes approved.
	if idx := strings.Index; !(idx(got, "changes requested") < idx(got, "pending") &&
		idx(got, "pending") < idx(got, "approved")) {
		t.Fatalf("roster order should rank by actionability: %q", got)
	}
}

func TestFlagGlyph(t *testing.T) {
	if flagGlyph("", "CLEAN") != "" {
		t.Fatal("CLEAN should show no flag")
	}
	if !strings.Contains(flagGlyph("", "DIRTY"), warnGlyph) {
		t.Fatal("DIRTY should show the conflict flag")
	}
	if !strings.Contains(flagGlyph("", "BEHIND"), warnGlyph) {
		t.Fatal("BEHIND should show the behind flag")
	}
	if !strings.Contains(flagGlyph("CONFLICTING", ""), warnGlyph) {
		t.Fatal("CONFLICTING should show the conflict flag")
	}
}

func TestFlagGlyphUnknownRendersBlank(t *testing.T) {
	if flagGlyph("UNKNOWN", "") != "" {
		t.Error("UNKNOWN mergeable should render no flag")
	}
	if flagGlyph("", "") != "" {
		t.Error("no signal at all should render no flag")
	}
}

func TestFlagGlyphDistinguishesConflictFromBehind(t *testing.T) {
	conflict := flagGlyph("CONFLICTING", "")
	behind := flagGlyph("", "BEHIND")
	if conflict == "" || behind == "" {
		t.Fatalf("both should render a warning glyph: conflict=%q behind=%q", conflict, behind)
	}
	if conflict == behind {
		t.Errorf("conflict (failStyle, red) and behind (pendStyle, yellow) must render with different styles, both got %q", conflict)
	}
}

func TestMergeStateListFallbackWhenNoDetail(t *testing.T) {
	p := gh.PR{Mergeable: "CONFLICTING"}
	mergeable, _ := mergeState(p, gh.PRDetail{}, false)
	if mergeable != "CONFLICTING" {
		t.Errorf("mergeable = %q, want CONFLICTING (from the list value)", mergeable)
	}
}

func TestMergeStateDetailWinsWhenResolved(t *testing.T) {
	p := gh.PR{Mergeable: "CONFLICTING"} // stale list value
	cached := gh.PRDetail{Mergeable: "MERGEABLE"}
	mergeable, _ := mergeState(p, cached, true)
	if mergeable != "MERGEABLE" {
		t.Errorf("mergeable = %q, want MERGEABLE (resolved detail wins over a differing list value)", mergeable)
	}
}

func TestMergeStateFallsBackWhenDetailUnresolved(t *testing.T) {
	p := gh.PR{Mergeable: "CONFLICTING"}
	cached := gh.PRDetail{Mergeable: "UNKNOWN"} // e.g. painted by hydrateDetail from a stale disk cache
	mergeable, _ := mergeState(p, cached, true)
	if mergeable != "CONFLICTING" {
		t.Errorf("mergeable = %q, want CONFLICTING — an unresolved cached value must not mask a resolved list value", mergeable)
	}
}

func TestSectionHeaderHasNoRuleAndNoUppercasing(t *testing.T) {
	got := stripANSIForTest(sectionHeader(blockerGlyph, "blocker", 60))
	if strings.Contains(got, "─") {
		t.Errorf("section header must not draw a rule: %q", got)
	}
	if strings.Contains(got, "BLOCKER") {
		t.Errorf("section header must not uppercase: %q", got)
	}
	if !strings.Contains(got, "Blocker") {
		t.Errorf("section header should be Title Case: %q", got)
	}
}

func TestPrefetchNumbers(t *testing.T) {
	ps := NewPRSection("is:open")
	// Board order is number descending, so the fixture is given already sorted.
	ps.SetPRs([]gh.PR{{Number: 5}, {Number: 4}, {Number: 3}, {Number: 2}, {Number: 1}})
	fresh := map[int]bool{4: true} // #4 already refreshed this session

	got := prefetchNumbers(ps, 0, fresh, 3)
	want := []int{5, 3, 2} // skips fresh #4, capped at window=3; cursor at top → only downward
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

// TestPrefetchNumbersBidirectional: cursor in the middle fills both directions
// by distance, preferring below on ties.
func TestPrefetchNumbersBidirectional(t *testing.T) {
	ps := NewPRSection("is:open")
	// Numbers 1..9 given ascending; the board's number-descending sort reverses
	// them, so shown index i holds Number 9-i.
	prs := make([]gh.PR, 9)
	for i := range prs {
		prs[i].Number = i + 1
	}
	ps.SetPRs(prs)

	got := prefetchNumbers(ps, 4, nil, 5) // cursor on #5 (index 4)
	// distances: 0→#5, 1→below #4 then above #6, 2→below #3 then above #7
	want := []int{5, 4, 6, 3, 7}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPrefetchNumbersSkipsFreshAndFillsFarther(t *testing.T) {
	ps := NewPRSection("is:open")
	// Numbers 1..7 given ascending; reversed by the sort so shown index i holds
	// Number 7-i.
	prs := make([]gh.PR, 7)
	for i := range prs {
		prs[i].Number = i + 1
	}
	ps.SetPRs(prs)
	// Cursor #4 (index 3). Mark nearest neighbors fresh so the window reaches farther.
	fresh := map[int]bool{4: true, 5: true, 3: true}
	got := prefetchNumbers(ps, 3, fresh, 3)
	want := []int{2, 6, 1} // dist 2 below, 2 above, 3 below
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPrefetchNumbersAtEndGoesUp(t *testing.T) {
	ps := NewPRSection("is:open")
	// Numbers 1..4 given ascending; reversed by the sort, so the last shown row
	// (index 3) holds #1.
	ps.SetPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}})
	got := prefetchNumbers(ps, 3, nil, 3) // cursor on last row (#1)
	want := []int{1, 2, 3}                // only upward (toward higher numbers) after cursor
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
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

func TestOuterFrameWrapsBoard(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.SetOuterFrame(true)
	m.InitTheme()
	u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = u.(Model)
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.renderList()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "prdash") {
		t.Fatalf("outer frame should title the border with prdash:\n%s", out)
	}
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Fatalf("outer frame should be rounded:\n%s", out)
	}
	// Inner content size is terminal − 2; without the frame the board would be 100 wide.
	if m.width != 98 || m.height != 28 {
		t.Fatalf("inner size = %dx%d, want 98x28", m.width, m.height)
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
	if got := render(gh.PRDetail{MergeStateStatus: "BLOCKED"}); strings.Contains(got, checksGlyph+" Checks") {
		t.Fatalf("checks section should be suppressed when the blocker is CI:\n%s", got)
	}
	// Blocker is a conflict that masks failing CI → checks section surfaces it.
	if got := render(gh.PRDetail{MergeStateStatus: "DIRTY"}); !strings.Contains(got, checksGlyph+" Checks") {
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

	// Only the body scrolls: the pinned head (identity + tab bar) and the blank
	// row under it come off the pane interior before the body's budget.
	head, body := m.previewParts()
	over := lipgloss.Height(body) - (m.previewHeight(computeLayout(150, 6)) - 2 - lipgloss.Height(head) - 1)
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

// TestPreviewHeadStaysPinned locks the pinned head: alt+j moves the body under
// the identity lines and tab bar, which keep their rows at the top of the pane.
func TestPreviewHeadStaysPinned(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 150, 6 // tiny height → preview content overflows the pane
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()

	head, body := m.previewParts()
	if head == "" {
		t.Fatal("PR preview must have a pinned head")
	}
	m.previewScrollBy(1)
	if m.previewOffset != 1 {
		t.Fatalf("fixture must overflow so the body scrolls; offset=%d", m.previewOffset)
	}
	got := m.previewScrolled()
	if !strings.HasPrefix(got, head+"\n\n") {
		t.Fatalf("head must stay at the top of the pane after scrolling; got %q", ansi.Strip(got))
	}
	firstBody := strings.SplitN(body, "\n", 2)[0]
	if rest := strings.TrimPrefix(got, head+"\n\n"); strings.HasPrefix(rest, firstBody) {
		t.Fatalf("body did not scroll: still starts with %q", ansi.Strip(firstBody))
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

func mouseScrollablePreview(t *testing.T, previewMax bool) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 150, 8
	m.previewMax = previewMax
	p := gh.PR{Number: 1, Title: "x", Body: strings.Repeat("A long preview body. ", 80)}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()
	return m
}

func wheelPreview(t *testing.T, m Model, x, y int, button tea.MouseButton) (Model, bool) {
	t.Helper()
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("preview view must enable mouse wheel reporting")
	}
	if v.OnMouse == nil {
		return m, false
	}
	cmd := v.OnMouse(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	if cmd == nil {
		return m, false
	}
	next, _ := m.Update(cmd())
	return next.(Model), true
}

func TestMouseWheelScrollsPreviewOnlyInsideVisiblePane(t *testing.T) {
	for _, previewMax := range []bool{false, true} {
		t.Run(map[bool]string{false: "wide", true: "maximized"}[previewMax], func(t *testing.T) {
			m := mouseScrollablePreview(t, previewMax)
			x, y, w, h, ok := m.previewMouseBounds()
			if !ok {
				t.Fatal("fixture must show a preview pane")
			}
			next, handled := wheelPreview(t, m, x+w/2, y+h/2, tea.MouseWheelDown)
			if !handled || next.previewOffset != 1 {
				t.Fatalf("wheel inside preview = handled:%v offset:%d, want true:1", handled, next.previewOffset)
			}
			next, handled = wheelPreview(t, next, x-1, y+h/2, tea.MouseWheelDown)
			if handled || next.previewOffset != 1 {
				t.Fatalf("wheel outside preview = handled:%v offset:%d, want false:1", handled, next.previewOffset)
			}
		})
	}
}

func TestMouseWheelPreviewScrollClampsAndIgnoresOverlays(t *testing.T) {
	m := mouseScrollablePreview(t, false)
	x, y, w, h, ok := m.previewMouseBounds()
	if !ok {
		t.Fatal("fixture must show a preview pane")
	}
	m, handled := wheelPreview(t, m, x+w/2, y+h/2, tea.MouseWheelDown)
	if !handled {
		t.Fatal("wheel in preview should be handled")
	}
	m, _ = wheelPreview(t, m, x+w/2, y+h/2, tea.MouseWheelUp)
	if m.previewOffset != 0 {
		t.Fatalf("wheel up at top should clamp to 0, got %d", m.previewOffset)
	}
	head, body := m.previewParts()
	over := lipgloss.Height(body) - (m.previewHeight(computeLayout(m.width, m.height)) - 2 - lipgloss.Height(head) - 1)
	next, _ := m.Update(previewMouseScrollMsg{delta: 1 << 20})
	m = next.(Model)
	if m.previewOffset != over {
		t.Fatalf("wheel down should clamp at %d, got %d", over, m.previewOffset)
	}
	_, _, _, _, ok = m.previewMouseBounds()
	if !ok {
		t.Fatal("preview should remain visible before overlay")
	}
	m.showLegend = true
	if _, handled := wheelPreview(t, m, x+w/2, y+h/2, tea.MouseWheelDown); handled {
		t.Fatal("wheel should not route through an overlay")
	}
	m.showLegend = false
	v := m.View()
	cmd := v.OnMouse(tea.MouseWheelMsg{X: x + w/2, Y: y + h/2, Button: tea.MouseWheelUp})
	if cmd == nil {
		t.Fatal("wheel message should be queued while the preview is visible")
	}
	before := m.previewOffset
	m.showLegend = true
	next, _ = m.Update(cmd())
	if got := next.(Model).previewOffset; got != before {
		t.Fatalf("queued wheel message under overlay changed offset to %d, want %d", got, before)
	}
}

func TestMouseWheelPreviewAccountsForOuterFrameAndNoPreview(t *testing.T) {
	m := mouseScrollablePreview(t, false)
	x, y, _, _, ok := m.previewMouseBounds()
	if !ok {
		t.Fatal("fixture must show a preview pane")
	}
	m.SetOuterFrame(true)
	m.termW, m.termH = m.width+2, m.height+2
	framedX, framedY, _, _, ok := m.previewMouseBounds()
	if !ok || framedX != x+1 || framedY != y+1 {
		t.Fatalf("outer frame bounds = (%d,%d), want (%d,%d)", framedX, framedY, x+1, y+1)
	}
	m.previewMax = false
	m.width = sideThreshold - 1
	if _, _, _, _, ok := m.previewMouseBounds(); ok {
		t.Fatal("narrow layout must not accept preview mouse scrolling")
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

func TestPreviewDescriptionCollapsesForOwnPR(t *testing.T) {
	body := "- L1\n- L2\n- L3\n- L4\n- L5\n- L6\n- L7\n- L8\n- L9\n- L10"
	mk := func(login string) gh.PR {
		p := gh.PR{Body: body}
		p.Author.Login = login
		return p
	}
	own := previewDescriptionBody(mk("me"), "me", 60)
	others := previewDescriptionBody(mk("them"), "me", 60)
	if !strings.Contains(own, "full text in Description tab") {
		t.Fatalf("truncated own PR should hint at the Description tab:\n%s", own)
	}
	ownLines := strings.Count(own, "\n")
	otherLines := strings.Count(others, "\n")
	if ownLines >= otherLines {
		t.Fatalf("own PR (%d lines) should collapse smaller than others (%d):\nown=%q\nothers=%q",
			ownLines, otherLines, own, others)
	}
}

func TestPreviewDescriptionEmptyOmitted(t *testing.T) {
	if got := previewDescriptionBody(gh.PR{Body: ""}, "me", 60); got != "" {
		t.Fatalf("empty body should yield no section, got %q", got)
	}
}

func TestPreviewPaneShowsDescriptionSection(t *testing.T) {
	ansiRe := regexp.MustCompile("\x1b\\[[0-9;]*m")
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40
	m.viewerLogin = "me"
	p := gh.PR{Number: 1, Title: "x", Body: "does a cool thing"}
	p.Author.Login = "them"
	m.setPRs([]gh.PR{p})
	m.renderList()
	out := ansiRe.ReplaceAllString(m.previewPane(), "")
	if !strings.Contains(out, descriptionGlyph+" Description") || !strings.Contains(out, "does a cool thing") {
		t.Fatalf("preview should show the description section:\n%s", out)
	}
}

func TestContentHeightReclaimsHiddenFooterRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 14
	l := computeLayout(m.width, m.height)
	if l.ShowFooter {
		t.Fatal("test setup: expected footer hidden at h=14")
	}
	// The footer rows are reclaimed, but the always-visible filter bar still costs
	// its (blurred) row — the bar stays even in a window too small for the footer.
	if got, want := m.contentHeight(l), max(1, l.ContentHeight-m.filterBarRows()); got != want {
		t.Fatalf("contentHeight() = %d, want %d (footer reclaimed, filter bar still reserved) when footer is hidden and not filtering", got, want)
	}

	m.filtering = true
	if got, want := m.contentHeight(l), max(1, l.ContentHeight-m.filterBarRows()); got != want {
		t.Fatalf("contentHeight() while filtering with no footer = %d, want %d (filter bar input+hint reserved)", got, want)
	}
}

func TestPreviewPaneShowsTabBar(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.renderList()
	out := m.previewPane()
	for _, label := range []string{"Overview", "Diff", "Reviews"} {
		if !strings.Contains(out, label) {
			t.Fatalf("preview pane missing tab %q:\n%s", label, out)
		}
	}
}

func TestRenderTabBarMarksActive(t *testing.T) {
	bar := renderTabBar([]string{"Overview", "Diff"}, 1, 60)
	if !strings.Contains(bar, "Diff") || !strings.Contains(bar, "Overview") {
		t.Fatalf("tab bar missing labels: %s", bar)
	}
}

func TestRenderTabBarOverflowSafe(t *testing.T) {
	// A pane too narrow for every tab must drop trailing tabs rather than
	// truncate mid-cell, which would slice a styled tab's ANSI escape in half.
	bar := renderTabBar(expandedTabs, tabOverview, 10)
	if got := lipgloss.Width(bar); got > 10 {
		t.Fatalf("tab bar width = %d, want <= 10: %q", got, bar)
	}
	if !strings.Contains(bar, "Overview") {
		t.Fatalf("active tab should still render when it fits: %q", bar)
	}
}

// TestPreviewPaneOverviewPrefillsWhenNotCached is the CRITICAL regression: the
// side pane's default tab (Overview) must keep pre-filling the preliminary
// triage card from list-only data while detail is still loading, exactly like
// the pre-tab-bar previewPane did. Routing the Overview tab through
// expandedBody instead of renderOverview would regress to a bare "Loading…"
// because expandedBody's pre-switch !cached gate has no pre-fill of its own.
func TestPreviewPaneOverviewPrefillsWhenNotCached(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 150, 40
	p := gh.PR{Number: 1, Title: "hello world", StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}}}
	p.Author.Login = "a"
	m.setPRs([]gh.PR{p})
	m.renderList() // no m.detail[1]: detail not yet fetched
	if m.expandedTab != tabOverview {
		t.Fatalf("test setup: default expandedTab = %d, want tabOverview", m.expandedTab)
	}
	out := ansi.Strip(m.previewPane())
	if strings.Contains(out, "Loading…") {
		t.Fatalf("Overview tab should pre-fill, not show a bare Loading…:\n%s", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("Overview tab should show the identity header while uncached:\n%s", out)
	}
	if !strings.Contains(out, "1 check failing") {
		t.Fatalf("Overview tab should show the preliminary blocker card while uncached:\n%s", out)
	}
	if !strings.Contains(out, "loading details…") {
		t.Fatalf("Overview tab should still flag that detail is loading:\n%s", out)
	}
}

// Threads now ride the detail cache, so its key is what must not collide across
// repos — #7 in one repo painting #7's review comments in another.
func TestDetailKeyIsRepoScoped(t *testing.T) {
	a := detailKey("noamsto/prdash", 7)
	b := detailKey("noamsto/other", 7)
	if a == b {
		t.Fatal("detailKey must differ across repos")
	}
}

func TestDetailMsgStoresReviewThreads(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	d := gh.PRDetail{ReviewThreads: []gh.ReviewThread{{Path: "main.go", Line: 10}}}
	out, _ := m.Update(prDetailMsg{number: 7, detail: d})
	got := out.(Model)
	if len(got.detail[7].ReviewThreads) != 1 || !got.fresh[7] {
		t.Error("review threads not stored with the detail / not marked fresh")
	}
}

func TestRenderThreadsSummaryEmptyWhenAllResolved(t *testing.T) {
	ts := []gh.ReviewThread{{Path: "a.go", IsResolved: true, Comments: []gh.ThreadComment{{Author: "x", Body: "y"}}}}
	if got := renderThreadsSummary(ts, 2, 60); got != "" {
		t.Fatalf("want empty for all-resolved, got %q", got)
	}
}

func TestRenderThreadsSummaryShowsFileAndAuthor(t *testing.T) {
	ts := []gh.ReviewThread{{Path: "internal/ui/preview.go", Line: 288, IsResolved: false,
		Comments: []gh.ThreadComment{{Author: "alice", Body: "allocates every frame"}}}}
	out := renderThreadsSummary(ts, 2, 80)
	for _, want := range []string{"preview.go:288", "alice", "allocates"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}

// TestRenderThreadsSummaryLocAuthorLineNeverOverflows guards width-safety:
// unlike the body line below it, the loc+author line was never width-truncated,
// so a long author name could push the line past w. 24 is the realistic floor
// (renderItemRow's own documented sub-floor threshold): below it, a fixed-format
// "preview.go:288" location alone can outgrow the width, same as any other
// fixed-format text in this codebase at a degenerate width.
func TestRenderThreadsSummaryLocAuthorLineNeverOverflows(t *testing.T) {
	ts := []gh.ReviewThread{{Path: "internal/ui/preview.go", Line: 288, IsResolved: false,
		Comments: []gh.ThreadComment{{Author: strings.Repeat("verboseauthorname", 5), Body: "x"}}}}
	for w := 24; w <= 60; w++ {
		out := renderThreadsSummary(ts, 2, w)
		for i, ln := range strings.Split(out, "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("w=%d line %d width %d exceeds w: %q", w, i, got, ln)
			}
		}
	}
}
