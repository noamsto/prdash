# Group-Scoped `V` Selection Cycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `V` cycle **Group → All → None** instead of selecting every shown row.

**Architecture:** Derive the next step from the current selection — no stored mode. A small `groupRange` helper finds the cursor's contiguous group via `PRSection.groupLabel` when `grouped`; otherwise the whole board is one group. `case "V"` selects the group, then the board, then clears. Legend/README hints rename `all` → `group`.

**Tech Stack:** Go, Bubble Tea key handling, plain `go test ./internal/ui/`.

**Spec:** `docs/superpowers/specs/2026-08-03-board-prefetch-select-optimistic-design.md` (Feature 2)

## Global Constraints

- No `selMode` / cycle counter on `Model` — selection state alone drives the next step.
- Group step is **additive** (does not clear other groups).
- Issue board and flat PR boards: group == entire shown set → All → None.
- Do not change `space` toggle behavior.
- Run: `go test ./internal/ui/ -run 'SelectCycle|GroupRange|V' -count=1` then `go test ./internal/ui/ -count=1`

---

### Task 1: `groupRange` + `advanceSelection` helpers

**Files:**
- Modify: `internal/ui/prlist.go` (add helpers near `moveCursor`)
- Modify: `internal/ui/select.go` (optional — only if a `selectIndex` helper is cleaner than toggle-if-missing; prefer keeping select.go untouched and using the existing `!has → toggle` pattern)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `m.section`, `m.cursor`, `m.sel`, `(*PRSection).grouped`, `(*PRSection).groupLabel(i)`
- Produces:
  - `func (m Model) groupRange() (lo, hi int)`
  - `func (m *Model) advanceSelection()` — mutates `m.sel` per the cycle

- [ ] **Step 1: Write failing tests**

Append to `internal/ui/prlist_test.go`:

```go
func TestGroupRangeCategorized(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}},                         // Review requested
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("me")}, {Number: 3, Author: author("x")}},
		"me",
	)
	// After setSections: Review requested (#1), Mine (#2), Others (#3) — one each.
	m.cursor = 1 // Mine
	lo, hi := m.groupRange()
	if lo != 1 || hi != 1 {
		t.Fatalf("Mine groupRange = [%d,%d], want [1,1]", lo, hi)
	}
	m.cursor = 0
	lo, hi = m.groupRange()
	if lo != 0 || hi != 0 {
		t.Fatalf("Review groupRange = [%d,%d], want [0,0]", lo, hi)
	}
}

func TestGroupRangeAuthorGrouped(t *testing.T) {
	m := NewModel("/tmp", "author:x", nil) // non-sections → author grouping when ≥2 authors
	m.SetRepo("o/r")
	ps := NewPRSection("author:x")
	ps.SetForceGroup(true)
	ps.SetPRs([]gh.PR{
		{Number: 1, Author: author("alice"), UpdatedAt: time.Now()},
		{Number: 2, Author: author("alice"), UpdatedAt: time.Now().Add(-time.Hour)},
		{Number: 3, Author: author("bob"), UpdatedAt: time.Now()},
	})
	m.section = ps
	// ForceGroup + 2 authors → grouped by author. Find alice's span.
	var aliceLo = -1
	for i := 0; i < ps.Len(); i++ {
		if ps.groupLabel(i) == "alice" {
			if aliceLo < 0 {
				aliceLo = i
			}
			m.cursor = i
		}
	}
	if aliceLo < 0 {
		t.Fatal("alice group missing")
	}
	lo, hi := m.groupRange()
	if lo != aliceLo || hi != aliceLo+1 {
		t.Fatalf("alice groupRange = [%d,%d], want [%d,%d]", lo, hi, aliceLo, aliceLo+1)
	}
}

func TestGroupRangeFlatIsWholeBoard(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Author: author("only")}, {Number: 2, Author: author("only")}})
	m.cursor = 1
	lo, hi := m.groupRange()
	if lo != 0 || hi != 1 {
		t.Fatalf("flat groupRange = [%d,%d], want [0,1]", lo, hi)
	}
}

func TestAdvanceSelectionCycle(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}},
		nil,
		[]gh.PR{
			{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")},
			{Number: 3, Author: author("me")},
			{Number: 4, Author: author("x")},
		},
		"me",
	)
	// Review requested = #1,#2; Mine = #3; Others = #4
	m.cursor = 0 // in Review requested
	m.advanceSelection() // Group
	if m.sel.count() != 2 || !m.sel.has(0) || !m.sel.has(1) {
		t.Fatalf("after Group: sel=%v, want indexes 0,1", m.sel.indices())
	}
	m.advanceSelection() // All
	if m.sel.count() != 4 {
		t.Fatalf("after All: count=%d, want 4", m.sel.count())
	}
	m.advanceSelection() // None
	if m.sel.count() != 0 {
		t.Fatalf("after None: count=%d, want 0", m.sel.count())
	}
}

func TestAdvanceSelectionFillsPartialGroup(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}, {Number: 3, Author: author("a")}},
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("a")}, {Number: 3, Author: author("a")}, {Number: 4, Author: author("x")}},
		"me",
	)
	m.cursor = 0
	m.sel.toggle(1) // partial group
	m.advanceSelection()
	if !m.sel.has(0) || !m.sel.has(1) || !m.sel.has(2) || m.sel.has(3) {
		t.Fatalf("partial group should fill group only, sel=%v", m.sel.indices())
	}
}

func TestAdvanceSelectionFlatAllThenNone(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, Author: author("only")}, {Number: 2, Author: author("only")}})
	m.advanceSelection()
	if m.sel.count() != 2 {
		t.Fatalf("flat first V should select all, got %d", m.sel.count())
	}
	m.advanceSelection()
	if m.sel.count() != 0 {
		t.Fatalf("flat second V should clear, got %d", m.sel.count())
	}
}
```

Notes for the implementer:
- Import `time` in the test file if `TestGroupRangeAuthorGrouped` needs it and it is not already imported.
- `author` helper and `setSections` already exist in this test package (see commented-by-me tests). Match their signatures exactly — if `setSections` argument order is `(review, reviewed, open, viewer)`, keep that order.
- If `SetForceGroup` + `SetPRs` does not produce a two-row alice group under current sort, assert `hi >= lo` and that every index in `[lo,hi]` has `groupLabel == "alice"`, and that `bob` is outside — do not hard-code absolute indexes beyond what the test setup guarantees.

- [ ] **Step 2: Run tests — expect fail**

Run: `go test ./internal/ui/ -run 'TestGroupRange|TestAdvanceSelection' -count=1`

Expected: FAIL (undefined `groupRange` / `advanceSelection`).

- [ ] **Step 3: Implement helpers**

In `internal/ui/prlist.go`, near `moveCursor`:

```go
// groupRange returns the inclusive [lo, hi] shown-index span of the cursor's
// group. When the board is ungrouped (or not a PR section), the whole shown
// set is one group.
func (m Model) groupRange() (lo, hi int) {
	n := m.section.Len()
	if n == 0 {
		return 0, -1
	}
	ps, ok := m.section.(*PRSection)
	if !ok || !ps.grouped {
		return 0, n - 1
	}
	cur := m.cursor
	if cur < 0 {
		cur = 0
	}
	if cur >= n {
		cur = n - 1
	}
	label := ps.groupLabel(cur)
	lo, hi = cur, cur
	for lo > 0 && ps.groupLabel(lo-1) == label {
		lo--
	}
	for hi+1 < n && ps.groupLabel(hi+1) == label {
		hi++
	}
	return lo, hi
}

// advanceSelection cycles multi-select: Group → All → None, derived from the
// current selection (no stored mode).
func (m *Model) advanceSelection() {
	n := m.section.Len()
	if n == 0 {
		return
	}
	lo, hi := m.groupRange()
	groupFull := true
	for i := lo; i <= hi; i++ {
		if !m.sel.has(i) {
			groupFull = false
			break
		}
	}
	if !groupFull {
		for i := lo; i <= hi; i++ {
			if !m.sel.has(i) {
				m.sel.toggle(i)
			}
		}
		return
	}
	allFull := m.sel.count() == n
	if !allFull {
		for i := 0; i < n; i++ {
			if !m.sel.has(i) {
				m.sel.toggle(i)
			}
		}
		return
	}
	m.sel.clear()
}
```

- [ ] **Step 4: Run tests — expect pass**

Run: `go test ./internal/ui/ -run 'TestGroupRange|TestAdvanceSelection' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "$(cat <<'EOF'
feat(ui): derive V selection cycle helpers (group → all → none)

EOF
)"
```

---

### Task 2: Wire `V` + legend / README

**Files:**
- Modify: `internal/ui/prlist.go` (`case "V"`, legend hint `"all"` → `"group"`, `navHintsFor`)
- Modify: `README.md` (`V` select all → group cycle wording)
- Test: drive `Update(keyMsg("V"))` in `internal/ui/prlist_test.go` (optional thin wrapper over Task 1 — one integration test)

**Interfaces:**
- Consumes: `(*Model).advanceSelection()` from Task 1
- Produces: user-facing `V` behavior + hint text

- [ ] **Step 1: Write the key-path test**

```go
func TestVKeyAdvancesSelection(t *testing.T) {
	m := NewModel("/tmp", "is:open", nil)
	m.width, m.height = 120, 40
	m.setSections(
		[]gh.PR{{Number: 1, Author: author("a")}},
		nil,
		[]gh.PR{{Number: 1, Author: author("a")}, {Number: 2, Author: author("x")}},
		"me",
	)
	m.cursor = 0
	u, _ := m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 1 || !m.sel.has(0) {
		t.Fatalf("V on Review group: sel=%v, want {0}", m.sel.indices())
	}
	u, _ = m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 2 {
		t.Fatalf("second V should select all, got %d", m.sel.count())
	}
	u, _ = m.Update(keyMsg("V"))
	m = u.(Model)
	if m.sel.count() != 0 {
		t.Fatalf("third V should clear, got %d", m.sel.count())
	}
}
```

(`keyMsg` already exists in this package.)

- [ ] **Step 2: Run — expect fail or still-old behavior**

Run: `go test ./internal/ui/ -run TestVKeyAdvancesSelection -count=1`

Expected: FAIL — current `V` selects all on first press (`count == 2`).

- [ ] **Step 3: Wire the key and hints**

Replace `case "V":` in `prlist.go`:

```go
		case "V":
			m.advanceSelection()
			m.renderList()
			return m, nil
```

Replace both hint occurrences of `keyHint{"V", "all"}` with `keyHint{"V", "group"}` (legend nav builder ~line 2264 and `navHintsFor` ~line 2359).

In `README.md`, change:

```markdown
| `/` | fuzzy find · `space` select · `V` select all |
```

to:

```markdown
| `/` | fuzzy find · `space` select · `V` select group → all → none |
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestVKey|TestAdvanceSelection|TestGroupRange' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go README.md
git commit -m "$(cat <<'EOF'
feat(ui): V cycles select group → all → none

EOF
)"
```
