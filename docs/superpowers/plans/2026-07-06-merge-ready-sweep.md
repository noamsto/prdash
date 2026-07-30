# Merge-Ready Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `S` keybind that fans out a bounded-concurrency detail fetch for every shown PR, buckets them (Ready / Behind / Other) via `triage.Compute`, and offers two bulk actions in a focused overlay — `u` to update all Behind branches, `m` to merge all Ready.

**Architecture:** The sweep runs entirely as its own overlay state on the `Model` — it never mutates or reshuffles the live board. The fan-out is a wave-based bounded-concurrency loop expressed through the existing Bubble Tea message loop: `startSweep` dispatches the first N fetches, and each completion (`sweepDetailMsg`) dispatches exactly one more, so at most N `gh pr view` subprocesses run at once. Buckets are computed on demand from the already-shared detail cache (`m.detail`) using `triage.Compute`. The two bulk actions translate a bucket's PR numbers into board selection indices and hand off to the existing `startBulk`/`runBulk` plumbing (including the confirm modal for merge).

**Tech Stack:** Go, charmbracelet Bubble Tea v2 (`charm.land/bubbletea/v2`), lipgloss v2. `gh` CLI is the only OS seam, reached through `gh.Runner` (faked in tests).

## Global Constraints

- **Never reshuffle the live board from detail.** The sweep computes buckets in its own overlay state; it must not call `setPRs`, `applyFilter`, or reorder `shown`. (Board deliberately ignores merge-state — see `internal/ui/section.go:137` `prRank` comment.)
- **Bounded concurrency on the fan-out.** At most `sweepConcurrency = 5` in-flight `gh pr view` at any moment — mirror the spirit of `prefetchWindow = 5` in `internal/ui/preview.go:168`. Do NOT `tea.Batch` all N fetches at once.
- **Async fetches go through the Elm loop** as `tea.Cmd`/`tea.Msg` — no blocking calls in `Update`.
- **Partial failure never aborts the sweep.** A failed `gh pr view` buckets that PR as Other; the sweep still completes.
- **Reuse `triage.Compute`** for classification — do not re-derive merge-state logic.
- **Reuse `startBulk`/`runBulk`** for the `u`/`m` bulk actions — do not shell out to `gh` from the sweep.
- **Lean style:** match the file's existing conventions; comments only for non-obvious WHY; no speculative robustness.

---

## File Structure

- `internal/ui/sweep.go` — **NEW.** All sweep logic: the `sweepState` and `sweepBuckets` types, the fan-out state machine (`startSweep`, `sweepDispatch`, `sweepFetchCmd`), bucket computation (`sweepBuckets()`), the bulk-action bridge (`applySweepAction`), and the overlay renderer (`sweepView`). Files that change together live together — the sweep is one cohesive responsibility.
- `internal/ui/sweep_test.go` — **NEW.** Table-driven tests for bucketing, the bounded-concurrency dispatch bound, partial failure, key routing, and the bulk-action bridge, using the existing `recordRunner` fake.
- `internal/ui/messages.go` — **MODIFY.** Add the `sweepDetailMsg` type (the message home for the package).
- `internal/ui/prlist.go` — **MODIFY.** Add the `sweep sweepState` field to `Model`; extract a `storeDetail` helper and reuse it in the `prDetailMsg` case; add the `sweepDetailMsg` case and the sweep overlay guard to `Update`; add the `case "S"` starter to the board key switch; add the sweep case to `render()`; add an `S` entry to `legendView` for discoverability.
- `internal/ui/section.go` — **MODIFY.** Add `(*PRSection).indexOfNumber` to map a PR number back to its shown-row index.

---

### Task 1: Sweep state, bucket computation, and index lookup

Pure, synchronous logic with no message loop — the testable foundation the fan-out and overlay build on. Also does the small `storeDetail` extraction the fan-out will reuse.

**Files:**
- Create: `internal/ui/sweep.go`
- Create: `internal/ui/sweep_test.go`
- Modify: `internal/ui/section.go` (add `indexOfNumber` after `prAt`, ~line 81)
- Modify: `internal/ui/prlist.go` (add `sweep sweepState` field to `Model` ~line 66; extract `storeDetail`, reuse in `prDetailMsg` case ~line 621)

**Interfaces:**
- Produces:
  - `type sweepState struct { active, open bool; total, done, inflight int; queue []int; errs map[int]bool }`
  - `type sweepBuckets struct { ready, behind, other []int }` (fields hold PR numbers)
  - `func (m Model) sweepBuckets() sweepBuckets`
  - `func (m Model) shownPRNumbers() []int`
  - `func (s *PRSection) indexOfNumber(num int) (int, bool)`
  - `func (m *Model) storeDetail(number int, d gh.PRDetail, raw []byte)`
- Consumes: `triage.Compute(pr gh.PR, d gh.PRDetail) triage.Card`, `triage.KindReady`, `triage.KindBehind`; `m.detail map[int]gh.PRDetail`; `m.section.(*PRSection)`, `(*PRSection).prAt`, `.Len`.

- [ ] **Step 1: Write the failing test for bucketing**

Create `internal/ui/sweep_test.go`:

```go
package ui

import "testing"

import "github.com/noamsto/prdash/internal/gh"

func TestSweepBuckets(t *testing.T) {
	tests := []struct {
		name   string
		mss    string
		draft  bool
		errd   bool
		want   string // "ready" | "behind" | "other"
	}{
		{"clean is ready", "CLEAN", false, false, "ready"},
		{"has_hooks is ready", "HAS_HOOKS", false, false, "ready"},
		{"behind is behind", "BEHIND", false, false, "behind"},
		{"dirty conflict is other", "DIRTY", false, false, "other"},
		{"blocked is other", "BLOCKED", false, false, "other"},
		{"draft is other", "CLEAN", true, false, "other"},
		{"fetch error is other", "CLEAN", false, true, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel("/repo", "is:open", nil)
			m.SetRepo("x")
			m.setPRs([]gh.PR{{Number: 7, IsDraft: tt.draft}})
			m.sweep = sweepState{open: true, errs: map[int]bool{}}
			if tt.errd {
				m.sweep.errs[7] = true
			} else {
				m.detail[7] = gh.PRDetail{MergeStateStatus: tt.mss}
			}
			b := m.sweepBuckets()
			got := map[string]int{"ready": len(b.ready), "behind": len(b.behind), "other": len(b.other)}
			if got[tt.want] != 1 {
				t.Fatalf("PR #7 (%s) not bucketed as %s: %+v", tt.name, tt.want, b)
			}
		})
	}
}

func TestIndexOfNumber(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{{Number: 10}, {Number: 20}, {Number: 30}})
	i, ok := ps.indexOfNumber(20)
	if !ok || ps.prAt(i).Number != 20 {
		t.Fatalf("indexOfNumber(20) = %d, %v; want the row whose Number is 20", i, ok)
	}
	if _, ok := ps.indexOfNumber(999); ok {
		t.Fatal("indexOfNumber for an absent number should return false")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestSweepBuckets|TestIndexOfNumber' -v`
Expected: FAIL — `sweepState`, `sweepBuckets`, `indexOfNumber` undefined (compile error).

- [ ] **Step 3: Add `indexOfNumber` to `section.go`**

In `internal/ui/section.go`, immediately after `prAt` (~line 81):

```go
// indexOfNumber maps a PR number back to its shown-row index (for the sweep,
// which buckets by number but drives selection/bulk actions by row index).
func (s *PRSection) indexOfNumber(num int) (int, bool) {
	for i := range s.shown {
		if s.prs[s.shown[i]].Number == num {
			return i, true
		}
	}
	return -1, false
}
```

- [ ] **Step 4: Add the `sweep` field to `Model`**

In `internal/ui/prlist.go`, inside the `Model` struct (after `pendingExec`, ~line 66):

```go
	sweep sweepState // on-demand merge-ready sweep: fan-out progress + result buckets
```

- [ ] **Step 5: Extract `storeDetail` and reuse it in the `prDetailMsg` case**

In `internal/ui/prlist.go`, replace the body of the `case prDetailMsg:` (~lines 621-628):

```go
	case prDetailMsg:
		m.storeDetail(msg.number, msg.detail, msg.raw)
		m.renderList()
		return m, nil
```

Add the helper near the other detail helpers (e.g. just below the `prDetailMsg` type/`fetchDetailCmd` in `preview.go`, or at the top of the new `sweep.go`). Put it in `sweep.go` so the new file is self-contained:

```go
// storeDetail records a fetched PR detail into the shared cache the quick view
// and the sweep both read, mirroring the prDetailMsg bookkeeping.
func (m *Model) storeDetail(number int, d gh.PRDetail, raw []byte) {
	m.detail[number] = d
	m.fresh[number] = true
	if m.cache != nil && raw != nil {
		m.cache.Set(detailKey(m.repo, number), raw)
	}
}
```

- [ ] **Step 6: Create `sweep.go` with the state and bucket types**

Create `internal/ui/sweep.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

// sweepConcurrency bounds how many gh pr view subprocesses the fan-out runs at
// once, matching the spirit of preview.go's prefetchWindow.
const sweepConcurrency = 5

// sweepState holds the on-demand merge-ready sweep: the fan-out progress while
// active, then the result buckets while open. It lives in its own overlay and
// never touches the live board.
type sweepState struct {
	active   bool         // fetch fan-out in flight
	open     bool         // fetch done; the result overlay is showing
	total    int          // PRs in this sweep
	done     int          // fetches completed (success or failure)
	inflight int          // fetches currently running (bounded by sweepConcurrency)
	queue    []int        // PR numbers not yet dispatched
	errs     map[int]bool // PR numbers whose detail fetch failed
}

// sweepBuckets is the sweep's classification of shown PRs by merge-readiness.
type sweepBuckets struct {
	ready  []int // KindReady — one-click merge (CLEAN/HAS_HOOKS, not draft)
	behind []int // KindBehind — one-click update-branch
	other  []int // conflict/blocked/failing/running/draft/errored — not actionable here
}

// shownPRNumbers lists every PR number currently on the board, in shown order.
func (m Model) shownPRNumbers() []int {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return nil
	}
	out := make([]int, 0, ps.Len())
	for i := 0; i < ps.Len(); i++ {
		out = append(out, ps.prAt(i).Number)
	}
	return out
}

// sweepBuckets classifies each shown PR from cached detail via triage.Compute.
// Errored fetches and PRs with no cached detail fall into Other.
func (m Model) sweepBuckets() sweepBuckets {
	var b sweepBuckets
	ps, ok := m.section.(*PRSection)
	if !ok {
		return b
	}
	for i := 0; i < ps.Len(); i++ {
		pr := ps.prAt(i)
		num := pr.Number
		d, cached := m.detail[num]
		switch {
		case m.sweep.errs[num] || !cached:
			b.other = append(b.other, num)
		default:
			switch triage.Compute(pr, d).Kind {
			case triage.KindReady:
				b.ready = append(b.ready, num)
			case triage.KindBehind:
				b.behind = append(b.behind, num)
			default:
				b.other = append(b.other, num)
			}
		}
	}
	return b
}
```

(The `fmt`/`strings`/`lipgloss`/`gh` imports are used by later tasks in this same file; if the linter complains between tasks, add each import in the task that first uses it. `storeDetail` from Step 5 uses `gh`.)

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestSweepBuckets|TestIndexOfNumber' -v`
Expected: PASS (all subtests).

- [ ] **Step 8: Run the full package to confirm no regression from the refactor**

Run: `go test ./internal/ui/`
Expected: PASS (the `prDetailMsg` refactor is behavior-preserving).

- [ ] **Step 9: Commit**

```bash
git add internal/ui/sweep.go internal/ui/sweep_test.go internal/ui/section.go internal/ui/prlist.go
git commit -m "feat(ui): sweep state, bucketing, and number→index lookup"
```

---

### Task 2: Bounded-concurrency fan-out fetch

The wave-based state machine: `startSweep` seeds the first `sweepConcurrency` fetches; each `sweepDetailMsg` completion dispatches exactly one more, storing detail on success and recording the number on failure, until every PR is accounted for.

**Files:**
- Modify: `internal/ui/messages.go` (add `sweepDetailMsg`)
- Modify: `internal/ui/sweep.go` (add `sweepFetchCmd`, `sweepDispatch`, `startSweep`)
- Modify: `internal/ui/prlist.go` (add the `sweepDetailMsg` case to `Update`, near the `prDetailMsg` case ~line 628)
- Modify: `internal/ui/sweep_test.go` (add fan-out tests)

**Interfaces:**
- Produces:
  - `type sweepDetailMsg struct { number int; detail gh.PRDetail; raw []byte; err error }`
  - `func (m Model) sweepFetchCmd(number int) tea.Cmd`
  - `func (m *Model) sweepDispatch(n int) tea.Cmd`
  - `func (m *Model) startSweep() tea.Cmd`
- Consumes: `gh.PRViewArgs`, `gh.ParsePRDetail`, `m.runner`, `m.dir`, `storeDetail` (Task 1), `m.shownPRNumbers` (Task 1), `tea.Batch`.

- [ ] **Step 1: Write the failing tests for the fan-out state machine**

Add to `internal/ui/sweep_test.go`:

```go
import tea "charm.land/bubbletea/v2"

func TestStartSweepBoundsConcurrency(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(&recordRunner{})
	prs := make([]gh.PR, 10)
	for i := range prs {
		prs[i] = gh.PR{Number: i + 1}
	}
	m.setPRs(prs)

	cmd := m.startSweep()
	if cmd == nil {
		t.Fatal("startSweep should return the initial dispatch command")
	}
	if !m.sweep.active || m.sweep.total != 10 {
		t.Fatalf("sweep not started: active=%v total=%d", m.sweep.active, m.sweep.total)
	}
	if m.sweep.inflight != sweepConcurrency {
		t.Fatalf("initial inflight = %d, want the bound %d", m.sweep.inflight, sweepConcurrency)
	}
	if len(m.sweep.queue) != 10-sweepConcurrency {
		t.Fatalf("queue = %d, want %d still pending", len(m.sweep.queue), 10-sweepConcurrency)
	}
}

func TestSweepDrainsToOpen(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(&recordRunner{})
	prs := make([]gh.PR, 10)
	for i := range prs {
		prs[i] = gh.PR{Number: i + 1}
	}
	m.setPRs(prs)
	m.startSweep()

	var model tea.Model = m
	for i := 1; i <= 10; i++ {
		var mm Model
		var ok bool
		// numbers 1..5 fail (partial failure), 6..10 succeed
		msg := sweepDetailMsg{number: i}
		if i <= 5 {
			msg.err = fmt.Errorf("boom")
		} else {
			msg.detail = gh.PRDetail{MergeStateStatus: "CLEAN"}
		}
		model, _ = model.Update(msg)
		if mm, ok = model.(Model); !ok {
			t.Fatal("Update did not return a Model")
		}
		// inflight must never exceed the bound during the drain
		if mm.sweep.inflight > sweepConcurrency {
			t.Fatalf("after msg %d inflight=%d exceeds bound", i, mm.sweep.inflight)
		}
	}
	m = model.(Model)
	if m.sweep.active || !m.sweep.open {
		t.Fatalf("sweep should be done+open: active=%v open=%v", m.sweep.active, m.sweep.open)
	}
	if m.sweep.done != 10 {
		t.Fatalf("done = %d, want 10", m.sweep.done)
	}
	b := m.sweepBuckets()
	if len(b.other) != 5 {
		t.Fatalf("5 failed fetches should bucket as Other, got %d: %+v", len(b.other), b)
	}
	if len(b.ready) != 5 {
		t.Fatalf("5 CLEAN PRs should bucket as Ready, got %d: %+v", len(b.ready), b)
	}
}
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestStartSweep|TestSweepDrains' -v`
Expected: FAIL — `startSweep`, `sweepDetailMsg` undefined (compile error).

- [ ] **Step 3: Add the `sweepDetailMsg` type**

In `internal/ui/messages.go`, after `detailDebounceMsg` (~line 25):

```go
// sweepDetailMsg carries one fan-out fetch result for the merge-ready sweep. It
// carries number on both success and failure so a failed fetch can be bucketed
// as Other (unlike fetchFailedMsg, which drops the number).
type sweepDetailMsg struct {
	number int
	detail gh.PRDetail
	raw    []byte
	err    error
}
```

- [ ] **Step 4: Add the fan-out commands to `sweep.go`**

Add `tea "charm.land/bubbletea/v2"` to `sweep.go`'s imports, then:

```go
// sweepFetchCmd fetches one PR's detail for the sweep, tagging the result with
// the number so a failure can still be attributed to its PR.
func (m Model) sweepFetchCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		raw, err := r.Run(dir, gh.PRViewArgs(number)...)
		if err != nil {
			return sweepDetailMsg{number: number, err: err}
		}
		d, err := gh.ParsePRDetail(raw)
		if err != nil {
			return sweepDetailMsg{number: number, err: err}
		}
		return sweepDetailMsg{number: number, detail: d, raw: raw}
	}
}

// sweepDispatch pops up to n queued PR numbers and returns their fetch commands,
// bumping inflight so concurrency stays bounded by sweepConcurrency.
func (m *Model) sweepDispatch(n int) tea.Cmd {
	var cmds []tea.Cmd
	for len(cmds) < n && len(m.sweep.queue) > 0 {
		num := m.sweep.queue[0]
		m.sweep.queue = m.sweep.queue[1:]
		m.sweep.inflight++
		cmds = append(cmds, m.sweepFetchCmd(num))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// startSweep seeds the sweep over every shown PR and kicks off the first wave.
func (m *Model) startSweep() tea.Cmd {
	if m.runner == nil {
		return nil
	}
	nums := m.shownPRNumbers()
	if len(nums) == 0 {
		return nil
	}
	m.sweep = sweepState{active: true, total: len(nums), queue: nums, errs: map[int]bool{}}
	return m.sweepDispatch(sweepConcurrency)
}
```

- [ ] **Step 5: Add the `sweepDetailMsg` case to `Update`**

In `internal/ui/prlist.go`, right after the `case prDetailMsg:` block (~line 628):

```go
	case sweepDetailMsg:
		if !m.sweep.active {
			return m, nil // sweep dismissed mid-flight; drop the late arrival
		}
		m.sweep.inflight--
		m.sweep.done++
		if msg.err != nil {
			m.sweep.errs[msg.number] = true
		} else {
			m.storeDetail(msg.number, msg.detail, msg.raw)
		}
		if m.sweep.done >= m.sweep.total {
			m.sweep.active = false
			m.sweep.open = true
			return m, nil
		}
		return m, m.sweepDispatch(1)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestStartSweep|TestSweepDrains' -v`
Expected: PASS.

- [ ] **Step 7: Run the full package**

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages.go internal/ui/sweep.go internal/ui/prlist.go internal/ui/sweep_test.go
git commit -m "feat(ui): bounded-concurrency merge-ready sweep fan-out"
```

---

### Task 3: Sweep overlay rendering

The overlay: a live progress line while fetching, then the bucket summary with action hints. Wired into `render()` alongside the existing overlays.

**Files:**
- Modify: `internal/ui/sweep.go` (add `sweepView`, `sweepNums` helper)
- Modify: `internal/ui/prlist.go` (add the sweep case to `render()` ~line 878)
- Modify: `internal/ui/sweep_test.go` (add a render test)

**Interfaces:**
- Produces: `func (m Model) sweepView() string`
- Consumes: `titledBox`, `overlayTop`, `m.sweepBuckets`, styles (`passStyle`, `pendStyle`, `dimStyle`, `accentStyle`, `statusBarStyle`), `lipgloss.Width`.

- [ ] **Step 1: Write the failing render test**

Add to `internal/ui/sweep_test.go`:

```go
import "strings"

func TestSweepViewShowsProgressThenBuckets(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})

	m.sweep = sweepState{active: true, total: 2, done: 1, errs: map[int]bool{}}
	if got := m.sweepView(); !strings.Contains(got, "1/2") {
		t.Fatalf("active sweep should show progress 1/2:\n%s", got)
	}

	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.detail[2] = gh.PRDetail{MergeStateStatus: "BEHIND"}
	m.sweep = sweepState{open: true, total: 2, done: 2, errs: map[int]bool{}}
	got := m.sweepView()
	if !strings.Contains(got, "Ready") || !strings.Contains(got, "Behind") {
		t.Fatalf("result overlay should list buckets:\n%s", got)
	}
	if !strings.Contains(got, "update") || !strings.Contains(got, "merge") {
		t.Fatalf("result overlay should show u/m action hints:\n%s", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -run TestSweepViewShows -v`
Expected: FAIL — `sweepView` undefined.

- [ ] **Step 3: Add `sweepView` and `sweepNums` to `sweep.go`**

```go
// sweepNums renders a compact "#12 #34" list of PR numbers.
func sweepNums(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	return strings.Join(parts, " ")
}

// sweepView renders the sweep overlay: a progress line while fetching, then the
// bucket summary with the u/m/esc action hints.
func (m Model) sweepView() string {
	if m.sweep.active {
		body := dimStyle.Render(fmt.Sprintf("Fetching merge state… %d/%d", m.sweep.done, m.sweep.total))
		return titledBox(body, 44, 3, "Sweep")
	}
	b := m.sweepBuckets()
	bucket := func(glyph, label string, nums []int, style lipgloss.Style) string {
		line := style.Render(fmt.Sprintf("%s %-7s %d", glyph, label, len(nums)))
		if len(nums) > 0 {
			line += dimStyle.Render("  " + sweepNums(nums))
		}
		return line
	}
	hint := accentStyle.Render("u") + statusBarStyle.Render(fmt.Sprintf(" update %d behind   ", len(b.behind))) +
		accentStyle.Render("m") + statusBarStyle.Render(fmt.Sprintf(" merge %d ready   ", len(b.ready))) +
		accentStyle.Render("esc") + statusBarStyle.Render(" close")
	body := strings.Join([]string{
		bucket("✓", "Ready", b.ready, passStyle),
		bucket("●", "Behind", b.behind, pendStyle),
		bucket("•", "Other", b.other, dimStyle),
		"",
		hint,
	}, "\n")
	return titledBox(body, lipgloss.Width(body)+4, 8, "Merge-ready sweep")
}
```

(`passStyle`, `pendStyle`, `dimStyle`, `accentStyle`, `statusBarStyle` are package styles from `theme.go`, used the same way in `card.go`/`prlist.go`. `lipgloss.Style` is the type of those styles; confirm with `go build` — if the styles are a different concrete type, take `func(string) string` instead.)

- [ ] **Step 4: Wire the overlay into `render()`**

In `internal/ui/prlist.go`, add a case to the overlay `switch` in `render()` (~line 878), as the first case so it takes priority:

```go
	switch {
	case m.sweep.active || m.sweep.open:
		return overlayTop(board, m.sweepView(), m.width, m.height)
	case m.pending != nil:
		return overlayTop(board, m.confirmPanel(), m.width, m.height)
```

- [ ] **Step 5: Run the render test**

Run: `go test ./internal/ui/ -run TestSweepViewShows -v`
Expected: PASS.

- [ ] **Step 6: Run the full package**

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/sweep.go internal/ui/prlist.go internal/ui/sweep_test.go
git commit -m "feat(ui): render the merge-ready sweep overlay"
```

---

### Task 4: Key routing and the bulk-action bridge

`S` starts the sweep from the board; while the overlay is up, `esc` dismisses and `u`/`m` translate a bucket into board selection and hand off to `startBulk`. Legend gains an `S` entry.

**Files:**
- Modify: `internal/ui/sweep.go` (add `applySweepAction`)
- Modify: `internal/ui/prlist.go` (sweep guard in `Update` `KeyMsg`; `case "S"` in the board switch; `S` in `legendView`)
- Modify: `internal/ui/sweep_test.go` (add routing + bridge tests)

**Interfaces:**
- Produces: `func (m *Model) applySweepAction(key string) tea.Cmd`
- Consumes: `m.actions` (the `u`/`m` entries from `action.DefaultPRActions`), `m.sel` (`clear`, `toggle`), `m.startBulk`, `(*PRSection).indexOfNumber`, `m.sweepBuckets`.

- [ ] **Step 1: Write the failing tests for routing and the bridge**

Add to `internal/ui/sweep_test.go`:

```go
func TestSweepKeyRouting(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(&recordRunner{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})

	// S starts the sweep.
	model, _ := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	m = model.(Model)
	if !m.sweep.active {
		t.Fatal("pressing S should start the sweep")
	}

	// esc dismisses it.
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = model.(Model)
	if m.sweep.active || m.sweep.open {
		t.Fatalf("esc should dismiss the sweep: %+v", m.sweep)
	}
}

func TestApplySweepActionUpdatesBehind(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	rr := &recordRunner{}
	m.SetRunner(rr)
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}, {Number: 3}})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "BEHIND"}
	m.detail[2] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.detail[3] = gh.PRDetail{MergeStateStatus: "BEHIND"}
	m.sweep = sweepState{open: true, total: 3, done: 3, errs: map[int]bool{}}

	cmd := m.applySweepAction("u") // update all Behind (#1, #3)
	if m.sweep.open || m.sweep.active {
		t.Fatal("applying an action should close the sweep overlay")
	}
	if cmd == nil {
		t.Fatal("update-behind should return a bulk command")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
	if len(rr.calls) != 2 {
		t.Fatalf("want one update-branch per Behind PR (2), got %d: %v", len(rr.calls), rr.calls)
	}
	for _, args := range rr.calls {
		if args[0] != "pr" || args[1] != "update-branch" {
			t.Fatalf("unexpected gh call: %v", args)
		}
	}
}

func TestApplySweepActionMergeConfirms(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(&recordRunner{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 1}, {Number: 2}})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"}
	m.detail[2] = gh.PRDetail{MergeStateStatus: "BEHIND"}
	m.sweep = sweepState{open: true, total: 2, done: 2, errs: map[int]bool{}}

	cmd := m.applySweepAction("m") // merge all Ready (#1) — needs confirm
	if m.pending == nil || m.pending.Key != "m" {
		t.Fatalf("merge should stage a confirm prompt, pending=%+v", m.pending)
	}
	if cmd != nil {
		t.Fatal("startBulk defers a confirm action; it should return no command yet")
	}
	if m.sel.count() != 1 {
		t.Fatalf("selection should hold the 1 Ready PR, got %d", m.sel.count())
	}
}

func TestApplySweepActionEmptyBucketIsNoop(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.setPRs([]gh.PR{{Number: 1}})
	m.detail[1] = gh.PRDetail{MergeStateStatus: "CLEAN"} // Ready, so Behind is empty
	m.sweep = sweepState{open: true, total: 1, done: 1, errs: map[int]bool{}}

	if cmd := m.applySweepAction("u"); cmd != nil {
		t.Fatal("update with no Behind PRs should be a no-op")
	}
	if m.sweep.open {
		t.Fatal("even a no-op action should close the overlay")
	}
}
```

Note: match the `tea.KeyPressMsg` construction to whatever the existing tests use for key input. If the codebase's key tests use `tea.KeyMsg`/a different constructor, mirror that exact form (check `internal/ui/*_test.go` for the established pattern before writing these two routing assertions).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestSweepKeyRouting|TestApplySweepAction' -v`
Expected: FAIL — `applySweepAction` undefined; `S` not routed.

- [ ] **Step 3: Add `applySweepAction` to `sweep.go`**

```go
// applySweepAction translates a result bucket into board selection and hands off
// to the existing bulk plumbing: "u" updates every Behind branch, "m" merges
// every Ready PR (via the confirm prompt, since the m action is Confirm). It
// closes the overlay first so the confirm modal / bulk run owns the screen.
func (m *Model) applySweepAction(key string) tea.Cmd {
	b := m.sweepBuckets()
	var nums []int
	switch key {
	case "u":
		nums = b.behind
	case "m":
		nums = b.ready
	}
	ps, ok := m.section.(*PRSection)
	if !ok || len(nums) == 0 {
		m.sweep = sweepState{}
		return nil
	}
	m.sel.clear()
	for _, num := range nums {
		if i, ok := ps.indexOfNumber(num); ok {
			m.sel.toggle(i)
		}
	}
	m.sweep = sweepState{}
	return m.startBulk(m.actions[key])
}
```

- [ ] **Step 4: Add the sweep guard to `Update`'s `KeyMsg` handling**

In `internal/ui/prlist.go`, inside `case tea.KeyMsg:`, immediately after the `if m.expanded { ... }` block (~line 676, before the `m.pending` guard):

```go
		if m.sweep.active || m.sweep.open {
			switch msg.String() {
			case "esc":
				m.sweep = sweepState{}
			case "u", "m":
				if m.sweep.open {
					return m, m.applySweepAction(msg.String())
				}
			}
			return m, nil // swallow every other key while the sweep overlay is up
		}
```

- [ ] **Step 5: Add the `case "S"` starter to the board key switch**

In the main `switch msg.String()` (~line 778, e.g. next to `case "z":`):

```go
		case "S":
			return m, m.startSweep()
```

- [ ] **Step 6: Add `S` to the legend for discoverability**

In `legendView` (~line 1034), extend the last row so users can find the keybind:

```go
		accentStyle.Render("ctrl+j/k") + statusBarStyle.Render(" scroll preview   ") + accentStyle.Render("z") + statusBarStyle.Render(" maximize   ") + accentStyle.Render("S") + statusBarStyle.Render(" sweep   ") + accentStyle.Render("esc") + statusBarStyle.Render(" close"),
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestSweepKeyRouting|TestApplySweepAction' -v`
Expected: PASS (all four).

- [ ] **Step 8: Run the full package and vet**

Run: `go test ./internal/ui/`
Expected: PASS.
Run: `go vet ./...`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/sweep.go internal/ui/prlist.go internal/ui/sweep_test.go
git commit -m "feat(ui): S key starts merge-ready sweep with u/m bulk actions"
```

---

## Assumptions & open questions

- **Assumption — fetch ALL shown PRs, including already-fresh ones.** The spec says fetch "ALL currently-shown PRs," and the whole point is catching PRs that went Behind after main moved, so cached detail may be stale. `startSweep` therefore does not skip `m.fresh` numbers (unlike `prefetchNumbers`). If refetching warm PRs proves wasteful, an easy follow-up is to skip `m.fresh` in `shownPRNumbers`. **Open question:** is refetching-all the intended behavior, or should the sweep trust session-fresh detail?
- **Assumption — no auto-refresh after bulk merge/update.** The existing `m`/`u` actions settle an inline badge and leave the board as-is (no refetch). The sweep reuses that path verbatim, so after `m`/`u` the board is not re-swept. Matches existing behavior; not gold-plated. **Open question:** should completing a bulk action kick a background refresh or re-open the sweep?
- **Assumption — `lipgloss.Style` is the concrete type of the package styles** (`passStyle` etc.). Confirmed they're used as `x.Render(...)` in `card.go`; if `go build` rejects the `bucket` closure's `style lipgloss.Style` parameter, change it to `func(string) string` and pass `passStyle.Render` etc. This is the only unverified type in the plan.
- **Assumption — key-message construction in tests.** Task 4's routing test uses `tea.KeyPressMsg`; verify against the established form in `internal/ui/*_test.go` before writing it (the state-machine tests in Tasks 1-3 avoid key input and are unaffected).
- **Not built (out of scope, YAGNI):** no header spinner for the sweep — the progress counter advances naturally as `sweepDetailMsg`es arrive; no per-PR live board flagging (buckets live only in the overlay); no merge-strategy choice (reuses `--squash` from the `m` action).

## Self-review notes

- **Spec coverage:** (1) bounded fan-out over all shown PRs → Task 2 `startSweep`/`sweepDispatch` (`sweepConcurrency`); (2) buckets via `triage.Compute` → Task 1 `sweepBuckets`; (3) overlay with `u`/`m` bulk actions reusing `startBulk`/`runBulk` incl. confirm → Task 3 `sweepView` + Task 4 `applySweepAction`; progress indicator → Task 3 progress line; partial failure → Task 2 `sweepDetailMsg.err` path + `errs`→Other; `S` enter / `esc` dismiss → Task 4 guard. All covered.
- **Type consistency:** `sweepState`/`sweepBuckets`/`sweepDetailMsg` fields and the `startSweep`→`sweepDispatch`→`sweepFetchCmd`→`sweepDetailMsg`→`sweepBuckets`→`applySweepAction` chain use consistent names across tasks.
- **No board mutation:** the sweep only reads `m.section`/`m.detail` and writes `m.sweep`/`m.sel`; it never calls `setPRs`/`applyFilter`/`SetShown`.
