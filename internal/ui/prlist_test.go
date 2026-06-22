package ui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

func TestSetPRsBuildsRows(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 7, Title: "hello", HeadRefName: "feat/x"},
		{Number: 9, Title: "world", HeadRefName: "fix/y"},
	})
	if got := len(m.table.Rows()); got != 2 {
		t.Fatalf("table rows = %d, want 2", got)
	}
	if m.table.Rows()[0][0] != "#7" {
		t.Errorf("first row number cell = %q, want #7", m.table.Rows()[0][0])
	}
}

func TestHydrateFromCache(t *testing.T) {
	c := cache.Open(filepath.Join(t.TempDir(), "c.json"))
	raw, _ := json.Marshal([]gh.PR{{Number: 42, Title: "cached"}})
	c.Set(cache.Key("pr", "is:open", 20, schemaVer), raw)

	m := NewModel("/repo", "is:open", c)
	m.hydrate()
	if len(m.prs) != 1 || m.prs[0].Number != 42 {
		t.Fatalf("hydrate did not paint cached rows: %+v", m.prs)
	}
	if len(m.table.Rows()) != 1 {
		t.Fatal("table not painted from cache")
	}
}
