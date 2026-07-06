# Live Combined Checks View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a PR's failed and still-running checks together in the triage card, and auto-poll the whole list every 30s while any shown PR has running checks.

**Architecture:** Two independent changes. (1) `triage.Card` gains separate `Failing`/`Running` slices; both card builders (`Compute`, `Preliminary`) populate both and `renderCard` styles them apart. (2) A self-rescheduling `checksPollTick` loop (mirroring the existing `spinnerTick` idiom) fires a silent background list refresh while `anyChecksRunning()` holds, stopping itself when checks settle.

**Tech Stack:** Go, Bubble Tea (`tea.Cmd`/`tea.Tick`/`tea.Msg`), lipgloss.

## Global Constraints

- Match the existing lean style: no comments that restate code; explain only non-obvious WHY.
- Passed checks stay out of the card — the full pass/fail/pending list already lives in the expanded **Checks** tab (unchanged).
- `pollInterval` is a `const` — no config knob.
- The background poll must never clear rows or reorder them under an active user interaction.

---

### Task 1: Combined checks display (triage + card render)

Show failing and running checks together, styled apart. Touches the `triage` package (data + logic) and the `ui` card renderer.

**Files:**
- Modify: `internal/triage/triage.go` (Card struct, `Compute`, `Preliminary`, add `checksFailingCard`)
- Modify: `internal/triage/triage_test.go`
- Modify: `internal/ui/card.go` (`renderCard`)
- Modify: `internal/ui/card_test.go` (existing test uses the removed `Lines` field)

**Interfaces:**
- Produces: `triage.Card{ Kind, Headline, Failing []string, Running []string, ActionKey, ActionLabel, JumpTab }` — the `Lines` field is removed. `renderCard(c triage.Card, width int) string`.
- Consumes: existing `checksByState(pr, want) []string`, `ChecksFailingHeadline(n int) string`, `gh.PR.CIState() string`.

- [ ] **Step 1: Write the failing triage tests**

First, update the pre-existing `TestFailingChecksListed` — it reads the removed `Lines` field, so switch it to `Failing`:

```go
	if len(card.Failing) == 0 || card.Failing[0] != "lint" {
		t.Errorf("expected failing check 'lint' listed: %+v", card.Failing)
	}
```

Then add to `internal/triage/triage_test.go`:

```go
func TestChecksCardShowsFailingAndRunningTogether(t *testing.T) {
	p := pr(
		gh.Check{State: "FAILURE", Name: "lint"},
		gh.Check{State: "PENDING", Name: "build"},
		gh.Check{State: "PENDING", Name: "e2e"},
	)
	c := Compute(p, gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if c.Kind != KindChecksFailing {
		t.Fatalf("Kind = %v, want KindChecksFailing", c.Kind)
	}
	if got := c.Failing; len(got) != 1 || got[0] != "lint" {
		t.Fatalf("Failing = %v, want [lint]", got)
	}
	if got := c.Running; len(got) != 2 {
		t.Fatalf("Running = %v, want 2 entries", got)
	}
	if c.Headline != "1 failing · 2 running" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "1 failing · 2 running")
	}
}

func TestChecksFailingOnlyHeadlineUnchanged(t *testing.T) {
	c := Compute(pr(gh.Check{State: "FAILURE", Name: "lint"}), gh.PRDetail{MergeStateStatus: "BLOCKED"})
	if c.Headline != "1 check failing" {
		t.Fatalf("Headline = %q, want %q", c.Headline, "1 check failing")
	}
	if len(c.Running) != 0 {
		t.Fatalf("Running = %v, want empty", c.Running)
	}
}

func TestChecksRunningCardPopulatesRunning(t *testing.T) {
	c := Compute(pr(gh.Check{State: "PENDING", Name: "build"}), gh.PRDetail{MergeStateStatus: "UNSTABLE"})
	if c.Kind != KindChecksRunning {
		t.Fatalf("Kind = %v, want KindChecksRunning", c.Kind)
	}
	if got := c.Running; len(got) != 1 || got[0] != "build" {
		t.Fatalf("Running = %v, want [build]", got)
	}
}

func TestPreliminaryFoldsRunningIntoFailingCard(t *testing.T) {
	c := Preliminary(pr(
		gh.Check{State: "FAILURE", Name: "lint"},
		gh.Check{State: "PENDING", Name: "build"},
	))
	if c.Kind != KindChecksFailing {
		t.Fatalf("Kind = %v, want KindChecksFailing", c.Kind)
	}
	if len(c.Failing) != 1 || len(c.Running) != 1 {
		t.Fatalf("Failing=%v Running=%v, want one each", c.Failing, c.Running)
	}
}
```

- [ ] **Step 2: Run the triage tests to verify they fail**

Run: `go test ./internal/triage/`
Expected: FAIL — compile error, `c.Failing`/`c.Running` undefined on `triage.Card`.

- [ ] **Step 3: Implement the triage changes**

In `internal/triage/triage.go`, replace the `Card` struct's `Lines` field:

```go
type Card struct {
	Kind        Kind
	Headline    string
	Failing     []string // failing check labels
	Running     []string // in-progress check labels
	ActionKey   string   // key the user presses to act ("" if none)
	ActionLabel string
	JumpTab     string // "" | "checks" | "reviews" | "conversation"
}
```

Add the shared builder (place it just above `ChecksFailingHeadline`):

```go
// checksFailingCard builds the failing-checks card, folding any still-running
// checks in as a second group so the summary shows both at once.
func checksFailingCard(failing, pending []string) Card {
	headline := ChecksFailingHeadline(len(failing))
	if len(pending) > 0 {
		headline = fmt.Sprintf("%d failing · %d running", len(failing), len(pending))
	}
	return Card{Kind: KindChecksFailing, Headline: headline,
		Failing: failing, Running: pending,
		ActionKey: "r", ActionLabel: "rerun checks", JumpTab: "checks"}
}
```

In `Compute`, replace the failing branch (currently lines 50-53):

```go
	case len(failing) > 0:
		return checksFailingCard(failing, pending)
```

and the running branch (currently lines 69-71):

```go
	case len(pending) > 0 || mss == "UNSTABLE":
		return Card{Kind: KindChecksRunning, Headline: "Checks running…",
			Running: pending, JumpTab: "checks"}
```

In `Preliminary`, compute pending once at the top and reuse it:

```go
func Preliminary(pr gh.PR) Card {
	failing := checksByState(pr, "fail")
	pending := checksByState(pr, "pending")
	switch {
	case pr.IsDraft:
		return Card{Kind: KindDraft, Headline: "Draft — not ready",
			ActionKey: "M", ActionLabel: "Mark ready"}
	case len(failing) > 0:
		return checksFailingCard(failing, pending)
	case pr.ReviewDecision == "CHANGES_REQUESTED":
		return Card{Kind: KindChangesRequested, Headline: "Changes requested",
			ActionKey: "enter", ActionLabel: "worktree to address", JumpTab: "reviews"}
	case pr.CIState() == "pending":
		return Card{Kind: KindChecksRunning, Headline: "Checks running…",
			Running: pending, JumpTab: "checks"}
	case pr.ReviewDecision == "REVIEW_REQUIRED":
		return Card{Kind: KindAwaitingReview, Headline: "Awaiting review", JumpTab: "reviews"}
	default:
		return Card{Kind: KindFallback, Headline: ""}
	}
}
```

- [ ] **Step 4: Run the triage tests to verify they pass**

Run: `go test ./internal/triage/`
Expected: PASS (all triage tests, including the pre-existing `TestLadderPriority`/`TestPreliminary`).

- [ ] **Step 5: Update the card render tests**

In `internal/ui/card_test.go`, change the existing `TestRenderCardShowsHeadlineAndAction` to use `Failing` instead of the removed `Lines`:

```go
func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []string{"lint", "e2e"}, ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40)
	if !strings.Contains(out, "2 checks failing") {
		t.Fatalf("headline missing: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "e2e") {
		t.Fatalf("failing checks missing: %q", out)
	}
	if !strings.Contains(out, "r") || !strings.Contains(out, "rerun checks") {
		t.Fatalf("suggested action missing: %q", out)
	}
}
```

Add a new test for the combined styling:

```go
func TestRenderCardShowsFailingAndRunningGlyphs(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 failing · 1 running",
		Failing: []string{"lint"}, Running: []string{"build"},
		ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40)
	if !strings.Contains(out, "✗ lint") {
		t.Fatalf("failing glyph/label missing: %q", out)
	}
	if !strings.Contains(out, "● build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
}

func TestRenderCardRunningOnlyHasNoFailGlyph(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksRunning, Headline: "Checks running…",
		Running: []string{"build"}}
	out := renderCard(c, 40)
	if !strings.Contains(out, "● build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("unexpected fail glyph on running-only card: %q", out)
	}
}
```

- [ ] **Step 6: Run the ui card tests to verify they fail**

Run: `go test ./internal/ui/ -run TestRenderCard`
Expected: FAIL — compile error, `renderCard` still ranges over `c.Lines`.

- [ ] **Step 7: Implement the card renderer**

In `internal/ui/card.go`, replace the single `Lines` loop (currently lines 31-33) with two styled loops:

```go
	for _, l := range c.Failing {
		b.WriteString("  " + failStyle.Render("✗ "+truncate(l, width-4)) + "\n")
	}
	for _, l := range c.Running {
		b.WriteString("  " + pendStyle.Render("● "+truncate(l, width-4)) + "\n")
	}
```

- [ ] **Step 8: Run the full ui + triage suites to verify they pass**

Run: `go test ./internal/triage/ ./internal/ui/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/triage/triage.go internal/triage/triage_test.go internal/ui/card.go internal/ui/card_test.go
git commit -m "feat(ui): show failing and running checks together in the triage card"
```

---

### Task 2: Live whole-list auto-poll

Auto-refresh the list every 30s while any shown PR has running checks; stop when they settle; never fetch mid-interaction.

**Files:**
- Modify: `internal/ui/messages.go` (new `checksPollMsg`)
- Modify: `internal/ui/prlist.go` (Model field, tick, helpers, message handler, wiring)
- Modify: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes (from existing code): `m.section.(*PRSection)`, `(*PRSection).Len()`, `(*PRSection).prAt(i) gh.PR`, `gh.PR.CIState() string`, `m.fetchCmd(filter) tea.Cmd`, `m.mineFetchCmd() tea.Cmd`, `m.isMineView() bool`, `m.startSpinner() tea.Cmd`, `m.actionRunning() bool`, and the bool fields `m.refreshing`, `m.filtering`, `m.showPicker` and pointer `m.pending`.
- Produces: `m.anyChecksRunning() bool`, `m.pollBusy() bool`, `(*Model).maybeStartPoll() tea.Cmd`, `(*Model).backgroundRefresh() tea.Cmd`, `checksPollTick() tea.Cmd`, `const pollInterval`, `m.polling bool`, `checksPollMsg`.

- [ ] **Step 1: Write the failing poll tests**

Add to `internal/ui/prlist_test.go`:

```go
func TestAnyChecksRunningDetectsPending(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}},
		{Number: 2, StatusCheckRollup: []gh.Check{{State: "PENDING"}}},
	})
	if !m.anyChecksRunning() {
		t.Fatal("expected a running check to be detected")
	}
}

func TestAnyChecksRunningFalseWhenAllSettled(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}})
	if m.anyChecksRunning() {
		t.Fatal("did not expect any running checks")
	}
}

func TestFetchStartsPollLoopWhenChecksRunning(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	u, _ := m.Update(prsFetchedMsg{prs: []gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "PENDING"}}},
	}})
	if !u.(Model).polling {
		t.Fatal("expected poll loop to start after a fetch with running checks")
	}
}

func TestFetchDoesNotStartPollWhenAllSettled(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	u, _ := m.Update(prsFetchedMsg{prs: []gh.PR{
		{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}},
	}})
	if u.(Model).polling {
		t.Fatal("did not expect poll loop with no running checks")
	}
}

func TestPollTickStopsWhenChecksSettle(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.polling = true
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "SUCCESS"}}}})
	u, cmd := m.Update(checksPollMsg{})
	if u.(Model).polling {
		t.Fatal("expected poll loop to stop when nothing is running")
	}
	if cmd != nil {
		t.Fatal("expected no reschedule after the loop stops")
	}
}

func TestPollBusySkipsFetchButStaysAlive(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.polling = true
	m.refreshing = true // a fetch is already in flight
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{{State: "PENDING"}}}})
	if !m.pollBusy() {
		t.Fatal("expected pollBusy while refreshing")
	}
	u, cmd := m.Update(checksPollMsg{})
	if !u.(Model).polling {
		t.Fatal("poll loop should stay alive while busy")
	}
	if cmd == nil {
		t.Fatal("expected the loop to reschedule even when it skips a fetch")
	}
}
```

- [ ] **Step 2: Run the poll tests to verify they fail**

Run: `go test ./internal/ui/ -run 'Poll|AnyChecks|Fetch'`
Expected: FAIL — compile error, `anyChecksRunning`/`pollBusy`/`checksPollMsg`/`m.polling` undefined.

- [ ] **Step 3: Add the `checksPollMsg`**

In `internal/ui/messages.go`, add:

```go
// checksPollMsg fires the live-checks poll beat; the loop runs only while some
// shown PR has a running check.
type checksPollMsg struct{}
```

- [ ] **Step 4: Add the model field**

In `internal/ui/prlist.go`, add to the `Model` struct next to `spinning` (line 55):

```go
	polling         bool        // the live-checks poll tick loop is running
```

- [ ] **Step 5: Add the tick, predicates, and helpers**

In `internal/ui/prlist.go`, next to `spinnerTick`/`startSpinner` (around line 388-402):

```go
const pollInterval = 30 * time.Second

func checksPollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return checksPollMsg{} })
}

// anyChecksRunning reports whether any shown PR row has an in-flight check.
func (m Model) anyChecksRunning() bool {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return false
	}
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).CIState() == "pending" {
			return true
		}
	}
	return false
}

// pollBusy reports whether a user interaction or an in-flight fetch should defer
// this poll beat, so the background refresh never reorders rows under the user.
func (m Model) pollBusy() bool {
	return m.refreshing || m.filtering || m.showPicker || m.pending != nil || m.actionRunning()
}

// maybeStartPoll kicks the poll loop when a fetch reveals running checks, unless
// it is already running (one loop only, like the spinner).
func (m *Model) maybeStartPoll() tea.Cmd {
	if m.polling || !m.anyChecksRunning() {
		return nil
	}
	m.polling = true
	return checksPollTick()
}

// backgroundRefresh silently reconciles the current view without clearing rows —
// the same fetch path as a filter switch, minus the row reset.
func (m *Model) backgroundRefresh() tea.Cmd {
	m.refreshing = true
	fetch := m.fetchCmd(m.filter)
	if m.isMineView() {
		fetch = m.mineFetchCmd()
	}
	return tea.Batch(fetch, m.startSpinner())
}
```

- [ ] **Step 6: Handle the poll message**

In `internal/ui/prlist.go`, add a case to the `Update` switch (next to the `spinnerTickMsg` case, ~line 582):

```go
	case checksPollMsg:
		if !m.anyChecksRunning() {
			m.polling = false
			return m, nil
		}
		if m.pollBusy() {
			return m, checksPollTick() // skip this beat, keep the loop alive
		}
		return m, tea.Batch(m.backgroundRefresh(), checksPollTick())
```

- [ ] **Step 7: Wire the loop start into the fetch handlers**

In `internal/ui/prlist.go`, extend the `prsFetchedMsg` return (currently line 534) and the `mineFetchedMsg` return (currently line 550) to batch in `maybeStartPoll`:

```go
		return m, tea.Batch(m.detailCmdForCursor(), m.prefetchCmd(), m.maybeStartPoll())
```

Apply the identical change to both return statements.

- [ ] **Step 8: Run the poll tests to verify they pass**

Run: `go test ./internal/ui/ -run 'Poll|AnyChecks|Fetch'`
Expected: PASS.

- [ ] **Step 9: Run the full suite + build**

Run: `go test ./... && go build ./...`
Expected: PASS, clean build.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/messages.go internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): live-poll the list every 30s while checks are running"
```

---

## Notes for the implementer

- `Update` has a value receiver (`func (m Model) Update`), so `m` is a local, addressable copy — calling the pointer-receiver helpers (`maybeStartPoll`, `backgroundRefresh`) on it and returning `m` is correct and mutates only that copy.
- `time` is already imported in `prlist.go` (used by `spinnerTick`); no new import there. `messages.go` needs no new import.
- Do not add a manual-refresh keybinding or a config knob — out of scope.
