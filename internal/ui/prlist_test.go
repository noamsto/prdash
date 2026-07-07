package ui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

func TestHydrateFromCache(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw, _ := json.Marshal([]gh.PR{{Number: 42, Title: "cached"}})

	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	c.Set(prKey(m.repo, "is:open"), raw)
	m.hydrate()
	sec := m.section.(*PRSection)
	if len(sec.prs) != 1 || sec.prs[0].Number != 42 {
		t.Fatalf("hydrate did not paint cached rows: %+v", sec.prs)
	}
	if m.section.Len() != 1 {
		t.Fatal("section not painted from cache")
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
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 100, 30
	if m.presetIdx != 0 {
		t.Fatalf("initial presetIdx = %d, want 0 (mine)", m.presetIdx)
	}
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = m2.(Model)
	if m.filter != "is:open" {
		t.Fatalf("after f, filter = %q", m.filter)
	}
	if !strings.Contains(m.render(), "all") {
		t.Fatalf("header should show the active preset name: %q", m.render())
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
	m := NewModel("/repo", "is:open review-requested:@me", nil) // a non-"mine" preset
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
	if got := m.listTitle(); got != "PRs · 2" {
		t.Fatalf("listTitle = %q, want %q", got, "PRs · 2")
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

func TestAnyChecksRunningScansMineViewBothHalves(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil) // the mine preset
	m.setMine(
		[]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}},
		[]gh.PR{{Number: 2, StatusCheckRollup: []gh.Check{{State: "PENDING"}}}},
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
