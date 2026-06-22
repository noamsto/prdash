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

type Entry struct {
	Rows    json.RawMessage `json:"rows"`
	SavedAt time.Time       `json:"savedAt"`
}

type Cache struct {
	mu       sync.Mutex
	entries  map[string]Entry
	filePath string
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

func (c *Cache) Get(key string) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return e, ok
}

func (c *Cache) Set(key string, rows json.RawMessage) {
	c.mu.Lock()
	c.entries[key] = Entry{Rows: rows, SavedAt: time.Now()}
	c.mu.Unlock()
	if err := c.save(); err != nil {
		slog.Debug("cache save failed", "err", err)
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

// save holds the full lock across marshal+rename so concurrent saves can't
// clobber each other via rename ordering.
func (c *Cache) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
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
