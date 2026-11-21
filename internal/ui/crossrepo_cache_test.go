package ui

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/noamsto/prdash/internal/cache"
	"github.com/noamsto/prdash/internal/gh"
)

// fakeCrossSource records every cross-repo search so a test can assert the
// board issues exactly one, with the shared filter and limit.
type fakeCrossSource struct {
	mu      sync.Mutex
	calls   int
	filters []string
	limits  []int
	prs     []gh.PR
	err     error
}

func (f *fakeCrossSource) FetchCrossRepoPRs(filter string, limit int) ([]gh.PR, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.filters = append(f.filters, filter)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return nil, nil, f.err
	}
	raw, _ := json.Marshal(f.prs)
	return f.prs, raw, nil
}

func (f *fakeCrossSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCrossRepoHydratesFromDiskCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	review, open, cross := repoGroupFixture()

	c := cache.Open(path)
	seed := NewModel("/repo", "is:open author:@me", c)
	seed.SetRepo(launchRepo)
	c.Set(prKey(seed.repo, searchFor("pr", seed.state, reviewBody), defaultLimit), mustJSON(t, review))
	c.Set(prKey(seed.repo, searchFor("pr", seed.state, reviewedBody), defaultLimit), json.RawMessage("[]"))
	c.Set(prKey(seed.repo, "is:open", openListLimit), mustJSON(t, open))
	c.Set(prKey(seed.repo, crossRepoFilter, crossRepoLimit), mustJSON(t, cross))
	c.Set(viewerKey(), json.RawMessage(`"me"`))
	c.Flush()

	m := NewModel("/repo", "is:open author:@me", cache.Open(path))
	m.SetRepo(launchRepo)
	m.width, m.height = 100, 40
	fake := &fakeCrossSource{}
	m.SetCrossRepoSource(fake)
	cs := countLaunchSources(&m)
	m.Hydrate()

	want := []string{"owner/repo#01", "owner/repo#02", "owner/repo#03", "c/d#09", "c/d#08", "a/b#05"}
	if got := boardLayout(m); !slices.Equal(got, want) {
		t.Fatalf("hydrated layout = %v, want %v", got, want)
	}
	if n := cs.calls.Load() + int64(fake.callCount()); n != 0 {
		t.Fatalf("hydrate made %d source calls, want 0", n)
	}
}

func TestLaunchGateIncludesCrossKey(t *testing.T) {
	m, c := launchModel(t)
	m.SetCrossRepoSource(&fakeCrossSource{})
	crossKey := prKey(m.repo, crossRepoFilter, crossRepoLimit)
	if !slices.Contains(m.launchGateKeys(), crossKey) {
		t.Fatalf("launchGateKeys %v lack the cross key %q", m.launchGateKeys(), crossKey)
	}

	t.Run("all four fresh skips the fetch", func(t *testing.T) {
		warmLaunchCache(m, c)
		c.Set(crossKey, json.RawMessage("[]"))
		cmds := m.launchFetchCmds()
		if len(cmds) == 0 {
			t.Fatal("no launch commands")
		}
		if _, ok := cmds[0]().(fetchSkippedMsg); !ok {
			t.Fatal("fresh sections and cross keys should yield fetchSkippedMsg")
		}
	})

	t.Run("stale cross key fetches", func(t *testing.T) {
		countLaunchSources(&m)
		warmLaunchCache(m, c)
		c.Set(crossKey, json.RawMessage("[]"))
		c.Invalidate(crossKey)
		cmds := m.launchFetchCmds()
		if len(cmds) == 0 {
			t.Fatal("no launch commands")
		}
		if _, ok := cmds[0]().(fetchSkippedMsg); ok {
			t.Fatal("a stale cross key must trigger the sections fetch")
		}
	})
}

func TestCrossFetchIsOneSearch(t *testing.T) {
	m, c := launchModel(t)
	m.width, m.height = 100, 40
	countLaunchSources(&m)
	_, _, cross := repoGroupFixture()
	fake := &fakeCrossSource{prs: cross}
	m.SetCrossRepoSource(fake)

	msg, ok := m.sectionsFetchCmd(true)().(sectionsFetchedMsg)
	if !ok {
		t.Fatal("sectionsFetchCmd did not return a sectionsFetchedMsg")
	}
	if fake.callCount() != 1 {
		t.Fatalf("FetchCrossRepoPRs called %d times, want exactly 1", fake.callCount())
	}
	if fake.filters[0] != crossRepoFilter || fake.limits[0] != crossRepoLimit {
		t.Fatalf("called with (%q, %d), want (%q, %d)", fake.filters[0], fake.limits[0], crossRepoFilter, crossRepoLimit)
	}
	if !msg.crossOK || len(msg.cross) != len(cross) {
		t.Fatalf("msg crossOK = %v, cross = %d rows, want true and %d", msg.crossOK, len(msg.cross), len(cross))
	}

	u, _ := m.Update(msg)
	m = u.(Model)
	if _, ok := c.Get(prKey(m.repo, crossRepoFilter, crossRepoLimit)); !ok {
		t.Error("handler did not write the cross cache key")
	}
	if got := boardLayout(m); !slices.Contains(got, "c/d#09") || !slices.Contains(got, "a/b#05") {
		t.Errorf("layout = %v, want the c/d and a/b rows", got)
	}
}

func TestCrossFetchFailureKeepsBoard(t *testing.T) {
	m, _ := launchModel(t)
	m.width, m.height = 100, 40
	review, open, cross := repoGroupFixture()

	good := sectionsMsg(t, review, open)
	good.cross, good.crossRaw, good.crossOK = cross, mustJSON(t, cross), true
	u, _ := m.Update(good)
	m = u.(Model)
	before := boardLayout(m)
	if !slices.Contains(before, "c/d#09") {
		t.Fatalf("setup: layout %v lacks the c/d rows", before)
	}

	countLaunchSources(&m)
	fake := &fakeCrossSource{err: errors.New("search down")}
	m.SetCrossRepoSource(fake)
	msg := m.sectionsFetchCmd(true)()
	sf, ok := msg.(sectionsFetchedMsg)
	if !ok {
		t.Fatalf("cross failure produced %T, want sectionsFetchedMsg (not fetchFailedMsg)", msg)
	}
	if sf.crossOK {
		t.Fatal("crossOK = true after a failed cross search")
	}

	u, _ = m.Update(sf)
	m = u.(Model)
	if m.err != nil {
		t.Fatalf("m.err = %v, want nil", m.err)
	}
	if got := boardLayout(m); !slices.Contains(got, "c/d#09") || !slices.Contains(got, "a/b#05") {
		t.Errorf("layout after failed cross fetch = %v, want the c/d and a/b rows kept", got)
	}
	if m.actionStatus == nil || !strings.Contains(m.actionStatus.fail, "Other repos: search down") {
		t.Errorf("status = %+v, want an \"Other repos: search down\" hint", m.actionStatus)
	}
}

func TestNoCrossOnNonDefaultBoard(t *testing.T) {
	m, _ := launchModel(t)
	countLaunchSources(&m)
	fake := &fakeCrossSource{}
	m.SetCrossRepoSource(fake)
	m.state = "merged"

	invokeCmdTree(t, m.sectionsFetchCmd(true))
	if n := fake.callCount(); n != 0 {
		t.Fatalf("cross search ran %d times on the merged board, want 0", n)
	}
}

// A full cross page means a cross PR missing from the next fetch may only have
// been pushed past the limit, so it must not be tagged "left filter".
func TestFullCrossPageMarksHeldPageFull(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo(launchRepo)
	m.width, m.height = 100, 40
	review, open, _ := repoGroupFixture()
	cross := make([]gh.PR, crossRepoLimit)
	for i := range cross {
		cross[i] = repoPR("a/b", i+1, "zed", "2026-01-01")
	}
	m.paintSections(review, nil, open, cross, "me", true, false)
	if !m.heldPageFull {
		t.Error("heldPageFull = false with a full cross page")
	}
}
