package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

func TestSpaceTogglesSelection(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = u.(Model)

	if m.sel.count() != 1 {
		t.Fatalf("space did not select the cursor row: count=%d", m.sel.count())
	}
}

func TestPresetSwitchPaintsCachedRowsImmediately(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw, _ := json.Marshal([]gh.PR{{Number: 99, Title: "cached-all"}})
	c.Set(prKey("x", "is:open", openListLimit), raw) // the sections default reads is:open at openListLimit

	m := NewModel("/repo", "is:open author:@me", c)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "mine"}}) // current preset's rows
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"}) // mine → all
	m = u.(Model)

	if m.filter != "is:open" {
		t.Fatalf("filter=%q", m.filter)
	}
	ps := m.section.(*PRSection)
	if len(ps.prs) != 1 || ps.prs[0].Number != 99 {
		t.Fatalf("switch did not paint cached rows before fetch: %+v", ps.prs)
	}
}

func TestBackgroundFetchCachesWithoutClobbering(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open author:@me", c)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "mine"}})
	m.loaded = true

	other := "is:open review-requested:@me"
	raw, _ := json.Marshal([]gh.PR{{Number: 50}})
	u, _ := m.Update(prsFetchedMsg{filter: other, prs: []gh.PR{{Number: 50}}, raw: raw})
	m = u.(Model)

	ps := m.section.(*PRSection)
	if len(ps.prs) != 1 || ps.prs[0].Number != 1 {
		t.Fatalf("background fetch clobbered the current view: %+v", ps.prs)
	}
	if _, ok := c.Get(prKey("x", other, defaultLimit)); !ok {
		t.Fatal("background fetch did not populate the cache")
	}
}

func TestStateToggleRecomputesFilter(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "merged" || m.filter != "is:merged author:@me" {
		t.Fatalf("s toggle: state=%q filter=%q", m.state, m.filter)
	}
	u, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.state != "closed" || m.filter != "is:closed is:unmerged author:@me" {
		t.Fatalf("second s toggle: state=%q filter=%q", m.state, m.filter)
	}
}

func TestMineFetchedCachesPerState(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open author:@me", c) // mine view, open
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.loaded = true

	// A merged-state mine result arriving while viewing open: cache only, no repaint.
	mineRaw, _ := json.Marshal([]gh.PR{{Number: 7}})
	revRaw, _ := json.Marshal([]gh.PR{})
	u, _ := m.Update(mineFetchedMsg{state: "merged", mine: []gh.PR{{Number: 7}}, mineRaw: mineRaw, reviewRaw: revRaw})
	m = u.(Model)

	if _, ok := c.Get(prKey("x", "is:merged author:@me", defaultLimit)); !ok {
		t.Fatal("merged mine result not cached under its per-state key")
	}
	if ps := m.section.(*PRSection); ps.Len() != 0 {
		t.Fatalf("merged prewarm should not repaint the open view, got %d rows", ps.Len())
	}
}

func TestMutatingActionRefetchesAndRevalidates(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{}) // returns "[]"; backgroundRefresh just needs non-nil
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 42}})
	m.renderList()
	m.refreshing = false // NewModel starts true; clear so the assertion is meaningful
	m.fresh[42] = true
	m.actionStatus = &actionStat{run: "Updating", ok: "Updated", fail: "Failed", refresh: true, nums: []int{42}}

	u, cmd := m.Update(actionDoneMsg{})
	m = u.(Model)

	if m.fresh[42] {
		t.Fatal("successful mutating action should clear detail freshness for #42")
	}
	if !m.refreshing {
		t.Fatal("successful mutating action should trigger a refetch (refreshing=true)")
	}
	if cmd == nil {
		t.Fatal("expected a refetch command batch")
	}
}

func TestFailedMutatingActionDoesNotRefetch(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{})
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 42}})
	m.refreshing = false // NewModel starts true; clear so the assertion is meaningful
	m.fresh[42] = true
	m.actionStatus = &actionStat{run: "Updating", ok: "Updated", fail: "Failed", refresh: true, nums: []int{42}}

	u, _ := m.Update(actionDoneMsg{err: fmt.Errorf("boom")})
	m = u.(Model)

	if !m.fresh[42] {
		t.Fatal("failed action must not clear freshness")
	}
	if m.refreshing {
		t.Fatal("failed action must not refetch")
	}
}

func TestMineViewSections(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.width, m.height = 130, 40
	m.setMine(
		[]gh.PR{{Number: 1, Title: "my pr"}, {Number: 2, Title: "also mine"}},
		[]gh.PR{{Number: 2, Title: "also mine"}, {Number: 9, Title: "please review"}}, // #2 authored+requested → stays Mine
	)
	m.renderList()

	ps := m.section.(*PRSection)
	if ps.Len() != 3 {
		t.Fatalf("deduped mine view should show 3 PRs, got %d", ps.Len())
	}
	out := m.render()
	if !strings.Contains(out, "Mine") || !strings.Contains(out, "Review requested") {
		t.Fatalf("mine view should show both section headers:\n%s", out)
	}
}

func TestInlineActionShowsFeedback(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}})
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"}) // update-branch (inline argv)
	m = u.(Model)
	if !m.actionRunning() {
		t.Fatal("dispatching an inline action should show it running")
	}

	u, _ = m.Update(actionDoneMsg{err: nil})
	m = u.(Model)
	if m.actionRunning() || m.actionStatus == nil {
		t.Fatal("completion should mark the status done, not running")
	}
	if !strings.Contains(m.render(), "Branch updated") {
		t.Fatalf("header should surface the finished action in past tense:\n%s", m.render())
	}

	u, _ = m.Update(actionClearMsg{})
	m = u.(Model)
	if m.actionStatus != nil {
		t.Fatal("clear should drop the settled status")
	}
}

func TestBatchCopyJoinsSelectedRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.setPRs([]gh.PR{{Number: 1, URL: "u1"}, {Number: 2, URL: "u2"}, {Number: 3, URL: "u3"}})
	m.sel.toggle(0)
	m.sel.toggle(2)

	if got := m.copyPayload("copy-url"); got != "u1\nu3" {
		t.Fatalf("batch copy = %q, want %q", got, "u1\nu3")
	}
}

func TestCopyNumberPayload(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.setPRs([]gh.PR{{Number: 2443}})
	if got := m.copyPayload("copy-number"); got != "#2443" {
		t.Fatalf("copy-number = %q, want #2443", got)
	}
}

func TestBulkWorktreeWarnsOverFour(t *testing.T) {
	t.Setenv("PRDASH_ACTION_FILE", "")
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 40
	prs := make([]gh.PR, 6)
	for i := range prs {
		prs[i] = gh.PR{Number: i + 1, HeadRefName: fmt.Sprintf("b%d", i)}
	}
	m.setPRs(prs)
	for i := 0; i < 5; i++ {
		m.sel.toggle(i)
	}

	W := m.actions["W"]
	if cmd := m.startBulk(W); cmd != nil || m.pending == nil {
		t.Fatal("opening 5 worktrees should prompt before running")
	}
	m.confirmAnswer(true)
	if len(m.pendingExec) != 5 {
		t.Fatalf("after confirm want 5 worktree commands queued, got %d", len(m.pendingExec))
	}
}

func TestPRListCacheScopedByRepo(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "results.json"))
	filter := "is:open author:@me"

	a := NewModel("/a", filter, c)
	a.SetRepo("owner/repo-a")
	a.Update(prsFetchedMsg{filter: filter, raw: []byte(`[{"number":1}]`), prs: []gh.PR{{Number: 1}}})

	b := NewModel("/b", filter, c)
	b.SetRepo("owner/repo-b")
	if prs, ok := b.cachedPRs(filter, defaultLimit); ok {
		t.Fatalf("repo-b must not hydrate repo-a's cached PR list; got %d rows", len(prs))
	}
}

type recordRunner struct{ calls [][]string }

func (r *recordRunner) Run(_ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	return []byte("[]"), nil
}

func TestBulkInlineRunsPerSelected(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	rr := &recordRunner{}
	m.SetRunner(rr)
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.sel.toggle(0)
	m.sel.toggle(2)

	cmd := m.startBulk(m.actions["u"]) // update-branch: inline, per-selected
	if cmd == nil {
		t.Fatal("bulk inline action should return a command")
	}
	if m.actionStatus == nil || m.actionStatus.run != "Updating branch ×2" {
		t.Fatalf("running badge run = %q, want %q", m.actionStatus.run, "Updating branch ×2")
	}
	if m.sel.count() != 0 {
		t.Fatalf("bulk should consume the selection, %d left", m.sel.count())
	}

	// Drive the batched command so the runner actually fires per PR.
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
	if len(rr.calls) != 2 {
		t.Fatalf("want one gh call per selected PR (2), got %d: %v", len(rr.calls), rr.calls)
	}
	for _, args := range rr.calls {
		if len(args) < 2 || args[0] != "pr" || args[1] != "update-branch" {
			t.Fatalf("unexpected gh call: %v", args)
		}
	}
}

func TestPanelBatchModeShowsOnlyBatchActions(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 50
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	m.sel.toggle(0)
	m.sel.toggle(1)

	panel := m.keysActionsPanel(m.width)
	if !strings.Contains(panel, "BATCH") {
		t.Fatalf("selection should flip the panel to batch mode:\n%s", panel)
	}
	if !strings.Contains(panel, "Copy URL") {
		t.Fatalf("batch mode should keep the copy actions:\n%s", panel)
	}
	if !strings.Contains(panel, "Merge") {
		t.Fatalf("batch mode should include bulk-capable merge:\n%s", panel)
	}
	if !strings.Contains(panel, "Open in browser") {
		t.Fatalf("batch mode should include bulk-capable open-in-browser:\n%s", panel)
	}
	if strings.Contains(panel, "Rerun checks") {
		t.Fatalf("batch mode should hide single-only actions like rerun-checks:\n%s", panel)
	}
}

func TestLayoutReservesPanelByHeight(t *testing.T) {
	if !computeLayout(120, 50).ShowPanel {
		t.Fatal("a tall terminal should reserve the keys/actions panel")
	}
	if computeLayout(120, 16).ShowPanel {
		t.Fatal("a short terminal should fall back to the one-line status bar")
	}
}

func TestKeysActionsPanelListsKeysAndActions(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 50
	m.setPRs([]gh.PR{{Number: 1, Title: "hi"}})
	m.renderList()

	panel := m.keysActionsPanel(m.width)
	if !strings.Contains(panel, "move") {
		t.Fatalf("panel missing navigation keys:\n%s", panel)
	}
	if !strings.Contains(panel, "worktree") { // the enter action's label
		t.Fatalf("panel missing focused-PR actions:\n%s", panel)
	}
}

func TestMembersHydrateFromCache(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw := []byte(`[{"login":"octocat"},{"login":"hubber"}]`)
	c.Set(membersKey("noamsto/prdash"), raw)

	m := NewModel("/repo", "is:open", c)
	m.SetRepo("noamsto/prdash")
	m.Hydrate()

	if len(m.members) != 2 || m.members[0].Login != "octocat" {
		t.Fatalf("launch did not hydrate cached members: %+v", m.members)
	}
}

func TestActionPaneHeightIsConstantWhileFiltering(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}})
	m.showActions = true

	full := strings.Count(m.render(), "\n")
	m.actionFilter.SetValue("merge") // narrows to one action
	narrowed := strings.Count(m.render(), "\n")

	if full != narrowed {
		t.Fatalf("action pane height changed with filter: %d vs %d lines", full, narrowed)
	}
}

type stubRunner struct{}

func (stubRunner) Run(string, ...string) ([]byte, error) { return []byte("[]"), nil }

func TestDetailHydratesFromCacheOnLaunch(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw := []byte(`{"mergeStateStatus":"CLEAN","mergeable":"MERGEABLE","reviewRequests":[],"comments":[],"reviews":[],"latestReviews":[],"files":[]}`)
	c.Set(detailKey("noamsto/prdash", 7), raw)

	m := NewModel("/repo", "is:open", c)
	m.SetRepo("noamsto/prdash")
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.hydrateDetail()

	if _, ok := m.detail[7]; !ok {
		t.Fatal("launch hydrate did not paint cached detail — preview would show Loading…")
	}
}

func TestCachedDetailStillTriggersRefetch(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.SetRunner(stubRunner{})
	m.setPRs([]gh.PR{{Number: 7}})
	m.detail[7] = gh.PRDetail{} // painted from disk cache, but not refreshed this session

	if cmd := m.detailCmdForCursor(); cmd == nil {
		t.Fatal("stale (non-fresh) cursor detail must still refetch to revalidate")
	}
}

func TestExitActionWithoutHandoffQueuesExec(t *testing.T) {
	t.Setenv("PRDASH_ACTION_FILE", "") // no orchestrator sink

	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi", HeadRefName: "feat/x"}})
	m.renderList()

	u, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)

	got := m.PendingExec()
	if len(got) != 1 {
		t.Fatalf("enter should queue one exec command standalone, got %d", len(got))
	}
	if want := []string{"wt", "switch", "feat/x"}; !slices.Equal(got[0], want) {
		t.Fatalf("queued %v, want %v", got[0], want)
	}
	if cmd == nil {
		t.Fatal("enter should still quit the TUI")
	}
}

func TestExitActionWithHandoffDoesNotQueueExec(t *testing.T) {
	p := filepath.Join(t.TempDir(), "handoff")
	t.Setenv("PRDASH_ACTION_FILE", p)

	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("noamsto/prdash")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi", HeadRefName: "feat/x"}})
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)

	if len(m.PendingExec()) != 0 {
		t.Fatalf("with a handoff sink present, exec must not be queued: %v", m.PendingExec())
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("handoff file not written: %v", err)
	}
	if !strings.Contains(string(b), "enter") {
		t.Fatalf("handoff line missing the action key: %q", b)
	}
}

func TestHeaderShowsRefreshingDuringFetch(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1}})
	m.loaded = true
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = u.(Model)
	if !strings.Contains(m.render(), "refreshing") {
		t.Fatalf("switching presets should show a refreshing indicator:\n%s", m.render())
	}

	u, _ = m.Update(prsFetchedMsg{filter: m.filter, prs: []gh.PR{{Number: 2}}})
	m = u.(Model)
	if strings.Contains(m.render(), "refreshing") {
		t.Fatalf("indicator should clear once the fetch lands:\n%s", m.render())
	}
}
