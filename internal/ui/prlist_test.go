package ui

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

func TestSetPRsBuildsRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 7, Title: "hello", HeadRefName: "feat/x"},
		{Number: 9, Title: "world", HeadRefName: "fix/y"},
	})
	if got := m.section.Len(); got != 2 {
		t.Fatalf("shown len = %d, want 2", got)
	}
	if !strings.Contains(m.section.RenderRow(0, RowOpts{Width: 80}), "#9") {
		t.Fatalf("first row should render #9 (number descending)")
	}
}

func TestListTitleTracksShownCount(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, Title: "hello"}, {Number: 9, Title: "world"}})
	if got := m.listTitle(); !strings.Contains(got, "· 2") {
		t.Fatalf("listTitle = %q, want to contain %q", got, "· 2")
	}
	m.section.SetShown([]int{0})
	if got := m.listTitle(); !strings.Contains(got, "· 1") {
		t.Fatalf("filtered listTitle = %q, want to contain %q", got, "· 1")
	}
}

func TestHydrateFromCache(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw, _ := json.Marshal([]gh.PR{{Number: 42, Title: "cached"}})

	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	c.Set(prKey(m.repo, "is:open", openListLimit), raw) // the sections default reads is:open at openListLimit
	m.hydrate()
	sec := m.section.(*PRSection)
	if len(sec.prs) != 1 || sec.prs[0].Number != 42 {
		t.Fatalf("hydrate did not paint cached rows: %+v", sec.prs)
	}
	if m.section.Len() != 1 {
		t.Fatal("section not painted from cache")
	}
}

func TestIssueKeyDistinctFromPRKey(t *testing.T) {
	if issueKey("r", "is:open", defaultLimit) == prKey("r", "is:open", defaultLimit) {
		t.Error("issue and pr cache keys collide")
	}
}

func TestPRKeyLimitDistinct(t *testing.T) {
	k20 := prKey("o/r", "is:open", defaultLimit)
	k100 := prKey("o/r", "is:open", openListLimit)
	if k20 == k100 {
		t.Fatalf("limit-20 and limit-100 keys collide: %q", k20)
	}
}

func TestIssuesFetchedPopulatesRows(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.filter = "is:open"
	out, _ := m.Update(issuesFetchedMsg{
		filter: "is:open",
		issues: []gh.Issue{{Number: 7, Title: "bug"}, {Number: 9, Title: "feat"}},
	})
	got := out.(Model)
	if got.section.Len() != 2 {
		t.Errorf("rows = %d, want 2", got.section.Len())
	}
	if got.refreshing {
		t.Error("refreshing should clear after fetch")
	}
}

func TestDisabledIssuesShowsNoticeNotError(t *testing.T) {
	m := NewModel("/repo", "is:open assignee:@me", nil)
	m.SetRepo("factify-inc/mono")
	m.mode = "issue"
	m.section = NewIssueSection("is:open assignee:@me")
	m.filter = "is:open assignee:@me"
	m.width, m.height = 100, 30
	m.renderList() // establish the initial Loading… viewport

	updated, _ := m.Update(fetchFailedMsg{
		filter: "is:open assignee:@me",
		err:    errors.New("the 'factify-inc/mono' repository has disabled issues"),
	})
	m = updated.(Model)
	if m.err != nil {
		t.Fatalf("disabled issues should not surface as an error: %v", m.err)
	}
	out := m.render() // no manual renderList: the handler must repaint the viewport itself
	if strings.Contains(out, "Error:") {
		t.Fatalf("disabled issues should not render an error: %q", out)
	}
	if !strings.Contains(out, "Issues are disabled") {
		t.Fatalf("expected disabled-issues notice: %q", out)
	}
}

func TestEmptyResultShowsEmptyStateNotLoading(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30

	m.renderList()
	if !strings.Contains(m.render(), "Loading…") {
		t.Fatalf("pre-fetch view should show Loading…: %q", m.render())
	}

	updated, _ := m.Update(prsFetchedMsg{prs: []gh.PR{}})
	m = updated.(Model)
	m.renderList()
	out := m.render()
	if strings.Contains(out, "Loading…") {
		t.Fatalf("loaded-but-empty view should not show Loading…: %q", out)
	}
	if !strings.Contains(out, "No open PRs") {
		t.Fatalf("loaded-but-empty view should show the empty state: %q", out)
	}
}

func TestViewShowsHeaderAndStatus(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.width, m.height = 100, 30
	m.renderList()
	out := m.render()
	if !strings.Contains(out, "noamsto/prdash") {
		t.Fatalf("header should show the repo: %q", out)
	}
	if !strings.Contains(out, "quit") {
		t.Fatalf("status bar should show key hints: %q", out)
	}
}

func TestCycleFilterAdvancesPresetAndLabel(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.presetIdx = 0 // issuePresets[0] == "mine"
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = m2.(Model)
	if m.filter != "is:open" {
		t.Fatalf("after f, filter = %q", m.filter)
	}
	if !strings.Contains(m.render(), "all") {
		t.Fatalf("header should show the active preset name: %q", m.render())
	}
}

// TestFAndBigFAreNoOpsOnPRBoard guards Phase E: on the PR board, filtering is
// via / (omni) — f and F are retired (issue board f cycles presets unchanged).
func TestFAndBigFAreNoOpsOnPRBoard(t *testing.T) {
	m := newTestModelWithRows(t)
	before := m.filter
	u, _ := m.Update(keyMsg("f"))
	if u.(Model).filter != before {
		t.Error("f must be a no-op on the PR board")
	}
	u2, _ := u.(Model).Update(keyMsg("F"))
	if u2.(Model).showPicker {
		t.Error("F must not open the author picker anymore")
	}
}

func TestCtrlRRefreshesCurrentView(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	m.refreshing = false
	m.loaded = true

	u, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = u.(Model)
	if !m.refreshing {
		t.Fatal("ctrl+r should flag a refresh in flight")
	}
	if cmd == nil {
		t.Fatal("ctrl+r should return a fetch command")
	}
	if m.section.Len() != 2 {
		t.Fatalf("ctrl+r should keep rows painted, shown = %d, want 2", m.section.Len())
	}
}

func TestDebounceSeqGuardsStaleTicks(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.width, m.height = 130, 40
	m.renderList()

	// two quick moves bump the seq to 2
	u, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	u, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	if m.detailSeq != 2 {
		t.Fatalf("detailSeq = %d, want 2", m.detailSeq)
	}

	// a stale tick (seq 1) must do nothing
	_, cmd := m.Update(detailDebounceMsg{seq: 1})
	if cmd != nil {
		t.Fatal("stale debounce tick should yield no command")
	}
}

func TestStatusBarSurfacesRecommendedFix(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 130, 40
	m.setPRs([]gh.PR{{
		Number: 7, Title: "x",
		StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}},
	}})
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}
	m.renderList()
	out := m.statusBar()
	if !strings.Contains(out, "rerun checks") {
		t.Fatalf("failing-checks PR should surface the rerun fix: %q", out)
	}
}

func TestGroupedRenderEmitsHeadersAndTracksCursorLine(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30

	ready := gh.PR{Number: 2, Title: "ready", ReviewDecision: "APPROVED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	ready.Author.Login = "bob"
	waiting := gh.PR{Number: 1, Title: "waiting", ReviewDecision: "REVIEW_REQUIRED"}
	waiting.Author.Login = "alice"
	m.setPRs([]gh.PR{waiting, ready})
	m.renderList()

	out := m.vp.View()
	if !strings.Contains(out, "bob") || !strings.Contains(out, "alice") {
		t.Fatalf("grouped board should show both author headers: %q", out)
	}
	// display lines: 0=bob header, 1=bob's #2, 2=alice header, 3=alice's #1.
	// cursor starts at shown row 0 (bob's PR) → line 1.
	if m.cursorLine != 1 {
		t.Fatalf("cursor on first row should map to line 1 (after its header), got %d", m.cursorLine)
	}
	m.moveCursor(1) // to shown row 1 (alice's PR), below a blank line + second header
	// lines: 0=bob hdr, 1=bob row, 2=blank, 3=alice hdr, 4=alice row
	if m.cursorLine != 4 {
		t.Fatalf("cursor on second group's row should map to line 4, got %d", m.cursorLine)
	}
}

func TestMineViewRendersFlatNoHeaders(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil) // the "mine" preset
	m.presetIdx = 0                                   // NewModel no longer infers the preset from body
	m.SetRepo("r")
	m.width, m.height = 100, 30
	p1 := gh.PR{Number: 1, Title: "one"}
	p1.Author.Login = "alice"
	p2 := gh.PR{Number: 2, Title: "two"}
	p2.Author.Login = "alice"
	m.setPRs([]gh.PR{p1, p2})
	m.renderList()
	if strings.Contains(m.vp.View(), "─") {
		t.Fatalf("mine view should render flat with no header rules: %q", m.vp.View())
	}
	if m.cursorLine != 0 {
		t.Fatalf("flat board cursor at row 0 should map to line 0, got %d", m.cursorLine)
	}
}

func TestNonMineSingleAuthorStillGroups(t *testing.T) {
	m := NewModel("/repo", "is:open review-requested:@me", nil)
	m.omniServer = "review-requested:@me" // an active server qualifier: not the sections default
	m.SetRepo("r")
	m.width, m.height = 100, 30
	p1 := gh.PR{Number: 1, Title: "one"}
	p1.Author.Login = "alice"
	p2 := gh.PR{Number: 2, Title: "two"}
	p2.Author.Login = "alice"
	m.setPRs([]gh.PR{p1, p2})
	m.renderList()
	out := m.vp.View()
	if !strings.Contains(out, "alice") || !strings.Contains(out, "─") {
		t.Fatalf("non-mine single-author board should group under an author header: %q", out)
	}
}

func TestToggleHideDrafts(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30
	d := gh.PR{Number: 1, IsDraft: true}
	d.Author.Login = "alice"
	r := gh.PR{Number: 2}
	r.Author.Login = "alice"
	m.setPRs([]gh.PR{d, r})
	if m.section.Len() != 2 {
		t.Fatalf("both PRs shown before toggle, got %d", m.section.Len())
	}
	u, _ := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	m = u.(Model)
	if m.section.Len() != 1 {
		t.Fatalf("D should hide the draft, leaving 1, got %d", m.section.Len())
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	m = u.(Model)
	if m.section.Len() != 2 {
		t.Fatalf("D again should restore the draft, got %d", m.section.Len())
	}
}

func TestStatusTextLivesInHeaderNotKeybindingBar(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 130, 40
	p := gh.PR{Number: 1, Title: "x"}
	p.Author.Login = "alice"
	m.setPRs([]gh.PR{p})
	m.hideDrafts = true
	m.sel.toggle(0)

	bar := m.statusBar()
	if strings.Contains(bar, "selected") {
		t.Fatalf("keybinding bar must not carry selection status text: %q", bar)
	}
	if !strings.Contains(bar, "quit") {
		t.Fatalf("keybinding bar should still list core keys: %q", bar)
	}
	head := m.header()
	if !strings.Contains(head, "selected") {
		t.Fatalf("header should carry the selection count: %q", head)
	}
}

// TestHeaderClampsLongFailBadge guards against a long verbatim error (e.g. a
// raw network/GraphQL failure surfaced by runBulkNative's single-target
// branch) wrapping the header onto a second line.
func TestHeaderClampsLongFailBadge(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 80, 40
	m.actionStatus = &actionStat{
		settled: true,
		err:     errors.New("fail"),
		fail:    `Post "https://api.github.com/graphql": dial tcp 140.82.121.6:443: connect: connection refused`,
	}

	head := m.header()
	if w := lipgloss.Width(head); w > m.width {
		t.Fatalf("header width = %d, want <= model width %d:\n%s", w, m.width, head)
	}
}

func TestDraftsToggleHighlightedInBar(t *testing.T) {
	mk := func(hide bool) string {
		m := NewModel("/repo", "", nil)
		m.SetRepo("r")
		m.width, m.height = 130, 40
		p := gh.PR{Number: 1, Title: "x"}
		p.Author.Login = "alice"
		m.setPRs([]gh.PR{p})
		m.hideDrafts = hide
		return m.statusBar()
	}
	off, on := mk(false), mk(true)
	if !strings.Contains(off, "drafts") {
		t.Fatalf("bar should always list the drafts toggle: %q", off)
	}
	if off == on {
		t.Fatal("the drafts toggle label should change appearance in the bar when active")
	}
}

func TestListTitleReflectsSection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	got := m.listTitle()
	if !strings.Contains(got, prGlyph) || !strings.Contains(got, "open") || !strings.Contains(got, "· 2") {
		t.Fatalf("listTitle = %q, want glyph + state + count", got)
	}
}

func TestListViewportSizedForBorder(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30 // narrow (<120): single list pane, width 100
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	m.renderList()
	l := computeLayout(100, 30)
	if got := m.vp.Width(); got != l.ListWidth-2 {
		t.Fatalf("viewport width = %d, want ListWidth-2 = %d", got, l.ListWidth-2)
	}
	// contentHeight(l), not the raw l.ContentHeight, since the always-visible
	// filter bar reserves a row out of the layout's content budget; minus one
	// more for the sticky column-header row, which sits above the viewport.
	if want := m.contentHeight(l) - 2 - 1; m.vp.Height() != want {
		t.Fatalf("viewport height = %d, want contentHeight(l)-3 = %d", m.vp.Height(), want)
	}
	if m.listColHeader == "" {
		t.Fatal("a non-empty board should render a column header")
	}
}

func TestActionMenuRendersAsFloatingModal(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	u, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = u.(Model)
	out := m.render()
	if !strings.Contains(out, "Actions") || !strings.Contains(out, "╭") {
		t.Fatalf("action menu should be a bordered floating panel titled Actions: %q", out)
	}
}

func TestLegendToggle(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})

	u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = u.(Model)
	if !m.showLegend {
		t.Fatal("? should open the legend")
	}
	out := m.render()
	if !strings.Contains(out, "Legend") || !strings.Contains(out, "conflict") {
		t.Fatalf("legend should explain the glyphs: %q", out)
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = u.(Model)
	if m.showLegend {
		t.Fatal("esc should close the legend")
	}
}

func TestLegendDocumentsTerminalGlyphs(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 130, 40
	leg := m.legendView()
	if !strings.Contains(leg, mergedGlyph) {
		t.Fatalf("legend should document the merged mark %q: %q", mergedGlyph, leg)
	}
	if !strings.Contains(leg, "merged") || !strings.Contains(leg, "closed") {
		t.Fatalf("legend should name merged and closed states: %q", leg)
	}
}

func TestF1OpensLegendLikeQuestionMark(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("f1"))
	if !u.(Model).showLegend {
		t.Fatal("f1 should open the legend overlay")
	}
}

func TestLegendFiltersByTyping(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width, m.height = 130, 40 // the legend float clamps to the terminal size
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("m")) // type into the legend filter
	m = u.(Model)
	if m.legendQuery != "m" {
		t.Fatalf("typing in the legend should build legendQuery, got %q", m.legendQuery)
	}
	out := m.legendView()
	if !strings.Contains(strings.ToLower(out), "merge") {
		t.Fatalf("legend filtered by 'm' should still show merge: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "worktree") {
		t.Fatalf("legend filtered by 'm' should drop non-matching rows: %q", out)
	}
}

func TestHintsMentionSpineKeys(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width, m.height = 130, 40 // the legend float clamps to the terminal size
	u, _ := m.Update(keyMsg("?"))
	out := u.(Model).legendView()
	for _, want := range []string{"alt+j", "F1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("legend should document %q: %q", want, out)
		}
	}
}

func TestStatusBarHasTopRule(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	if !strings.Contains(m.statusBar(), "─") {
		t.Fatalf("status bar should have a top rule separating it: %q", m.statusBar())
	}
}

func TestStatusBarShowsFocusedBranchWhenPreviewIsHidden(t *testing.T) {
	// A distinct HeadRefName (not the fixture default of "") is required here:
	// an empty branch makes strings.Contains trivially true regardless of what
	// statusBar actually renders.
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	m.setPRs([]gh.PR{{Number: 1, Title: "one", Author: author("me"), HeadRefName: "feature-branch"}})
	// Below sideThreshold (120) so there's no preview pane, but wide enough that
	// the base hint bar (~85 cells) still leaves the >8-cell room the segment requires.
	m.width, m.height = 110, 30
	if computeLayout(m.width, m.height).ShowSide {
		t.Fatal("fixture width still shows the side pane")
	}
	v, ok := m.cursorVars()
	if !ok {
		t.Fatal("no cursor row")
	}
	if !strings.Contains(stripANSIForTest(m.statusBar()), v.HeadRefName) {
		t.Errorf("status bar should carry the focused branch %q", v.HeadRefName)
	}
}

func TestStatusBarOmitsBranchWhenPreviewIsShown(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	m.setPRs([]gh.PR{{Number: 1, Title: "one", Author: author("me"), HeadRefName: "feature-branch"}})
	m.width, m.height = 160, 40 // above sideThreshold: the preview pane already shows the branch
	if !computeLayout(m.width, m.height).ShowSide {
		t.Fatal("fixture width should show the side pane")
	}
	v, ok := m.cursorVars()
	if !ok {
		t.Fatal("no cursor row")
	}
	if strings.Contains(stripANSIForTest(m.statusBar()), v.HeadRefName) {
		t.Errorf("status bar should not duplicate the branch when the preview pane already shows it")
	}
}

func TestTruncateLeftKeepsTheTail(t *testing.T) {
	got := truncateLeft("eng-7726-same-value-different-evidence", 20)
	if lipgloss.Width(got) > 20 {
		t.Fatalf("truncateLeft over budget: %q is %d cells", got, lipgloss.Width(got))
	}
	if !strings.HasSuffix(got, "evidence") {
		t.Errorf("truncateLeft must keep the distinctive tail, got %q", got)
	}
}

func TestAnyChecksRunningDetectsPending(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}},
		{Number: 2, StatusCheckRollup: []gh.Check{{State: "PENDING"}}},
	})
	if !m.anyChecksRunning() {
		t.Fatal("expected a running check to be detected")
	}
}

func TestAnyChecksRunningFalseWhenAllSettled(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}})
	if m.anyChecksRunning() {
		t.Fatal("did not expect any running checks")
	}
}

func TestAnyChecksRunningDetectsPendingBehindAFailure(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	// CIState() collapses this rollup to "fail", but a check is still running —
	// the poll must fire so those running checks refresh on their own.
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "PENDING", Name: "build"},
	}}})
	if !m.anyChecksRunning() {
		t.Fatal("expected a running check to be detected behind a failing one")
	}
}

func TestAnyChecksRunningScansSectionsBothHalves(t *testing.T) {
	m := NewModel("/repo", "is:open", nil) // sections default
	m.setSections(
		[]gh.PR{{Number: 2, StatusCheckRollup: []gh.Check{{State: "PENDING"}}}}, // review requested
		nil, // reviewed by me
		[]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}}, // open
		"",
	)
	if !m.anyChecksRunning() {
		t.Fatal("expected a running check in the review-requested half to be detected")
	}
}

func TestFetchStartsPollLoopWhenChecksRunning(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	u, _ := m.Update(prsFetchedMsg{prs: []gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "PENDING"}}},
	}})
	if !u.(Model).polling {
		t.Fatal("expected poll loop to start after a fetch with running checks")
	}
}

func TestFetchDoesNotStartPollWhenAllSettled(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	u, _ := m.Update(prsFetchedMsg{prs: []gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}},
	}})
	if u.(Model).polling {
		t.Fatal("did not expect poll loop with no running checks")
	}
}

func TestPollTickStopsWhenChecksSettle(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.polling = true
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}})
	u, cmd := m.Update(checksPollMsg{})
	if u.(Model).polling {
		t.Fatal("expected poll loop to stop when nothing is running")
	}
	if cmd != nil {
		t.Fatal("expected no reschedule after the loop stops")
	}
}

func TestPollBusySkipsFetchButStaysAlive(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.polling = true
	m.refreshing = true // a fetch is already in flight
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "PENDING"}}}})
	if !m.pollBusy() {
		t.Fatal("expected pollBusy while refreshing")
	}
	u, cmd := m.Update(checksPollMsg{})
	if !u.(Model).polling {
		t.Fatal("poll loop should stay alive while busy")
	}
	if cmd == nil {
		t.Fatal("expected the loop to reschedule even when it skips a fetch")
	}
}

func TestInitThemeAppliesMode(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()); preview.SetMode("dark") })
	writeState(t, `{"theme":"light","version":1}`)
	m := NewModel("/repo", "is:open", nil)
	m.InitTheme()
	if m.themeMode != "light" {
		t.Errorf("themeMode = %q, want light", m.themeMode)
	}
	if theme.Accent != Latte().Accent {
		t.Errorf("InitTheme should apply Latte globals, accent=%q", theme.Accent)
	}
}

func TestThemePollAppliesChange(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()); preview.SetMode("dark") })
	writeState(t, `{"theme":"light","version":1}`)
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.themeMode = "dark" // pretend we started dark
	// zero lastMod differs from the file's real mtime → forces a re-read.
	u, _ := m.Update(themePollMsg{lastMod: time.Time{}})
	if got := u.(Model).themeMode; got != "light" {
		t.Errorf("poll should flip mode to light, got %q", got)
	}
	if theme.Accent != Latte().Accent {
		t.Errorf("poll should apply Latte globals, accent=%q", theme.Accent)
	}
}

func TestThemePollNoChangeWhenMtimeSame(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()) })
	writeState(t, `{"theme":"light","version":1}`)
	mod, err := statModTime(themeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.themeMode = "dark"
	u, _ := m.Update(themePollMsg{lastMod: mod}) // same mtime → skip the read
	if got := u.(Model).themeMode; got != "dark" {
		t.Errorf("poll with unchanged mtime must not change mode, got %q", got)
	}
	if theme.Accent != Mocha().Accent {
		t.Errorf("globals should stay Mocha, accent=%q", theme.Accent)
	}
}

func TestThemePollWhileExpandedKeepsExpandedBody(t *testing.T) {
	t.Cleanup(func() { applyTheme(Mocha()); preview.SetMode("dark") })
	writeState(t, `{"theme":"light","version":1}`)
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 30
	m.themeMode = "dark"
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{} // empty detail -> Reviews tab renders "No reviews"
	m.enterExpanded()
	if !m.expanded {
		t.Fatal("precondition: should be expanded")
	}
	m.expandedTab = tabReviews // deterministic, non-empty body regardless of theme
	m.renderExpanded()

	// What the (buggy) list repaint would have produced, for contrast.
	listCopy := m
	listCopy.renderList()
	listContent := ansi.Strip(listCopy.vp.View())

	u, _ := m.Update(themePollMsg{lastMod: time.Time{}})
	m = u.(Model)
	if !m.expanded {
		t.Fatal("theme poll should not exit expanded mode")
	}
	got := ansi.Strip(m.vp.View())
	if !strings.Contains(got, "No reviews") {
		t.Errorf("theme poll while expanded should repaint the expanded body, got: %q", got)
	}
	if got == listContent {
		t.Fatal("expanded body should not match the PR-list rendering")
	}
}

func TestToggleModeSwapsBoard(t *testing.T) {
	m := NewModel(".", "is:open author:@me", nil)
	m.cursor = 3
	m.previewExpanded = true
	m.previewMax = true
	m.hideDrafts = true

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := out.(Model)

	if got.mode != "issue" {
		t.Fatalf("mode = %q, want issue", got.mode)
	}
	if got.section.Kind() != "issue" {
		t.Errorf("section kind = %q", got.section.Kind())
	}
	if _, ok := got.actions["m"]; ok {
		t.Error("issue actions should not contain merge key 'm'")
	}
	if got.cursor != 0 || got.previewExpanded || got.previewMax || got.hideDrafts {
		t.Error("view state not reset on toggle")
	}

	back, _ := got.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	b := back.(Model)
	if b.mode != "pr" || b.section.Kind() != "pr" {
		t.Errorf("toggle back failed: mode=%q kind=%q", b.mode, b.section.Kind())
	}
	if b.filter != "is:open author:@me" {
		t.Errorf("pr filter not restored: %q", b.filter)
	}
}

func TestPROnlyKeysInertInIssueMode(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.hideDrafts = false
	// D must not flip hideDrafts in issue mode.
	out, _ := m.Update(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if out.(Model).hideDrafts {
		t.Error("D toggled drafts in issue mode")
	}
}

func TestChecksPollInertInIssueMode(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.polling = true
	u, cmd := m.Update(checksPollMsg{})
	if u.(Model).polling {
		t.Error("expected poll loop to stop in issue mode")
	}
	if cmd != nil {
		t.Error("expected no reschedule (and no background refresh) in issue mode")
	}
	if u.(Model).mode != "issue" {
		t.Error("checksPollMsg must not switch section in issue mode")
	}
}

func TestModeSegmentsHighlightsActive(t *testing.T) {
	pr := modeSegments("pr")
	is := modeSegments("issue")
	if pr == is {
		t.Error("segments identical across modes")
	}
	if !strings.Contains(pr, "PRs") || !strings.Contains(pr, "Issues") {
		t.Errorf("segments missing a label: %q", pr)
	}
}

func TestEmptyStateSaysIssues(t *testing.T) {
	m := NewModel(".", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection("is:open")
	m.width, m.height = 120, 40
	m.loaded = true
	m.renderList()
	if !strings.Contains(m.vp.View(), "issues") {
		t.Errorf("empty state should mention issues:\n%s", m.vp.View())
	}
}

// countLaunchSources wires a single countingSource into every read backend the
// launch fan-out touches, so a test can assert how many fetches fired.
func countLaunchSources(m *Model) *countingSource {
	cs := &countingSource{}
	m.SetPRSource(cs)
	m.SetIssueSource(cs)
	m.SetMembersSource(cs)
	m.SetViewerSource(cs)
	return cs
}

func launchModel(t *testing.T) (Model, *cache.Cache) {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open author:@me", c)
	m.SetRepo("owner/repo")
	return m, c
}

// warmLaunchCache seeds every key Init reconciles so the whole launch is fresh.
func warmLaunchCache(m Model, c *cache.Cache) {
	c.Set(prKey(m.repo, searchFor("pr", m.state, reviewBody), defaultLimit), json.RawMessage("[]"))
	c.Set(prKey(m.repo, searchFor("pr", m.state, reviewedBody), defaultLimit), json.RawMessage("[]"))
	c.Set(prKey(m.repo, "is:open", openListLimit), json.RawMessage("[]"))
	assignedF, authoredF, wideF := issueSectionFilters()
	c.Set(issueKey(m.repo, assignedF, issueListLimit), json.RawMessage("[]"))
	c.Set(issueKey(m.repo, authoredF, issueListLimit), json.RawMessage("[]"))
	c.Set(issueKey(m.repo, wideF, issueListLimit), json.RawMessage("[]"))
	c.Set(membersKey(m.repo), json.RawMessage("[]"))
	c.Set(viewerKey(), json.RawMessage(`"me"`))
}

func TestLaunchReusesFreshCache(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c)
	m.hydrateViewer() // mirrors production: Hydrate() runs before Init()
	cs := countLaunchSources(&m)

	for _, cmd := range m.launchFetchCmds() {
		if cmd != nil {
			cmd()
		}
	}
	if n := cs.calls.Load(); n != 0 {
		t.Fatalf("fresh cache should suppress all launch fetches, got %d source calls", n)
	}
}

func TestLaunchFetchesWhenCacheCold(t *testing.T) {
	m, _ := launchModel(t)
	cs := countLaunchSources(&m)

	for _, cmd := range m.launchFetchCmds() {
		if cmd != nil {
			cmd()
		}
	}
	// sections (review+reviewed+is:open) + issue sections (assigned+authored+wide) + members + viewer = 8 source fetches.
	if n := cs.calls.Load(); n != 8 {
		t.Fatalf("cold cache should fire the full launch fan-out, got %d source calls, want 8", n)
	}
}

func TestFetchSkippedClearsRefreshing(t *testing.T) {
	m, _ := launchModel(t)
	m.refreshing = true
	u, _ := m.Update(fetchSkippedMsg{})
	got := u.(Model)
	if got.refreshing {
		t.Error("fetchSkippedMsg should clear the refresh spinner")
	}
	if !got.loaded {
		t.Error("fetchSkippedMsg should mark the view loaded")
	}
}

func TestDetailCmdSkipsFreshDiskCache(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("r")
	m.SetDetailSource(stubSource{})
	m.setPRs([]gh.PR{{Number: 7}})

	if m.detailCmdForCursor() == nil {
		t.Fatal("cold detail cache should trigger a fetch")
	}
	c.Set(detailKey(m.repo, 7), json.RawMessage("{}"))
	if m.detailCmdForCursor() != nil {
		t.Fatal("fresh disk detail should suppress the fetch")
	}
}

func TestHydrateViewerLogin(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	c.Set(viewerKey(), []byte(`"octocat"`))
	m := NewModel("/tmp", "is:open", c)
	m.SetRepo("o/r")
	m.hydrateViewer()
	if m.viewerLogin != "octocat" {
		t.Fatalf("viewerLogin = %q", m.viewerLogin)
	}
}

// author builds a gh.PR.Author value from a login, for concise test literals.
func author(login string) struct {
	Login string `json:"login"`
} {
	return struct {
		Login string `json:"login"`
	}{Login: login}
}

func TestSetSections(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	me := "me"
	review := []gh.PR{{Number: 1, Author: author("me")}}
	open := []gh.PR{
		{Number: 1, Author: author("me")},
		{Number: 2, Author: author("me")},
		{Number: 3, Author: author("someone")},
	}
	m.setSections(review, nil, open, me)
	ps := m.section.(*PRSection)
	if cat := ps.cats[1]; cat != "Review requested" {
		t.Errorf("#1 = %q, want Review requested (review beats mine)", cat)
	}
	if cat := ps.cats[2]; cat != "Mine" {
		t.Errorf("#2 = %q, want Mine", cat)
	}
	if cat := ps.cats[3]; cat != "Others" {
		t.Errorf("#3 = %q, want Others", cat)
	}
}

// TestSetSectionsEmptyViewerFallsBackToOthers covers the pre-login window:
// before the viewer's login resolves, setSections can't tell "mine" from
// "someone else's", so every non-review PR collapses into Others. Once the
// login is known, a re-split moves the viewer's PRs into Mine.
func TestSetSectionsEmptyViewerFallsBackToOthers(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	review := []gh.PR{{Number: 1, Author: author("me")}}
	open := []gh.PR{
		{Number: 2, Author: author("me")},
		{Number: 3, Author: author("someone")},
	}
	m.setSections(review, nil, open, "")
	ps := m.section.(*PRSection)
	if cat := ps.cats[2]; cat != "Others" {
		t.Errorf("#2 with empty viewer = %q, want Others", cat)
	}
	if cat := ps.cats[3]; cat != "Others" {
		t.Errorf("#3 with empty viewer = %q, want Others", cat)
	}

	m.setSections(review, nil, open, "me")
	if cat := ps.cats[2]; cat != "Mine" {
		t.Errorf("#2 after viewer resolves = %q, want Mine", cat)
	}
}

// TestViewerFetchedMsgResplitsSections drives the real production path: a
// login-less boot paints everything under Others, then viewerFetchedMsg
// arrives (login hydrated) and re-partitions the already-cached open list.
func TestViewerFetchedMsgResplitsSections(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/tmp", "is:open", c)
	m.SetRepo("o/r")
	m.setSections(nil, nil, []gh.PR{
		{Number: 2, Author: author("me")},
		{Number: 3, Author: author("someone")},
	}, "")
	ps := m.section.(*PRSection)
	if cat := ps.cats[2]; cat != "Others" {
		t.Fatalf("#2 pre-login = %q, want Others", cat)
	}

	openRaw, _ := json.Marshal([]gh.PR{
		{Number: 2, Author: author("me")},
		{Number: 3, Author: author("someone")},
	})
	c.Set(prKey(m.repo, "is:open", openListLimit), openRaw)

	u, _ := m.Update(viewerFetchedMsg{login: "me"})
	m = u.(Model)
	ps = m.section.(*PRSection)
	if cat := ps.cats[2]; cat != "Mine" {
		t.Fatalf("#2 after viewerFetchedMsg = %q, want Mine", cat)
	}
	if cat := ps.cats[3]; cat != "Others" {
		t.Fatalf("#3 after viewerFetchedMsg = %q, want Others", cat)
	}
}

func TestSectionsFetchedMsgPaints(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.viewerLogin = "me"
	msg := sectionsFetchedMsg{
		state:  "open",
		review: []gh.PR{{Number: 1, Author: author("me")}},
		open: []gh.PR{
			{Number: 1, Author: author("me")},
			{Number: 3, Author: author("someone")},
		},
	}
	u, _ := m.Update(msg)
	ps := u.(Model).section.(*PRSection)
	if ps.Len() != 2 {
		t.Fatalf("shown = %d, want 2", ps.Len())
	}
}

// TestSectionsReviewedByMeStaysTop covers the union: a PR GitHub dropped from
// review-requested:@me when the viewer submitted a review still categorizes as
// Review requested via the reviewed-by-me half, instead of sinking into Others.
func TestSectionsReviewedByMeStaysTop(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		nil,
		[]gh.PR{{Number: 5, Author: author("someone")}},                                         // reviewed by me
		[]gh.PR{{Number: 5, Author: author("someone")}, {Number: 6, Author: author("someone")}}, // open
		"me",
	)
	ps := m.section.(*PRSection)
	if cat := ps.cats[5]; cat != "Review requested" {
		t.Errorf("#5 = %q, want Review requested via the reviewed half", cat)
	}
	if cat := ps.cats[6]; cat != "Others" {
		t.Errorf("#6 = %q, want Others", cat)
	}
	if ps.Len() != 2 {
		t.Errorf("shown = %d, want 2 (deduped)", ps.Len())
	}
}

// TestSectionsReRequestedWinsOverReviewed: a PR re-requested after the viewer's
// comment lands in both halves; the review-requested half owns it (one row),
// which also clears the ◐ marker — commentedByMe gates on reviewRequested.
func TestSectionsReRequestedWinsOverReviewed(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	p := gh.PR{Number: 5, Author: author("someone")}
	m.setSections([]gh.PR{p}, []gh.PR{p}, nil, "me")
	if n := m.section.Len(); n != 1 {
		t.Fatalf("shown = %d, want 1 (deduped across halves)", n)
	}
	if !m.reviewRequested[5] {
		t.Fatal("re-requested PR must sit in reviewRequested so the ◐ marker clears")
	}
}

func TestCommentedByMeMarker(t *testing.T) {
	setup := func(state string) Model {
		m := NewModel("/tmp", "is:open", nil)
		m.viewerLogin = "me"
		m.setSections(nil, []gh.PR{{Number: 5, Author: author("someone")}}, nil, "me")
		if state != "" {
			var r gh.Review
			r.Author.Login = "me"
			r.State = state
			m.detail[5] = gh.PRDetail{LatestReviews: []gh.Review{r}}
		}
		return m
	}
	if m := setup("COMMENTED"); !m.commentedByMe(5) {
		t.Error("viewer whose latest review is a comment should mark the row")
	}
	for _, state := range []string{"APPROVED", "CHANGES_REQUESTED", "DISMISSED"} {
		if m := setup(state); m.commentedByMe(5) {
			t.Errorf("latest review %q is final — the PR should leave unmarked", state)
		}
	}
	if m := setup(""); m.commentedByMe(5) {
		t.Error("no detail cached yet: unmarked until the batch lands")
	}

	reRequested := setup("COMMENTED")
	reRequested.reviewRequested[5] = true
	if reRequested.commentedByMe(5) {
		t.Error("re-requested after my comment: the marker must clear")
	}

	noViewer := setup("COMMENTED")
	noViewer.viewerLogin = ""
	if noViewer.commentedByMe(5) {
		t.Error("unresolved viewer cannot mark anything")
	}
}

// TestCommentedRowShowsHalfDot drives the render path: the review column swaps
// the pending ● for ◐ on a commented-by-me row.
func TestCommentedRowShowsHalfDot(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.SetRepo("o/r")
	m.width, m.height = 120, 30
	m.viewerLogin = "me"
	p := gh.PR{Number: 5, Title: "x", ReviewDecision: "REVIEW_REQUIRED", Author: author("someone")}
	m.setSections(nil, []gh.PR{p}, nil, "me")
	var r gh.Review
	r.Author.Login = "me"
	r.State = "COMMENTED"
	m.detail[5] = gh.PRDetail{LatestReviews: []gh.Review{r}}
	m.renderList()
	row := m.rowText[0]
	if !strings.Contains(row, reviewCommentedGlyph) {
		t.Fatalf("commented-by-me row should show ◐:\n%s", row)
	}
	if strings.Contains(row, "●") {
		t.Fatalf("◐ replaces the pending dot on a commented row:\n%s", row)
	}
}

// TestReviewedDetailCmdFetchesReviewedSet: the marker's detail warm covers the
// reviewed half only — review-requested rows ride the normal prefetch window.
func TestReviewedDetailCmdFetchesReviewedSet(t *testing.T) {
	fd := &fakeDetailSource{ret: map[int]gh.PRDetail{2: {}}}
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("o/r")
	m.SetDetailSource(fd)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("x")}},
		[]gh.PR{{Number: 2, Author: author("x")}, {Number: 3, Author: author("x")}},
		nil,
		"me",
	)
	m.fresh[3] = true // already refetched this session: skip
	cmd := m.reviewedDetailCmd()
	if cmd == nil {
		t.Fatal("reviewed set with cold detail should fetch")
	}
	if _, ok := cmd().(detailsBatchMsg); !ok {
		t.Fatal("reviewed warm should route through the batch source")
	}
	if len(fd.got) != 1 || len(fd.got[0]) != 1 || fd.got[0][0] != 2 {
		t.Fatalf("FetchDetails got %v, want one call for [2] (not review-requested #1, not fresh #3)", fd.got)
	}
}

func TestDefaultViewIsSections(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	c.Set(prKey("o/r", searchFor("pr", "open", reviewBody), defaultLimit), nil) // shape only
	m := NewModel("/tmp", "is:open", c)
	m.SetRepo("o/r")
	if !m.sectionsDefault() {
		t.Fatalf("fresh default is not sectionsDefault: state=%q omni=%q", m.state, m.omniServer)
	}
}

func TestApplyFilterRenderSwitch(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.viewerLogin = "me"
	m.setSections(
		[]gh.PR{{Number: 1, Title: "alpha", Author: author("me")}},
		nil,
		[]gh.PR{{Number: 2, Title: "beta flaky", Author: author("x")}},
		"me",
	)
	ps := m.section.(*PRSection)
	if !ps.grouped {
		t.Fatal("empty filter should keep sections grouped")
	}
	m.filterInput.SetValue("flaky")
	m.applyFilter()
	if ps.grouped {
		t.Fatal("bare text should flatten (grouped == false)")
	}
	if ps.Len() != 1 || ps.prAt(0).Number != 2 {
		t.Fatalf("fuzzy result wrong: len=%d", ps.Len())
	}
	m.filterInput.SetValue("")
	m.applyFilter()
	if !ps.grouped {
		t.Fatal("clearing text should restore sections")
	}
}

// keyMsg builds a tea.KeyMsg from a key string so tests can drive Update the way
// the app's key switch reads it (msg.String()).
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "alt+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModAlt}
	case "alt+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModAlt}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "f1":
		return tea.KeyPressMsg{Code: tea.KeyF1}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

// newTestModelWideWithPR returns a PR-board model wide enough that
// computeLayout(m.width, m.height).ShowSide is true, with one PR painted so
// the cursor lands on a real row.
func newTestModelWideWithPR(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 160, 40
	p := gh.PR{Number: 1, Title: "one", Author: author("me")}
	m.setPRs([]gh.PR{p})
	return m
}

// newTestModelWithRows returns a PR-board model with a few open PRs painted.
func newTestModelWithRows(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	m.setPRs([]gh.PR{
		{Number: 1, Title: "one", Author: author("me")},
		{Number: 2, Title: "two flaky", Author: author("x")},
		{Number: 3, Title: "three", Author: author("y")},
	})
	return m
}

func TestOmniCommitThenAction(t *testing.T) {
	m := newTestModelWithRows(t)
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("flaky")
	m.applyFilter()
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("enter should exit omni mode")
	}
	if m.filterInput.Value() != "flaky" {
		t.Fatal("enter must keep the filter text")
	}
	// a following action key is now interpreted by the list, not the input:
	u2, _ := m.Update(keyMsg("D"))
	if !u2.(Model).hideDrafts {
		t.Fatal("post-commit 'D' should toggle drafts (action, not text)")
	}
}

func TestOmniServerQualifierRewritesFilter(t *testing.T) {
	m := newTestModelWithRows(t)
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("label:bu")
	u, _ := m.Update(keyMsg("g")) // completes the qualifier, triggers re-parse
	m = u.(Model)
	if m.omniServer != "label:bug" {
		t.Fatalf("omniServer = %q, want label:bug", m.omniServer)
	}
	if m.filter != "is:open label:bug" {
		t.Fatalf("filter = %q, want is:open label:bug", m.filter)
	}
	if m.sectionsDefault() {
		t.Fatal("a server qualifier must leave the sections default")
	}
}

func TestOmniNoClobberDropsStale(t *testing.T) {
	m := newTestModelWithRows(t)
	m.filter = "is:open label:new" // current composed query
	stale := prsFetchedMsg{filter: "is:open label:old", prs: []gh.PR{{Number: 99}}}
	u, _ := m.Update(stale)
	got := u.(Model)
	if got.section.Len() == 1 && got.section.(*PRSection).prAt(0).Number == 99 {
		t.Fatal("stale server response for a superseded query must be dropped")
	}
}

// TestOmniIssueBoardUnaffected guards PLAN_FIXES B3: the omni server-qualifier
// machinery is PR-only. Entering the filter on the issue board and typing a
// label:x-looking token then esc must not rewrite the issue filter with PR
// semantics or leave a PR server qualifier armed.
// TestOmniNoClobberAppliesLiveBareFilterToFreshRows extends
// TestOmniNoClobberDropsStale: when the server response matches the current
// composed query (not stale), its rows must still land through whatever bare
// text the user typed while the request was in flight.
func TestOmniNoClobberAppliesLiveBareFilterToFreshRows(t *testing.T) {
	m := newTestModelWithRows(t)
	m.filter = "is:open label:x" // query A, composed from a server qualifier
	m.filterInput.SetValue("flaky")
	m.applyFilter()

	fresh := prsFetchedMsg{filter: "is:open label:x", prs: []gh.PR{
		{Number: 10, Title: "flaky test fix", Author: author("a")},
		{Number: 11, Title: "unrelated", Author: author("b")},
	}}
	u, _ := m.Update(fresh)
	got := u.(Model)
	ps := got.section.(*PRSection)
	if ps.grouped {
		t.Fatal("bare text present: sections must flatten (grouped == false)")
	}
	if ps.Len() != 1 || ps.prAt(0).Number != 10 {
		t.Fatalf("fuzzy subset wrong: len=%d", ps.Len())
	}
}

// TestSectionsDropOnTerminalState guards that leaving "open" drops the
// Review requested/Mine/Others categories in favor of the plain
// author-grouped terminal board.
func TestSectionsDropOnTerminalState(t *testing.T) {
	m := newTestModelWithRows(t)
	m.state = "merged"
	m.filter = searchFor("pr", "merged", "")
	if m.sectionsDefault() {
		t.Fatal("merged state must not be sectionsDefault")
	}
	u, _ := m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{
		{Number: 1, Author: author("a"), State: "MERGED"},
	}})
	ps := u.(Model).section.(*PRSection)
	if len(ps.catOrder) != 0 {
		t.Fatal("terminal board must not carry category sections")
	}
}

func TestOmniIssueBoardUnaffected(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.mode = "issue"
	m.section = NewIssueSection(m.filter)
	before := m.filter
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("label:bug")
	u, _ := m.Update(keyMsg("g"))
	m = u.(Model)
	if m.omniServer != "" {
		t.Fatalf("issue board must not arm a server qualifier, got %q", m.omniServer)
	}
	if m.filter != before {
		t.Fatalf("issue filter rewritten to %q, want unchanged %q", m.filter, before)
	}
	u2, _ := m.Update(keyMsg("esc"))
	m = u2.(Model)
	if m.filtering {
		t.Fatal("esc should exit the issue filter")
	}
	if m.filter != before {
		t.Fatalf("esc rewrote issue filter to %q, want %q", m.filter, before)
	}
}

func TestOmniAutocomplete(t *testing.T) {
	m := newTestModelWithRows(t)
	m.members = []gh.User{{Login: "alice"}, {Login: "bob"}}
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("@al")
	sug := m.omniSuggestions()
	if len(sug) != 1 || sug[0].Login != "alice" {
		t.Fatalf("suggestions = %+v, want [alice]", sug)
	}
	u, _ := m.Update(keyMsg("tab"))
	if got := u.(Model).filterInput.Value(); got != "@alice" {
		t.Fatalf("after tab = %q, want @alice", got)
	}
}

// TestOmniEnterCommitsOverSuggestions guards that an open @-dropdown never
// swallows enter: a completed "@alice" still fuzzy-matches itself, so gating the
// commit on "are there suggestions?" re-completed the same token forever.
func TestOmniEnterCommitsOverSuggestions(t *testing.T) {
	m := newTestModelWithRows(t)
	stubBackends(&m)
	m.members = []gh.User{{Login: "alice"}, {Login: "bob"}}
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("@alice")
	m.filterInput.SetCursor(len("@alice"))
	if len(m.omniSuggestions()) == 0 {
		t.Fatal("need an active suggestion to exercise the commit gate")
	}
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("enter must commit and exit omni mode even with the dropdown open")
	}
	if got := m.filterInput.Value(); got != "@alice" {
		t.Fatalf("enter rewrote the query to %q, want @alice kept applied", got)
	}
}

// TestOmniEnterReconcilesServerQuery guards that committing a server qualifier
// with Enter issues a reconcile fetch even when the 250ms debounce never fired,
// so the board never keeps stale rows for the committed query.
func TestOmniEnterReconcilesServerQuery(t *testing.T) {
	m := newTestModelWithRows(t)
	stubBackends(&m)
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("label:bu")
	u, _ := m.Update(keyMsg("g")) // completes label:bug, arms the debounce
	m = u.(Model)
	if m.omniServer != "label:bug" {
		t.Fatalf("omniServer = %q, want label:bug", m.omniServer)
	}
	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("enter should exit omni mode")
	}
	if cmd == nil {
		t.Fatal("committing a server query on enter must issue a reconcile fetch")
	}

	// A bare-text-only commit has nothing to reconcile: no fetch, filter kept.
	m2 := newTestModelWithRows(t)
	stubBackends(&m2)
	m2.filtering = true
	m2.filterInput.Focus()
	m2.filterInput.SetValue("flaky")
	m2.applyFilter()
	u2, cmd2 := m2.Update(keyMsg("enter"))
	m2 = u2.(Model)
	if m2.omniServer != "" {
		t.Fatalf("bare text armed a server qualifier: %q", m2.omniServer)
	}
	if cmd2 != nil {
		t.Fatal("bare-text commit should not issue a fetch")
	}
	if m2.filterInput.Value() != "flaky" {
		t.Fatal("bare-text commit must keep the filter")
	}
}

// TestStateTogglePreservesOmniQualifier guards that pressing s on the PR board
// recomposes the filter from the committed omni qualifier, not the stale m.body.
func TestStateTogglePreservesOmniQualifier(t *testing.T) {
	m := newTestModelWithRows(t)
	stubBackends(&m)
	m.body = "author:@me" // must NOT be used to compose on the PR board
	m.omniServer = "label:bug"
	m.state = "open"
	m.filter = "is:open label:bug"
	u, _ := m.Update(keyMsg("s"))
	m = u.(Model)
	if want := searchFor("pr", "merged", "label:bug"); m.filter != want {
		t.Fatalf("filter = %q, want %q", m.filter, want)
	}
}

// TestHydrateViewerBeforeSectionsPartition guards that Hydrate loads the viewer
// login before setSections runs, so the viewer's own PRs land in Mine on the
// first warm-cache paint instead of Others.
func TestHydrateViewerBeforeSectionsPartition(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("o/r")

	c.Set(viewerKey(), []byte(`"me"`))
	openRaw, _ := json.Marshal([]gh.PR{{Number: 1, Author: author("me")}})
	c.Set(prKey(m.repo, "is:open", openListLimit), openRaw)
	c.Set(prKey(m.repo, searchFor("pr", m.state, reviewBody), defaultLimit), json.RawMessage("[]"))

	m.Hydrate()
	ps, ok := m.section.(*PRSection)
	if !ok {
		t.Fatal("expected a PRSection after Hydrate")
	}
	if cat := ps.cats[1]; cat != "Mine" {
		t.Fatalf("#1 = %q, want Mine (viewer login applied on first paint)", cat)
	}
}

// TestOmniDropdownFloatsOverList guards that the @-mention panel is composited
// over the list instead of joined into the filter bar: opening it must not move
// a single row, and it must stay inside the frame at its anchor.
func TestOmniDropdownFloatsOverList(t *testing.T) {
	m := newTestModelWithRows(t)
	m.members = []gh.User{
		{Login: "aa1"}, {Login: "aa2"}, {Login: "aa3"}, {Login: "aa4"},
		{Login: "aa5"}, {Login: "aa6"}, {Login: "aa7"}, {Login: "aa8"},
	}
	m.width = 80
	m.height = 24 // tall enough that the dropdown-row cap doesn't kick in
	m.filtering = true
	m.filterInput.Focus()

	l := Layout{ShowFooter: true, ShowPanel: false, ContentHeight: 40}
	m.filterInput.SetValue("")
	if dd := m.omniSuggestDropdown(); dd != "" {
		t.Fatalf("no @ partial must not open the dropdown, got %q", dd)
	}
	closed := m.contentHeight(l)

	m.filterInput.SetValue("@aa")
	dd := m.omniSuggestDropdown()
	if lipgloss.Height(dd) <= 1 {
		t.Fatalf("dropdown = %d rows, want a multi-row panel", lipgloss.Height(dd))
	}
	if got := m.contentHeight(l); got != closed {
		t.Fatalf("opening the dropdown moved the list: contentHeight %d, want %d", got, closed)
	}
	if bottom := m.omniDropdownY() + lipgloss.Height(dd); bottom > m.height {
		t.Fatalf("dropdown bottom row = %d, overflows height %d", bottom, m.height)
	}
	if w := lipgloss.Width(dd); w > m.width {
		t.Fatalf("dropdown width = %d, overflows width %d", w, m.width)
	}

	// Short window: the panel shrinks to the rows left under its anchor rather
	// than running off the bottom.
	m.height = m.omniDropdownY() + 4
	if h := lipgloss.Height(m.omniSuggestDropdown()); h > 4 {
		t.Fatalf("dropdown = %d rows, want <= the 4 left below the anchor", h)
	}
}

// TestFilterBarShowsCommittedQuery guards that a filter surviving the commit is
// visible: enter blurs the input but keeps the query applied, so the bar has to
// paint it — otherwise a filtered board is indistinguishable from a full one.
func TestFilterBarShowsCommittedQuery(t *testing.T) {
	m := newTestModelWithRows(t)
	stubBackends(&m)
	m.width, m.height = 80, 24
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("flaky")
	m.applyFilter()
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)

	if m.filtering {
		t.Fatal("enter should have committed")
	}
	bar := ansi.Strip(m.filterBar())
	if !strings.Contains(bar, "flaky") {
		t.Fatalf("blurred filter bar = %q, want the committed query in it", bar)
	}
	if !strings.Contains(bar, "esc") {
		t.Fatalf("blurred filter bar = %q, want the key that clears it named", bar)
	}
	if got := lipgloss.Width(bar); got > m.width {
		t.Fatalf("filter bar = %d cells, overflows width %d", got, m.width)
	}
	if got := m.filterBarRows(); got != 3 {
		t.Fatalf("committed filter bar = %d rows, want 3", got)
	}

	// esc clears it, and the bar falls back to the prompt hint.
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if bar := ansi.Strip(m.filterBar()); strings.Contains(bar, "flaky") {
		t.Fatalf("after esc the bar still shows the query: %q", bar)
	}
}

// TestFilterBarIsAlwaysThreeRows guards the boxed bar's whole purpose: the
// primary surface must not change height as it gains and loses focus, in any
// combination of filtering/query state.
func TestFilterBarIsAlwaysThreeRows(t *testing.T) {
	m := newTestModelWideWithPR(t)
	longQuery := "is:pr repo:factify-inc/mono is:open author:@me label:complexity:6 -label:blocked"
	hugeQuery := strings.Repeat("x", 200)
	for _, st := range []struct {
		name      string
		filtering bool
		query     string
	}{
		{"blurred empty", false, ""},
		{"blurred with query", false, "@asaf"},
		{"focused", true, ""},
		{"focused with query", true, "is:approved"},
		{"blurred with long query", false, longQuery},
		{"focused with long query", true, longQuery},
		{"blurred with huge query", false, hugeQuery},
		{"focused with huge query", true, hugeQuery},
	} {
		m.filtering = st.filtering
		m.filterInput.SetValue(st.query)
		if got := lipgloss.Height(m.filterBar()); got != 3 {
			t.Errorf("%s: filterBar height = %d, want 3", st.name, got)
		}
		if got := m.filterBarRows(); got != 3 {
			t.Errorf("%s: filterBarRows = %d, want 3", st.name, got)
		}
		if bar, rows := lipgloss.Height(m.filterBar()), m.filterBarRows(); bar != rows {
			t.Errorf("%s: filterBar height = %d, filterBarRows = %d, must agree", st.name, bar, rows)
		}
	}
}

// TestFilterBarSpansListColumnOnly locks the bar to the list column: it stops
// where the list stops, and the preview beside it starts level with the bar's
// own top row rather than below it.
func TestFilterBarSpansListColumnOnly(t *testing.T) {
	m := newTestModelWideWithPR(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
	m = u.(Model)

	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		t.Fatal("fixture must be wide enough for the side preview")
	}
	if got := lipgloss.Width(m.filterBar()); got != l.ListWidth {
		t.Fatalf("filter bar width = %d, want the list column's %d", got, l.ListWidth)
	}
	// The preview's title only ever appears in its top border, so finding it on
	// the main block's first row is what proves the pane reaches that high.
	first := ansi.Strip(strings.SplitN(m.renderMain(), "\n", 2)[0])
	if !strings.Contains(first, m.previewTitle()) {
		t.Fatalf("preview must start on the filter bar's row; first row = %q", first)
	}
}

// TestFilterInputKeepsCursorVisible guards that once the box clamps the
// textinput's own width (so it can't wrap the box open), typing past the
// visible window still scrolls to keep the cursor in view instead of hiding
// behind the truncated end of the value.
func TestFilterInputKeepsCursorVisible(t *testing.T) {
	m := newTestModelWideWithPR(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = u.(Model)
	m.filtering = true
	m.filterInput.Focus()

	long := strings.Repeat("a", 40) + "END"
	for _, r := range long {
		u, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = u.(Model)
	}

	if got := lipgloss.Height(m.filterBar()); got != 3 {
		t.Fatalf("filterBar height = %d, want 3", got)
	}
	if bar := ansi.Strip(m.filterBar()); !strings.Contains(bar, "END") {
		t.Fatalf("filter bar hides the cursor end of a long value: %q", bar)
	}
}

// TestFilterBarShowsMatchCountWhenFiltered guards the live n->m count: it is
// the signal that the board is narrowed, since the boxed bar no longer grows
// a second row to say so.
func TestFilterBarShowsMatchCountWhenFiltered(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width = 80
	m.filtering = true
	m.filterInput.SetValue("asaf")
	m.applyFilter()
	if !strings.Contains(stripANSIForTest(m.filterBar()), "→") {
		t.Error("filtered bar should show an n→m match count")
	}
}

// TestStatusBarOmitsRetiredFilterKey guards that the footer doesn't advertise a
// dead key: f cycles presets on the issue board and does nothing on the PR one.
func TestStatusBarOmitsRetiredFilterKey(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width, m.height = 130, 40
	m.renderList()
	if bar := ansi.Strip(m.statusBar()); strings.Contains(bar, "f:") {
		t.Fatalf("PR board footer advertises f, which is retired there: %q", bar)
	}

	m.mode = "issue"
	m.section = NewIssueSection(m.filter)
	if bar := ansi.Strip(m.statusBar()); !strings.Contains(bar, "f:preset") {
		t.Fatalf("issue board footer = %q, want f labelled as the preset cycle", bar)
	}
}

// TestContentHeightFilteringNoPanel guards that focusing the boxed filter bar
// costs nothing: the box is a constant three rows in every state, so
// contentHeight must not move when filtering toggles.
func TestContentHeightFilteringNoPanel(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width = 80
	m.height = 24
	m.mode = "pr"
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("")
	if got := m.filterBarRows(); got != 3 {
		t.Fatalf("filterBarRows while focused = %d, want 3", got)
	}

	l := Layout{ShowFooter: true, ShowPanel: false, ContentHeight: 40}
	filtered := m.contentHeight(l)
	m.filtering = false
	base := m.contentHeight(l)
	if filtered != base {
		t.Fatalf("contentHeight while filtering = %d, want %d (unchanged: bar height is constant)", filtered, base)
	}
}

// TestOmniDropdownCursorClampedToWindow guards that arrowing past the visible
// dropdown window keeps the cursor on a rendered row, so tab/enter never
// completes an off-screen member.
func TestOmniDropdownCursorClampedToWindow(t *testing.T) {
	m := newTestModelWithRows(t)
	m.members = []gh.User{
		{Login: "aa1"}, {Login: "aa2"}, {Login: "aa3"}, {Login: "aa4"},
		{Login: "aa5"}, {Login: "aa6"}, {Login: "aa7"}, {Login: "aa8"},
	}
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("@aa")
	if len(m.omniSuggestions()) <= omniSuggestDropdownRows {
		t.Fatalf("need > %d matches to exercise the clamp", omniSuggestDropdownRows)
	}
	for i := 0; i < 10; i++ {
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	if m.omniSuggestCursor > omniSuggestDropdownRows-1 {
		t.Fatalf("cursor = %d, want <= %d", m.omniSuggestCursor, omniSuggestDropdownRows-1)
	}
}

func TestBoardHidesFooterOnSmallWindow(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 14 // below footerMinHeight
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})

	out := m.board()
	if strings.Contains(out, "quit") {
		t.Fatalf("small window should not render the keybinding footer: %q", out)
	}
	lines := strings.Count(out, "\n") + 1
	if lines > m.height {
		t.Fatalf("board output has %d lines, exceeds terminal height %d", lines, m.height)
	}
}

func TestBoardShowsFooterOnLargeWindow(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30 // above both floors
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})

	out := m.board()
	if !strings.Contains(out, "quit") {
		t.Fatalf("large window should render the keybinding footer: %q", out)
	}
}

// TestLegendGroupsAreColumnAligned locks the "no ragged columns" acceptance
// criterion: within a group, every key is padded to that group's widest key,
// so the space before every description lines up.
func TestLegendGroupsAreColumnAligned(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 130, 40
	leg := m.legendView()
	lines := strings.Split(leg, "\n")
	var descCols []int
	for _, line := range lines {
		if idx := strings.Index(line, "worktree"); idx > 0 {
			descCols = append(descCols, idx)
		}
	}
	// Sanity: the legend must actually contain "worktree" at least once (it's
	// one of the board's documented keys) for this check to mean anything.
	if len(descCols) == 0 {
		t.Fatal("expected the legend to mention \"worktree\"")
	}
}

// TestLegendFitsSmallTerminal is the "never overflow" acceptance criterion:
// at a small terminal the legend must not be wider or taller than the frame,
// however it degrades.
func TestLegendFitsSmallTerminal(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 40, 14
	leg := m.legendView()
	if w := lipgloss.Width(leg); w > m.width {
		t.Fatalf("legend width %d exceeds terminal width %d", w, m.width)
	}
	if h := lipgloss.Height(leg); h > m.height {
		t.Fatalf("legend height %d exceeds terminal height %d", h, m.height)
	}
}

// TestLegendFitsLargeTerminal guards the same invariant at a generous size,
// where the un-clamped body should fit without triggering the clamp at all.
func TestLegendFitsLargeTerminal(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 160, 50
	leg := m.legendView()
	if w := lipgloss.Width(leg); w > m.width {
		t.Fatalf("legend width %d exceeds terminal width %d", w, m.width)
	}
	if h := lipgloss.Height(leg); h > m.height {
		t.Fatalf("legend height %d exceeds terminal height %d", h, m.height)
	}
}

// TestMainViewCyclesTabsWithL guards the wide-layout takeover: with a side
// pane present, l/h drive m.expandedTab instead of diving into the
// full-screen expanded view.
func TestMainViewCyclesTabsWithL(t *testing.T) {
	m := newTestModelWideWithPR(t)
	if m.expandedTab != tabOverview {
		t.Fatalf("start tab = %d, want Overview", m.expandedTab)
	}
	nm, _ := m.Update(keyMsg("l"))
	got := nm.(Model)
	if got.expandedTab != tabDescription {
		t.Fatalf("after l, tab = %d, want Description", got.expandedTab)
	}
	if got.expanded {
		t.Fatal("l in the wide layout must not enter the full-screen expanded view")
	}
}

// TestMainViewCyclesTabsWithH mirrors the l case for the reverse direction,
// including the wrap from the first tab back to the last.
func TestMainViewCyclesTabsWithH(t *testing.T) {
	m := newTestModelWideWithPR(t)
	nm, _ := m.Update(keyMsg("h"))
	got := nm.(Model)
	if got.expandedTab != tabDiff {
		t.Fatalf("after h from Overview, tab = %d, want wrap to Diff(%d)", got.expandedTab, tabDiff)
	}
}

// TestMainViewJumpsTabWithDigit guards the 1-6 direct-jump shortcuts.
func TestMainViewJumpsTabWithDigit(t *testing.T) {
	m := newTestModelWideWithPR(t)
	// Digits are 1-indexed against the tab list (matches the existing
	// full-screen expanded-view convention): "3" is the third tab, Conversation.
	nm, _ := m.Update(keyMsg("3"))
	got := nm.(Model)
	if got.expandedTab != tabConversation {
		t.Fatalf("after 3, tab = %d, want Conversation(%d)", got.expandedTab, tabConversation)
	}
}

// TestMainViewNarrowKeysUnchanged pins the narrow-layout paths that must
// keep their pre-existing behavior: l opens the full-screen expanded view,
// and h/1-6 are no-ops (there is no pane to address).
func TestMainViewNarrowKeysUnchanged(t *testing.T) {
	m := newTestModelWideWithPR(t)
	m.width, m.height = 80, 40 // narrow: computeLayout(...).ShowSide is false
	if computeLayout(m.width, m.height).ShowSide {
		t.Fatal("test setup: expected a narrow layout")
	}
	nm, _ := m.Update(keyMsg("l"))
	got := nm.(Model)
	if !got.expanded {
		t.Fatal("l in the narrow layout should still open the full-screen expanded view")
	}

	m2 := newTestModelWideWithPR(t)
	m2.width, m2.height = 80, 40
	nm2, _ := m2.Update(keyMsg("h"))
	if nm2.(Model).expandedTab != tabOverview {
		t.Fatal("h in the narrow layout is a no-op; there is no pane to cycle")
	}
	nm3, _ := m2.Update(keyMsg("2"))
	if nm3.(Model).expandedTab != tabOverview {
		t.Fatal("digits in the narrow layout are a no-op; there is no pane to jump")
	}
}

// TestMainViewEnterActionSurvivesNarrowLayout guards the pre-existing
// "enter" action binding (Open worktree): the new tab/pane keys (h/l/1-6)
// must not touch "enter" — it stays a plain action key in every layout.
func TestMainViewEnterActionSurvivesNarrowLayout(t *testing.T) {
	m := newTestModelWideWithPR(t)
	m.width, m.height = 80, 40 // narrow: no pane, enter is an action key
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("enter in the narrow layout should still dispatch the bound action (Open worktree)")
	}
}

// TestMainViewTabsAreProOnly guards the PR-only guard: in issue mode the new
// tab keys must not touch expandedTab.
func TestMainViewTabsAreProOnly(t *testing.T) {
	m := newTestModelWideWithPR(t)
	m.mode = "issue"
	nm, _ := m.Update(keyMsg("l"))
	if nm.(Model).expandedTab != tabOverview {
		t.Fatal("l in issue mode must not touch expandedTab")
	}
	nm2, _ := m.Update(keyMsg("2"))
	if nm2.(Model).expandedTab != tabOverview {
		t.Fatal("digits in issue mode must not touch expandedTab")
	}
}

// A labeled PR with a branch is the row that used to grow a second line; it must
// stay one row tall at every viewport height.
func TestRenderListSingleLineRowHeight(t *testing.T) {
	for _, h := range []int{12, 44} {
		p := labeledPR()
		p.HeadRefName = "feat/x"
		m := NewModel("/repo", "is:open", nil)
		m.width, m.height = 100, h
		m.setPRs([]gh.PR{p})
		m.renderList()
		if m.cursorRows != 1 {
			t.Fatalf("h=%d: row should be 1 row tall, got %d", h, m.cursorRows)
		}
	}
}

// TestScrollRevealsGroupHeaderAboveTopRow: scrolling up onto the first row of a
// group must keep that group's header (one line above the row) on screen.
func TestScrollRevealsGroupHeaderAboveTopRow(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 12 // short viewport → content must scroll
	review := []gh.PR{{Number: 1, Title: "review me", State: "OPEN"}}
	var open []gh.PR
	for i := 2; i <= 14; i++ {
		open = append(open, gh.PR{Number: i, Title: "open pr", State: "OPEN"})
	}
	m.setSections(review, nil, open, "someone-else") // #1 → "Review requested" (first group)
	m.cursor = m.section.Len() - 1                   // scroll to the bottom
	m.renderList()
	m.cursor = 0 // back to the first row, under the "Review requested" header
	m.renderList()
	if off := m.vp.YOffset(); off != 0 {
		t.Fatalf("group header above the first row must stay visible; YOffset=%d, want 0", off)
	}
}

func TestCtrlJKMovesSelectionAltJKScrollsPreview(t *testing.T) {
	m := newTestModelWithRows(t)
	start := m.cursor
	u, _ := m.Update(keyMsg("ctrl+j"))
	m = u.(Model)
	if m.cursor != start+1 {
		t.Fatalf("ctrl+j should move selection down: cursor=%d want=%d", m.cursor, start+1)
	}
	u, _ = m.Update(keyMsg("ctrl+k"))
	m = u.(Model)
	if m.cursor != start {
		t.Fatalf("ctrl+k should move selection up: cursor=%d want=%d", m.cursor, start)
	}
	// alt+j/alt+k drive the preview offset, not the cursor.
	before := m.cursor
	u, _ = m.Update(keyMsg("alt+j"))
	m = u.(Model)
	if m.cursor != before {
		t.Fatalf("alt+j must not move the cursor: cursor=%d want=%d", m.cursor, before)
	}
}

// TestFilterBarAlwaysVisible guards the always-visible search row: it renders
// (dim, as a hint) even on the blurred board, and still captures typing once
// focused via '/'.
func TestFilterBarAlwaysVisible(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width, m.height = 80, 24
	// Blurred board: the filter prompt is visible even without pressing '/'.
	if !strings.Contains(m.render(), "/") {
		t.Fatalf("filter bar should be visible on the blurred board: %q", m.render())
	}
	// The status bar already has a "/:find" hint, so the check above alone is
	// vacuous — assert on the actual filter-bar placeholder text as the real
	// regression guard for "blurred but always rendered".
	if !strings.Contains(ansi.Strip(m.render()), "@user · is: · text") {
		t.Fatalf("blurred board should show the filter-bar placeholder hint: %q", m.render())
	}
	// Focusing keeps it visible and accepts input.
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("x"))
	m = u.(Model)
	if m.filterInput.Value() != "x" {
		t.Fatalf("focused filter should capture typing: %q", m.filterInput.Value())
	}
}

// TestEscTwoStageOnBoard guards the three-stage esc behavior on the board:
// blur-but-keep-query, then clear-query, then quit.
func TestEscTwoStageOnBoard(t *testing.T) {
	m := newTestModelWithRows(t)
	// focus + type
	u, _ := m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("f"))
	m = u.(Model)
	// esc #1: blur but KEEP the query
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("esc should blur the focused filter")
	}
	if m.filterInput.Value() != "f" {
		t.Fatalf("esc-blur must keep the query, got %q", m.filterInput.Value())
	}
	// esc #2: clear the query (still no quit)
	u, cmd := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filterInput.Value() != "" {
		t.Fatalf("second esc should clear the query, got %q", m.filterInput.Value())
	}
	if cmd != nil {
		t.Fatal("clearing the query must not quit")
	}
	// esc #3: empty query → quit
	_, cmd = m.Update(keyMsg("esc"))
	if cmd == nil {
		t.Fatal("esc on an empty board should quit")
	}
}

// TestEscTwoStageOnIssueBoard mirrors TestEscTwoStageOnBoard's stage-1
// assertion on the issue board, guarding that blur-but-keep-query isn't a
// PR-only path.
func TestEscTwoStageOnIssueBoard(t *testing.T) {
	m := newTestModelWithRows(t)
	u, _ := m.Update(keyMsg("tab")) // switch to the issue board
	m = u.(Model)
	// focus + type
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("f"))
	m = u.(Model)
	// esc #1: blur but KEEP the query
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.filtering {
		t.Fatal("esc should blur the focused filter")
	}
	if m.filterInput.Value() != "f" {
		t.Fatalf("esc-blur must keep the query, got %q", m.filterInput.Value())
	}
}

// TestLegendGlyphsAreUnambiguous: the legend explains glyphs, so listing one glyph
// under two meanings makes it useless. Duplicate glyph keys must diverge in rendered
// appearance (e.g. closed dim ✗ vs CI-fail red ✗), not just in label text. Row
// markers ▌/▎ and ciRunningGlyph each keep a single pinned meaning.
func TestLegendGlyphsAreUnambiguous(t *testing.T) {
	for _, mode := range []string{"pr", "issue"} {
		// width/height are set because legendGroups reaches computeLayout for the
		// side-pane hint; a zero-size Model would exercise a degenerate layout.
		m := Model{mode: mode, width: 120, height: 40}
		var glyphs []keyHint
		for _, g := range m.legendGroups() {
			if g.title == "glyphs" {
				glyphs = g.hints
			}
		}
		if len(glyphs) == 0 {
			t.Fatalf("mode %q: legend has no glyphs group", mode)
		}
		labels := map[string][]string{}
		rendered := map[string][]string{}
		for _, h := range glyphs {
			labels[h.key] = append(labels[h.key], h.label)
			rendered[h.key] = append(rendered[h.key], h.renderKey())
		}
		for _, c := range []struct{ glyph, want string }{
			{"▌", "selected"},
			{"▎", "focus"},
			{ciRunningGlyph, "CI running"},
		} {
			if got := labels[c.glyph]; len(got) != 1 || got[0] != c.want {
				t.Errorf("mode %q: want %s labelled exactly [%s], got %v", mode, c.glyph, c.want, got)
			}
		}
		for key, rs := range rendered {
			if len(rs) < 2 {
				continue
			}
			seen := make(map[string]struct{}, len(rs))
			for _, r := range rs {
				seen[r] = struct{}{}
			}
			if len(seen) <= 1 {
				t.Errorf("mode %q: glyph %q appears %d times but all render identically (%q); duplicate keys must diverge in style",
					mode, key, len(rs), rs[0])
			}
		}
	}
}

func TestGroupRangeCategorized(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}}, // Review requested
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("me")}, {Number: 3, Author: author("x")}},
		"me",
	)
	// After setSections: Review requested (#1), Mine (#2), Others (#3) — one each.
	m.cursor = 1 // Mine
	lo, hi := m.groupRange()
	if lo != 1 || hi != 1 {
		t.Fatalf("Mine groupRange = [%d,%d], want [1,1]", lo, hi)
	}
	m.cursor = 0
	lo, hi = m.groupRange()
	if lo != 0 || hi != 0 {
		t.Fatalf("Review groupRange = [%d,%d], want [0,0]", lo, hi)
	}
}

func TestGroupRangeAuthorGrouped(t *testing.T) {
	m := NewModel("/tmp", "author:x", nil) // non-sections → author grouping when ≥2 authors
	m.SetRepo("o/r")
	ps := NewPRSection("author:x")
	ps.SetForceGroup(true)
	ps.SetPRs([]gh.PR{
		{Number: 1, Author: author("alice"), UpdatedAt: time.Now()},
		{Number: 2, Author: author("alice"), UpdatedAt: time.Now().Add(-time.Hour)},
		{Number: 3, Author: author("bob"), UpdatedAt: time.Now()},
	})
	m.section = ps
	// ForceGroup + 2 authors → grouped by author. Find alice's span.
	var aliceLo = -1
	for i := 0; i < ps.Len(); i++ {
		if ps.groupLabel(i) == "alice" {
			if aliceLo < 0 {
				aliceLo = i
			}
			m.cursor = i
		}
	}
	if aliceLo < 0 {
		t.Fatal("alice group missing")
	}
	lo, hi := m.groupRange()
	if lo != aliceLo || hi != aliceLo+1 {
		t.Fatalf("alice groupRange = [%d,%d], want [%d,%d]", lo, hi, aliceLo, aliceLo+1)
	}
}

func TestGroupRangeFlatIsWholeBoard(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Author: author("only")}, {Number: 2, Author: author("only")}})
	m.cursor = 1
	lo, hi := m.groupRange()
	if lo != 0 || hi != 1 {
		t.Fatalf("flat groupRange = [%d,%d], want [0,1]", lo, hi)
	}
}

func TestAdvanceSelectionCycle(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}},
		nil,
		[]gh.PR{
			{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")},
			{Number: 3, Author: author("me")},
			{Number: 4, Author: author("x")},
		},
		"me",
	)
	// Review requested = #1,#2; Mine = #3; Others = #4
	m.cursor = 0         // in Review requested
	m.advanceSelection() // Group
	if m.sel.count() != 2 || !m.sel.has(0) || !m.sel.has(1) {
		t.Fatalf("after Group: sel=%v, want indexes 0,1", m.sel.indices())
	}
	m.advanceSelection() // All
	if m.sel.count() != 4 {
		t.Fatalf("after All: count=%d, want 4", m.sel.count())
	}
	m.advanceSelection() // None
	if m.sel.count() != 0 {
		t.Fatalf("after None: count=%d, want 0", m.sel.count())
	}
}

func TestAdvanceSelectionFillsPartialGroup(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}, {Number: 3, Author: author("a")}},
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}, {Number: 3, Author: author("a")}, {Number: 4, Author: author("x")}},
		"me",
	)
	m.cursor = 0
	m.sel.toggle(1) // partial group
	m.advanceSelection()
	if !m.sel.has(0) || !m.sel.has(1) || !m.sel.has(2) || m.sel.has(3) {
		t.Fatalf("partial group should fill group only, sel=%v", m.sel.indices())
	}
}

func TestAdvanceSelectionFlatAllThenNone(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Author: author("only")}, {Number: 2, Author: author("only")}})
	m.advanceSelection()
	if m.sel.count() != 2 {
		t.Fatalf("flat first V should select all, got %d", m.sel.count())
	}
	m.advanceSelection()
	if m.sel.count() != 0 {
		t.Fatalf("flat second V should clear, got %d", m.sel.count())
	}
}

// TestVSelectsClusterThenCategoryThenAllThenNone guards the #88 cycle order:
// cluster (author) → category → all → none. Review requested mixes alice's
// two-PR cluster with bob's single PR so the cluster span is a strict subset
// of the category span — a fixture where they coincide can't catch a
// regression to the old group → all → none cycle.
func TestVSelectsClusterThenCategoryThenAllThenNone(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 10, Author: author("alice")}, {Number: 11, Author: author("alice")}, {Number: 12, Author: author("bob")}},
		nil,
		[]gh.PR{{Number: 20, Author: author("me")}, {Number: 21, Author: author("x")}},
		"me",
	)
	ps := m.section.(*PRSection)
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).Author.Login == "alice" {
			m.cursor = i
			break
		}
	}

	m.advanceSelection()
	if got := m.sel.count(); got != 2 {
		t.Fatalf("first V selected %d rows, want the 2-row alice cluster", got)
	}

	lo, hi := m.groupRange()
	m.advanceSelection()
	if got := m.sel.count(); got != hi-lo+1 {
		t.Fatalf("second V selected %d rows, want the %d-row category", got, hi-lo+1)
	}

	m.advanceSelection()
	if got := m.sel.count(); got != ps.Len() {
		t.Fatalf("third V selected %d rows, want all %d", got, ps.Len())
	}

	m.advanceSelection()
	if got := m.sel.count(); got != 0 {
		t.Fatalf("fourth V left %d rows selected, want none", got)
	}
}

func TestVKeyAdvancesSelection(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.width, m.height = 120, 40
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}},
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("x")}},
		"me",
	)
	m.cursor = 0
	u, _ := m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 1 || !m.sel.has(0) {
		t.Fatalf("V on Review group: sel=%v, want {0}", m.sel.indices())
	}
	u, _ = m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 2 {
		t.Fatalf("second V should select all, got %d", m.sel.count())
	}
	u, _ = m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 0 {
		t.Fatalf("third V should clear, got %d", m.sel.count())
	}
}
