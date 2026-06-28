package ui

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

type nopRunner struct{}

func (nopRunner) Run(string, ...string) ([]byte, error) { return nil, nil }

func TestDetailCmdsPrefetchesNeighbors(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRunner(nopRunner{})
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.cursor = 1
	if m.detailCmds() == nil {
		t.Fatal("uncached neighbors should be fetched")
	}
	m.detail[1], m.detail[2], m.detail[3] = gh.PRDetail{}, gh.PRDetail{}, gh.PRDetail{}
	if m.detailCmds() != nil {
		t.Fatal("all cached → no fetch")
	}
}

func TestFilterSwitchClearsStaleRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRunner(nopRunner{})
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	if m.section.Len() != 2 {
		t.Fatalf("precondition: want 2 rows, got %d", m.section.Len())
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(Model)
	if m.section.Len() != 0 {
		t.Fatalf("f should clear the previous filter's rows during refetch, got %d", m.section.Len())
	}
}

func TestErrorToastSurfacedWithPRs(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.err = errors.New("merge failed: not mergeable")
	if !strings.Contains(m.render(), "merge failed") {
		t.Fatalf("action error must be visible even with a non-empty list: %q", m.render())
	}
}

func TestErrorClearedOnKeypress(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRunner(nopRunner{})
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7}, {Number: 8}})
	m.err = errors.New("boom")
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if updated.(Model).err != nil {
		t.Fatal("a keypress should dismiss the error toast")
	}
}

func TestConfirmModalShown(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7}})
	a := m.actions["m"]
	m.pending = &a
	out := m.render()
	if !strings.Contains(out, "Merge") || !strings.Contains(out, "y confirm") {
		t.Fatalf("confirm modal should prompt prominently: %q", out)
	}
}

func TestHydrateLoadsCachedDetail(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	listRaw, _ := json.Marshal([]gh.PR{{Number: 7, Title: "hi"}})
	c.Set(cache.Key("pr", "is:open", defaultLimit, schemaVer), listRaw)
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("o/r")
	detRaw, _ := json.Marshal(gh.PRDetail{MergeStateStatus: "CLEAN"})
	c.Set(m.detailKey(7), detRaw)

	m.Hydrate()
	if d, ok := m.detail[7]; !ok || d.MergeStateStatus != "CLEAN" {
		t.Fatalf("hydrate should load cached detail, got %+v ok=%v", d, ok)
	}
}

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
	c.Set(cache.Key("pr", "is:open", 20, schemaVer), raw)

	m := NewModel("/repo", "is:open", c)
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
	if !strings.Contains(out, "q quit") {
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
	if m.filter != "is:open review-requested:@me" {
		t.Fatalf("after f, filter = %q", m.filter)
	}
	if !strings.Contains(m.render(), "review-requested") {
		t.Fatalf("header should show the active preset name: %q", m.render())
	}
}
