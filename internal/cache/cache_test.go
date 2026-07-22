package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	return &Cache{
		entries:  map[string]Entry{},
		filePath: filepath.Join(t.TempDir(), "results-cache.json"),
	}
}

func TestRoundTrip(t *testing.T) {
	c := newTestCache(t)
	rows := json.RawMessage(`[{"number":1}]`)
	c.Set("pr:is:open\x0020\x00v1", rows)
	got, ok := c.Get("pr:is:open\x0020\x00v1")
	if !ok || string(got.Rows) != string(rows) {
		t.Fatalf("got=%q ok=%v", got.Rows, ok)
	}
}

func TestPersistsAcrossLoad(t *testing.T) {
	c := newTestCache(t)
	c.Set("k", json.RawMessage(`[]`))
	c.Flush() // writes are debounced; Flush persists synchronously (as on shutdown)
	reloaded := &Cache{entries: map[string]Entry{}, filePath: c.filePath}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := reloaded.Get("k"); !ok {
		t.Fatal("expected hit after reload")
	}
}

// TestSetCoalescesWrites: a burst of Sets writes at most once until Flush, and
// Flush persists the final state.
func TestSetCoalescesWrites(t *testing.T) {
	c := newTestCache(t)
	for range 5 {
		c.Set("k", json.RawMessage(`[]`))
	}
	// Before the debounce fires, nothing is on disk yet.
	if _, err := os.Stat(c.filePath); !os.IsNotExist(err) {
		t.Fatalf("expected no file before flush, stat err=%v", err)
	}
	c.Flush()
	if _, err := os.Stat(c.filePath); err != nil {
		t.Fatalf("expected file after flush: %v", err)
	}
}

func TestPrunesOld(t *testing.T) {
	c := newTestCache(t)
	c.entries["fresh"] = Entry{SavedAt: time.Now()}
	c.entries["stale"] = Entry{SavedAt: time.Now().Add(-maxAge - time.Hour)}
	c.prune()
	if _, ok := c.entries["stale"]; ok {
		t.Error("stale should be pruned")
	}
	if _, ok := c.entries["fresh"]; !ok {
		t.Error("fresh should survive")
	}
}

func TestFresh(t *testing.T) {
	c := newTestCache(t)
	c.entries["recent"] = Entry{SavedAt: time.Now().Add(-10 * time.Second)}
	c.entries["old"] = Entry{SavedAt: time.Now().Add(-10 * time.Minute)}
	if !c.Fresh("recent", time.Minute) {
		t.Error("entry within ttl should be fresh")
	}
	if c.Fresh("old", time.Minute) {
		t.Error("entry older than ttl should be stale")
	}
	if c.Fresh("missing", time.Minute) {
		t.Error("missing key should be stale")
	}
}

func TestKey(t *testing.T) {
	k := Key("pr", "is:open author:@me", 20, "abc123")
	want := "pr:is:open author:@me\x0020\x00abc123"
	if k != want {
		t.Errorf("Key = %q, want %q", k, want)
	}
}
