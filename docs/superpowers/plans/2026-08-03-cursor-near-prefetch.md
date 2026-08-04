# Cursor-Near Prefetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prefetch PR detail for rows closest to the cursor in both directions, not only downward.

**Architecture:** Rewrite the pure helper `prefetchNumbers` in `internal/ui/preview.go` to walk shown indexes by `|i - cursor|` (tie → prefer below). `warmDetailCmd` / `detailWindow` / `prefetchWindow` stay unchanged — only window membership and order change.

**Tech Stack:** Go, plain `go test ./internal/ui/`.

**Spec:** `docs/superpowers/specs/2026-08-03-board-prefetch-select-optimistic-design.md` (Feature 1)

## Global Constraints

- Do **not** change `prefetchWindow`, `warmDetailCmd`, CI poll tiers, or `detailFreshTTL`.
- Do **not** add staggered/near-far batching — one batch for the non-cursor window, as today.
- Keep the existing `TestPrefetchNumbers` behavior for the downward-from-0 case (still valid under distance order when cursor is 0).
- Run: `go test ./internal/ui/ -run Prefetch -count=1`

---

### Task 1: Distance-ordered `prefetchNumbers`

**Files:**
- Modify: `internal/ui/preview.go` (`prefetchNumbers`)
- Modify: `internal/ui/preview_test.go` (`TestPrefetchNumbers` + new cases)

**Interfaces:**
- Consumes: `(*PRSection).Len()`, `(*PRSection).prAt(i) gh.PR`, `fresh map[int]bool`
- Produces: same signature — `prefetchNumbers(ps *PRSection, cursor int, fresh map[int]bool, window int) []int`

- [ ] **Step 1: Expand the failing tests**

Replace `TestPrefetchNumbers` in `internal/ui/preview_test.go` with:

```go
func TestPrefetchNumbers(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5}})
	fresh := map[int]bool{2: true} // #2 already refreshed this session

	got := prefetchNumbers(ps, 0, fresh, 3)
	want := []int{1, 3, 4} // skips fresh #2, capped at window=3; cursor at top → only downward
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	all := map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}
	if n := prefetchNumbers(ps, 0, all, 3); n != nil {
		t.Fatalf("all fresh should yield nil, got %v", n)
	}
}

// TestPrefetchNumbersBidirectional: cursor in the middle fills both directions
// by distance, preferring below on ties.
func TestPrefetchNumbersBidirectional(t *testing.T) {
	ps := NewPRSection("is:open")
	// Numbers 1..9 at shown indexes 0..8.
	prs := make([]gh.PR, 9)
	for i := range prs {
		prs[i].Number = i + 1
	}
	ps.SetPRs(prs)

	got := prefetchNumbers(ps, 4, nil, 5) // cursor on #5 (index 4)
	// distances: 0→#5, 1→#6 then #4, 2→#7 then #3
	want := []int{5, 6, 4, 7, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPrefetchNumbersSkipsFreshAndFillsFarther(t *testing.T) {
	ps := NewPRSection("is:open")
	prs := make([]gh.PR, 7)
	for i := range prs {
		prs[i].Number = i + 1
	}
	ps.SetPRs(prs)
	// Cursor #4 (index 3). Mark nearest neighbors fresh so the window reaches farther.
	fresh := map[int]bool{4: true, 5: true, 3: true}
	got := prefetchNumbers(ps, 3, fresh, 3)
	want := []int{6, 2, 7} // dist 2 below, 2 above, 3 below
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestPrefetchNumbersAtEndGoesUp(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}})
	got := prefetchNumbers(ps, 3, nil, 3) // cursor on last row (#4)
	want := []int{4, 3, 2}                // only upward after cursor
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Run tests — expect bidirectional cases to fail**

Run: `go test ./internal/ui/ -run 'TestPrefetchNumbers' -count=1`

Expected: `TestPrefetchNumbers` may still pass (cursor 0); `TestPrefetchNumbersBidirectional` / `AtEnd` / `SkipsFresh` FAIL because the old loop only walks `i++`.

- [ ] **Step 3: Implement distance order**

Replace `prefetchNumbers` in `internal/ui/preview.go` with:

```go
// prefetchNumbers returns up to window PR numbers nearest the cursor whose
// detail hasn't been refreshed this session yet. Order is by |i - cursor|
// ascending; on a tie the row below the cursor wins (preserves the old
// downward bias for the first neighbor).
func prefetchNumbers(ps *PRSection, cursor int, fresh map[int]bool, window int) []int {
	n := ps.Len()
	if n == 0 || window <= 0 {
		return nil
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	var out []int
	// d == 0 yields only the cursor; d > 0 yields below then above.
	for d := 0; len(out) < window && d < n; d++ {
		candidates := []int{cursor + d}
		if d > 0 {
			candidates = append(candidates, cursor-d)
		}
		for _, i := range candidates {
			if i < 0 || i >= n {
				continue
			}
			num := ps.prAt(i).Number
			if fresh[num] {
				continue
			}
			out = append(out, num)
			if len(out) >= window {
				return out
			}
		}
	}
	return out
}
```

Also update the doc comment above it if the old "from cursor downward" wording remains on `detailWindow` — change `detailWindow`'s comment to "cursor-nearest" rather than "cursor-first downward":

```go
// detailWindow is the cursor-nearest set of shown PR numbers still needing detail
// (not refreshed this session, not fresh on disk), bounded by prefetchWindow.
```

- [ ] **Step 4: Run tests — expect pass**

Run: `go test ./internal/ui/ -run 'TestPrefetchNumbers' -count=1`

Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/preview.go internal/ui/preview_test.go
git commit -m "$(cat <<'EOF'
perf(ui): prefetch detail by distance from the cursor

Walk both directions from the cursor (below first on ties) so rows
above stay warm when they are nearer than rows further down.

EOF
)"
```
