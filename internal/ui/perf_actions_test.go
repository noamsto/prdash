package ui

import (
	"encoding/json"
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
	raw, _ := json.Marshal([]gh.PR{{Number: 99, Title: "cached-review"}})
	c.Set(cache.Key("pr", "is:open review-requested:@me", defaultLimit, schemaVer), raw)

	m := NewModel("/repo", "is:open author:@me", c)
	m.SetRepo("x")
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1, Title: "mine"}}) // current preset's rows
	m.renderList()

	u, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = u.(Model)

	if m.filter != "is:open review-requested:@me" {
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
	if _, ok := c.Get(cache.Key("pr", other, defaultLimit, schemaVer)); !ok {
		t.Fatal("background fetch did not populate the cache")
	}
}

func TestWarmFiltersCoversAllPresetsCurrentFirst(t *testing.T) {
	got := warmFilters("is:open review-requested:@me")
	if len(got) != len(defaultPresets) {
		t.Fatalf("warmFilters returned %d filters, want %d", len(got), len(defaultPresets))
	}
	if got[0] != "is:open review-requested:@me" {
		t.Fatalf("current filter should be warmed first, got %q", got[0])
	}
	for _, p := range defaultPresets {
		if !containsStr(got, p.search) {
			t.Fatalf("preset %q missing from warm list %v", p.search, got)
		}
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

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
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
	if want := []string{"wt", "switch", "pr:7"}; !slices.Equal(got[0], want) {
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
