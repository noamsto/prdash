package ui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
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
	if !strings.Contains(out, "rerun failed") {
		t.Fatalf("failing-checks PR should surface the rerun fix: %q", out)
	}
}
