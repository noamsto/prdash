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
	if !strings.Contains(m.section.RenderRow(0, RowOpts{Width: 80}), "#7") {
		t.Fatalf("first row should render #7")
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
	if issueKey("r", "is:open") == prKey("r", "is:open", defaultLimit) {
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
	if got := m.vp.Height(); got != l.ContentHeight-2 {
		t.Fatalf("viewport height = %d, want ContentHeight-2 = %d", got, l.ContentHeight-2)
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
		t.Fatal("a key should close the legend")
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

func TestStatusBarHasTopRule(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("r")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "x"}})
	if !strings.Contains(m.statusBar(), "─") {
		t.Fatalf("status bar should have a top rule separating it: %q", m.statusBar())
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

// countingRunner records how many gh invocations a command tree makes, so a
// test can assert whether a fresh cache suppressed the launch fetches.
type countingRunner struct{ calls int }

func (r *countingRunner) Run(string, ...string) ([]byte, error) {
	r.calls++
	return []byte("[]"), nil
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
	c.Set(prKey(m.repo, "is:open", openListLimit), json.RawMessage("[]"))
	c.Set(issueKey(m.repo, searchFor("issue", "open", assigneeBody)), json.RawMessage("[]"))
	c.Set(membersKey(m.repo), json.RawMessage("[]"))
	c.Set(viewerKey(), json.RawMessage(`"me"`))
}

func TestLaunchReusesFreshCache(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c)
	m.hydrateViewer() // mirrors production: Hydrate() runs before Init()
	rec := &countingRunner{}
	m.SetRunner(rec)

	for _, cmd := range m.launchFetchCmds() {
		if cmd != nil {
			cmd()
		}
	}
	if rec.calls != 0 {
		t.Fatalf("fresh cache should suppress all launch fetches, got %d gh calls", rec.calls)
	}
}

func TestLaunchFetchesWhenCacheCold(t *testing.T) {
	m, _ := launchModel(t)
	rec := &countingRunner{}
	m.SetRunner(rec)

	for _, cmd := range m.launchFetchCmds() {
		if cmd != nil {
			cmd()
		}
	}
	// sections (review+is:open) + issues + members + viewer = 5 gh invocations.
	if rec.calls != 5 {
		t.Fatalf("cold cache should fire the full launch fan-out, got %d gh calls, want 5", rec.calls)
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
	m.SetRunner(stubRunner{})
	m.setPRs([]gh.PR{{Number: 7}})

	if m.detailCmdForCursor() == nil {
		t.Fatal("cold detail cache should trigger a fetch")
	}
	c.Set(detailKey(m.repo, 7), json.RawMessage("{}"))
	if m.detailCmdForCursor() != nil {
		t.Fatal("fresh disk detail should suppress the fetch")
	}
}

func TestPrefetchSkipsFreshDiskDetails(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("r")
	m.SetRunner(stubRunner{})
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})

	if m.prefetchCmd() == nil {
		t.Fatal("cold window should prefetch detail")
	}
	c.Set(detailKey(m.repo, 1), json.RawMessage("{}"))
	c.Set(detailKey(m.repo, 2), json.RawMessage("{}"))
	if m.prefetchCmd() != nil {
		t.Fatal("all-fresh window should skip prefetch entirely")
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
	m.setSections(review, open, me)
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
	m.setSections(review, open, "")
	ps := m.section.(*PRSection)
	if cat := ps.cats[2]; cat != "Others" {
		t.Errorf("#2 with empty viewer = %q, want Others", cat)
	}
	if cat := ps.cats[3]; cat != "Others" {
		t.Errorf("#3 with empty viewer = %q, want Others", cat)
	}

	m.setSections(review, open, "me")
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
	m.setSections(nil, []gh.PR{
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

// TestOmniEnterReconcilesServerQuery guards that committing a server qualifier
// with Enter issues a reconcile fetch even when the 250ms debounce never fired,
// so the board never keeps stale rows for the committed query.
func TestOmniEnterReconcilesServerQuery(t *testing.T) {
	m := newTestModelWithRows(t)
	m.SetRunner(stubRunner{})
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
	m2.SetRunner(stubRunner{})
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
	m.SetRunner(stubRunner{})
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

// TestOmniHintRowsReservesDropdown guards that the height render() draws under
// the filter input is reserved by contentHeight, so the dropdown/hint never
// overflows the frame.
func TestOmniHintRowsReservesDropdown(t *testing.T) {
	m := newTestModelWithRows(t)
	m.members = []gh.User{
		{Login: "aa1"}, {Login: "aa2"}, {Login: "aa3"}, {Login: "aa4"},
		{Login: "aa5"}, {Login: "aa6"}, {Login: "aa7"}, {Login: "aa8"},
	}
	m.width = 80
	m.height = 24 // tall enough that the dropdown-row cap (FIX B) doesn't kick in
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("@aa")
	if got, want := m.omniHintRows(), lipgloss.Height(m.omniSuggestDropdown()); got != want {
		t.Fatalf("omniHintRows with dropdown = %d, want %d", got, want)
	}
	if m.omniHintRows() <= 1 {
		t.Fatalf("omniHintRows with a full dropdown = %d, want > 1", m.omniHintRows())
	}

	m.filterInput.SetValue("") // no @ partial: falls back to the static hint line
	if got := m.omniHintRows(); got != 1 {
		t.Fatalf("omniHintRows without a partial = %d, want 1", got)
	}

	m.mode = "issue"
	if got := m.omniHintRows(); got != 0 {
		t.Fatalf("omniHintRows on the issue board = %d, want 0", got)
	}

	// contentHeight shrinks by exactly the reserved rows, minus the +1 the
	// filter input reclaims from the statusBar footer it replaces.
	m.mode = "pr"
	m.filterInput.SetValue("@aa")
	l := Layout{ShowFooter: true, ShowPanel: false, ContentHeight: 40}
	filtered := m.contentHeight(l)
	m.filtering = false
	base := m.contentHeight(l)
	m.filtering = true
	if base-filtered != m.omniHintRows()-1 {
		t.Fatalf("contentHeight delta = %d, want %d", base-filtered, m.omniHintRows()-1)
	}
}

// TestContentHeightFilteringNoPanel guards FIX A: with no docked panel, the
// 2-line statusBar footer is replaced by the 1-line filter input, so filtering
// with only the static hint line (omniHintRows()==1) reclaims exactly enough
// to match the non-filtering baseline.
func TestContentHeightFilteringNoPanel(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width = 80
	m.height = 24
	m.mode = "pr"
	m.filtering = true
	m.filterInput.Focus()
	m.filterInput.SetValue("") // no @ partial: omniHintRows() == 1
	if got := m.omniHintRows(); got != 1 {
		t.Fatalf("omniHintRows = %d, want 1", got)
	}

	l := Layout{ShowFooter: true, ShowPanel: false, ContentHeight: 40}
	filtered := m.contentHeight(l)
	m.filtering = false
	base := m.contentHeight(l)
	if filtered != base {
		t.Fatalf("contentHeight while filtering = %d, want %d (baseline)", filtered, base)
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
