package ui

import (
	"encoding/json"
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

// TestInvalidateLaunchCacheMarksGateKeysAndDetail: dispatch-time invalidation
// must hit every key launchFetchCmds gates on, plus the mutated PR's detail —
// the same keys a relaunch (or the side preview) would otherwise trust.
func TestInvalidateLaunchCacheMarksGateKeysAndDetail(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c)
	c.Set(detailKey(m.repo, 61), json.RawMessage(`{"number":61}`))

	m.invalidateLaunchCache(61)

	for _, key := range m.launchGateKeys() {
		if m.cache.Fresh(key, launchFreshTTL) {
			t.Errorf("launch gate key %q still fresh after invalidateLaunchCache", key)
		}
	}
	if m.cache.Fresh(detailKey(m.repo, 61), launchFreshTTL) {
		t.Error("detail key for the mutated PR still fresh after invalidateLaunchCache")
	}
	// Untouched keys (issue/members/viewer) must survive so launch doesn't
	// refetch more than the mutation actually invalidated.
	if !m.cache.Fresh(membersKey(m.repo), launchFreshTTL) {
		t.Error("invalidateLaunchCache must not touch unrelated keys like membersKey")
	}
}

// TestHydrateWithholdsStaleEntry: a mutation-invalidated list entry must not
// paint — hydrate() has to report a miss so the caller falls back to
// Loading… instead of the pre-mutation snapshot.
func TestHydrateWithholdsStaleEntry(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c)

	if _, ok := m.cachedPRs("is:open", openListLimit); !ok {
		t.Fatal("sanity: is:open should hit before invalidation")
	}
	m.invalidateLaunchCache() // as a merge dispatch would, marking every section key stale

	if _, ok := m.cachedPRs("is:open", openListLimit); ok {
		t.Fatal("cachedPRs should miss once its entry is invalidated")
	}
	if hit := m.hydrate(); hit {
		t.Error("hydrate() painted from an invalidated entry")
	}
	if m.section.Len() != 0 {
		t.Errorf("hydrate() left %d stale rows on the board", m.section.Len())
	}
}

// TestMergeDispatchInvalidatesCacheBeforeCompletion is the issue #99 repro at
// the model level: merge a PR, then "quit" before the mutation's tea.Cmd ever
// runs (so actionDoneMsg/backgroundRefresh never fire) — next launch must not
// trust the fresh-looking pre-merge cache.
func TestMergeDispatchInvalidatesCacheBeforeCompletion(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c)
	m.SetMutationSource(&fakeMutationSource{})
	m.setPRs([]gh.PR{{Number: 61, ID: "pr61node", State: "OPEN"}})

	cmd := m.runBulk(action.DefaultPRActions()["m"]) // dispatch only — never executed, like a quit mid-flight
	if cmd == nil {
		t.Fatal("expected a dispatch command")
	}

	for _, key := range m.launchGateKeys() {
		if m.cache.Fresh(key, launchFreshTTL) {
			t.Fatalf("launch gate key %q still fresh right after merge dispatch — next launch would trust it", key)
		}
	}

	cs := countLaunchSources(&m)
	for _, fc := range m.launchFetchCmds() {
		if fc != nil {
			fc()
		}
	}
	if n := cs.calls.Load(); n == 0 {
		t.Error("relaunch after a mid-flight merge dispatch should not skip the list fetch")
	}
}

// TestHydrateSetsLoadedOnGenuinelyEmptyBoard: "Also fixes" — Hydrate() must
// propagate hydrate()'s hit like switchToFilter does, so a legitimately empty
// cached board reads "No open PRs." instead of a stuck "Loading…".
func TestHydrateSetsLoadedOnGenuinelyEmptyBoard(t *testing.T) {
	m, c := launchModel(t)
	warmLaunchCache(m, c) // sections cached, all empty ("[]")

	m.Hydrate()

	if !m.loaded {
		t.Error("Hydrate() did not propagate hydrate()'s hit onto m.loaded")
	}
}
