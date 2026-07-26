package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

func rowCacheModel(t *testing.T) Model {
	t.Helper()
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	m := NewModel("/repo", "is:open", c)
	m.SetRepo("owner/repo")
	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	return u.(Model)
}

// TestRowCacheReflectsContentChange guards the invalidation: a content change at
// the same row count must not serve a stale cached row. setPRs → applyFilter
// bumps rowGen, so every row re-renders.
func TestRowCacheReflectsContentChange(t *testing.T) {
	m := rowCacheModel(t)
	m.setPRs([]gh.PR{{Number: 1, Title: "alpha-title", State: "OPEN"}})
	if !strings.Contains(strings.Join(m.rowText, "\n"), "alpha-title") {
		t.Fatal("first render missing alpha-title")
	}

	m.setPRs([]gh.PR{{Number: 2, Title: "bravo-title", State: "OPEN"}})
	joined := strings.Join(m.rowText, "\n")
	if strings.Contains(joined, "alpha-title") {
		t.Error("stale cached row survived a content change")
	}
	if !strings.Contains(joined, "bravo-title") {
		t.Error("new content not rendered after content change")
	}
}

// TestRowCacheKeepsAllRowsOnCursorMove: a cursor move reuses unchanged rows but
// still emits every row (only the focus flips).
func TestRowCacheKeepsAllRowsOnCursorMove(t *testing.T) {
	m := rowCacheModel(t)
	m.setPRs([]gh.PR{
		{Number: 1, Title: "row-one", State: "OPEN"},
		{Number: 2, Title: "row-two", State: "OPEN"},
		{Number: 3, Title: "row-three", State: "OPEN"},
	})
	m.moveCursor(1)
	joined := strings.Join(m.rowText, "\n")
	for _, want := range []string{"row-one", "row-two", "row-three"} {
		if !strings.Contains(joined, want) {
			t.Errorf("row %q missing after cursor move", want)
		}
	}
}
