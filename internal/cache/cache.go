package cache

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxAge = 7 * 24 * time.Hour

// writeDebounce coalesces a burst of Set calls (e.g. the ~7 writes a refresh
// settle triggers) into a single disk write once writes go quiet.
const writeDebounce = 400 * time.Millisecond

type Entry struct {
	Rows    json.RawMessage `json:"rows"`
	SavedAt time.Time       `json:"savedAt"`
	// Stale marks an entry a local mutation has invalidated: the rows on disk
	// may no longer reflect reality, even though SavedAt is recent. Fresh and
	// Get both treat it as a miss, so a mutation invalidated mid-flight (quit
	// before the reconcile fetch lands) can't paint or be trusted next launch.
	Stale bool `json:"stale,omitempty"`
}

type Cache struct {
	mu       sync.Mutex
	entries  map[string]Entry
	filePath string
	dirty    bool        // unwritten changes pending a flush
	timer    *time.Timer // debounce timer; fires flush after writes go quiet
}

// Key composes a cache key. schemaVer makes a changed field set a clean miss.
func Key(kind, filter string, limit int, schemaVer string) string {
	return fmt.Sprintf("%s:%s\x00%d\x00%s", kind, filter, limit, schemaVer)
}

func Open(filePath string) *Cache {
	c := &Cache{entries: map[string]Entry{}, filePath: filePath}
	if err := c.load(); err != nil {
		slog.Debug("cache load failed, starting empty", "err", err)
	}
	return c
}

// Get returns the entry for key, treating a Stale one as a miss so a caller
// painting from cache never repaints a mutation-invalidated snapshot.
func (c *Cache) Get(key string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.Stale {
		return Entry{}, false
	}
	return e, true
}

// Fresh reports whether key has an entry saved within the last ttl and not
// marked Stale. Callers use it to skip a live reconcile fetch when the cached
// data is recent enough.
func (c *Cache) Fresh(key string, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.Stale {
		return false
	}
	return time.Since(e.SavedAt) < ttl
}

// Invalidate marks the given keys Stale, arming the same debounce Set uses so
// Flush persists it on exit. Call it when a mutation is dispatched — not on
// success — so a quit while the mutation is still in flight still leaves the
// pre-mutation snapshot unpainted and untrusted at next launch. A key with no
// entry yet is already a miss and needs no mark.
func (c *Cache) Invalidate(keys ...string) {
	c.mu.Lock()
	changed := false
	for _, key := range keys {
		e, ok := c.entries[key]
		if !ok || e.Stale {
			continue
		}
		e.Stale = true
		c.entries[key] = e
		changed = true
	}
	if changed {
		c.dirty = true
		if c.timer == nil {
			c.timer = time.AfterFunc(writeDebounce, c.flush)
		} else {
			c.timer.Reset(writeDebounce)
		}
	}
	c.mu.Unlock()
}

// Set updates the entry in memory immediately (so reads are current) and
// schedules a debounced disk write off the caller's goroutine — the UI update
// loop no longer blocks on a full-file marshal per call.
func (c *Cache) Set(key string, rows json.RawMessage) {
	c.mu.Lock()
	c.entries[key] = Entry{Rows: rows, SavedAt: time.Now()}
	c.dirty = true
	if c.timer == nil {
		c.timer = time.AfterFunc(writeDebounce, c.flush)
	} else {
		c.timer.Reset(writeDebounce)
	}
	c.mu.Unlock()
}

// Flush writes any pending changes synchronously. Call it on shutdown so a
// quit right after a fetch still persists the cache.
func (c *Cache) Flush() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	c.flush()
}

// flush writes the entries to disk if dirty. Marshals under the lock (a fast,
// consistent snapshot) then writes outside it, so a Set concurrent with the
// write isn't blocked on I/O.
func (c *Cache) flush() {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	b, err := json.Marshal(c.entries)
	c.dirty = false
	c.mu.Unlock()
	if err != nil {
		slog.Debug("cache marshal failed", "err", err)
		return
	}
	if err := c.writeFile(b); err != nil {
		slog.Debug("cache save failed", "err", err)
		c.mu.Lock()
		c.dirty = true // let the next Set/Flush retry
		c.mu.Unlock()
	}
}

func (c *Cache) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := os.ReadFile(c.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var entries map[string]Entry
	if err := json.Unmarshal(b, &entries); err != nil {
		return err
	}
	c.entries = entries
	c.prune()
	return nil
}

// prune drops entries older than maxAge. Caller holds the lock.
func (c *Cache) prune() {
	cutoff := time.Now().Add(-maxAge)
	for k, e := range c.entries {
		if e.SavedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}

// writeFile atomically writes b to filePath via a temp file + rename. The
// debounce timer serializes flushes, so no cross-write locking is needed here.
func (c *Cache) writeFile(b []byte) error {
	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, c.filePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
