# Reversible Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `A` (auto-merge) and `M` (mark ready) toggle instead of firing one way only.

**Architecture:** Two new source methods mirroring the existing enable/mark-ready pair. Each key resolves to one of two `Action` variants based on the **cursor** PR's state, so `Action` keeps its plain static strings and only dispatch and hint rendering learn about state. For a multi-row selection the cursor's state picks one direction for the whole set.

**Tech Stack:** Go 1.24, shurcooL/githubv4, stdlib `testing`.

Spec: `docs/superpowers/specs/2026-08-04-reversible-actions-design.md`
Issue: #90 · Independent of #88 and #89, but Task 4 touches the same optimistic-paint path as #88 Task 4.

## Global Constraints

- Mutation inputs are the `githubv4` typed structs — `DisablePullRequestAutoMergeInput{PullRequestID}` and `ConvertPullRequestToDraftInput{PullRequestID}`. Both are verified present in the pinned module and take exactly that one required field.
- **The cursor decides the direction for the whole selection.** Never flip rows independently: one keystroke producing three different outcomes cannot be undone by a second press.
- The panel label is the safety mechanism for a state-dependent key. If `actionHints` does not show the direction before the key is pressed, the feature is a trap — Task 3 is not optional.
- Run `go test ./...` from the repo root before every commit.
- Conventional Commits with a scope.

---

### Task 1: The two reverse mutations

**Files:**
- Modify: `internal/gh/mutations_graphql.go`
- Modify: `internal/gh/source.go` (`MutationSource` interface ~line 58)
- Test: `internal/gh/mutations_graphql_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `DisableAutoMerge(prID string) error` and `ConvertToDraft(prID string) error` on `GraphSource` and on the `MutationSource` interface. Task 2 calls both.

- [ ] **Step 1: Write the failing test**

Mirror whatever pattern the existing `EnableAutoMerge` / `MarkReady` tests use in this package (a stub HTTP server asserting the mutation name). Add:

```go
func TestDisableAutoMergeIssuesTheDisableMutation(t *testing.T) {
	var got string
	s := newTestGraphSource(t, func(body string) string {
		got = body
		return `{"data":{"disablePullRequestAutoMerge":{"pullRequest":{"number":3065}}}}`
	})
	if err := s.DisableAutoMerge("PR_kwDOA123"); err != nil {
		t.Fatalf("DisableAutoMerge: %v", err)
	}
	if !strings.Contains(got, "disablePullRequestAutoMerge") {
		t.Errorf("wrong mutation:\n%s", got)
	}
	if !strings.Contains(got, "PR_kwDOA123") {
		t.Errorf("PR id not sent:\n%s", got)
	}
}

func TestConvertToDraftIssuesTheConvertMutation(t *testing.T) {
	var got string
	s := newTestGraphSource(t, func(body string) string {
		got = body
		return `{"data":{"convertPullRequestToDraft":{"pullRequest":{"number":3083}}}}`
	})
	if err := s.ConvertToDraft("PR_kwDOA456"); err != nil {
		t.Fatalf("ConvertToDraft: %v", err)
	}
	if !strings.Contains(got, "convertPullRequestToDraft") {
		t.Errorf("wrong mutation:\n%s", got)
	}
}
```

If `newTestGraphSource` does not exist under that name, use the helper the existing mutation tests use — read `internal/gh/mutations_graphql_test.go` first and match it exactly rather than inventing a fixture.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gh/ -run 'TestDisableAutoMerge|TestConvertToDraft' -v`
Expected: FAIL to compile — `s.DisableAutoMerge undefined`.

- [ ] **Step 3: Add the methods**

In `internal/gh/mutations_graphql.go`, directly after `EnableAutoMerge`:

```go
// DisableAutoMerge disarms squash auto-merge, the inverse of EnableAutoMerge.
func (s GraphSource) DisableAutoMerge(prID string) error {
	var mut struct {
		DisablePullRequestAutoMerge struct {
			PullRequest struct{ Number int }
		} `graphql:"disablePullRequestAutoMerge(input: $input)"`
	}
	input := githubv4.DisablePullRequestAutoMergeInput{PullRequestID: githubv4.ID(prID)}
	ctx, cancel := context.WithTimeout(context.Background(), graphTimeout)
	defer cancel()
	return s.client.Mutate(ctx, &mut, input, nil)
}
```

And after `MarkReady`:

```go
// ConvertToDraft puts a PR back into draft, the inverse of MarkReady. More
// disruptive than it looks: it removes the PR from every reviewer's queue.
func (s GraphSource) ConvertToDraft(prID string) error {
	var mut struct {
		ConvertPullRequestToDraft struct {
			PullRequest struct{ Number int }
		} `graphql:"convertPullRequestToDraft(input: $input)"`
	}
	input := githubv4.ConvertPullRequestToDraftInput{PullRequestID: githubv4.ID(prID)}
	ctx, cancel := context.WithTimeout(context.Background(), graphTimeout)
	defer cancel()
	return s.client.Mutate(ctx, &mut, input, nil)
}
```

Match the context/timeout idiom of the neighbouring methods — read `EnableAutoMerge` and copy its exact shape rather than the sketch above if they differ.

- [ ] **Step 4: Extend the interface**

In `internal/gh/source.go`, add to `MutationSource` beside `EnableAutoMerge` and `MarkReady`:

```go
	DisableAutoMerge(prID string) error
	ConvertToDraft(prID string) error
```

- [ ] **Step 5: Update the test doubles**

Run: `go build ./... && go vet ./...`
Expected: failures naming every fake that implements `MutationSource`. Add both methods to each (`internal/ui/mutationsource_test.go` and any others `rg -l 'EnableAutoMerge' --type go` reports), recording the call the same way the existing fakes record `EnableAutoMerge`.

- [ ] **Step 6: Run tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/gh/ internal/ui/
git commit -m "feat(gh): add DisableAutoMerge and ConvertToDraft

Exact inverses of EnableAutoMerge and MarkReady, taking the same
single-field githubv4 input.

Refs #90"
```

---

### Task 2: Direction from cursor state

**Files:**
- Modify: `internal/action/defaults.go`
- Modify: `internal/ui/actions.go` (~line 224-243)
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: Task 1's source methods.
- Produces: natives `"auto-merge-off"` and `"mark-draft"`; `func (m Model) actionVariant(key string) action.Action`. Task 3 calls `actionVariant`.

- [ ] **Step 1: Write the failing test**

```go
func TestActionVariantFollowsCursorState(t *testing.T) {
	m := newTestModelWideWithPR(t)
	ps := m.section.(*PRSection)

	ps.prs[0].AutoMergeRequest = nil
	ps.prs[0].IsDraft = true
	if got := m.actionVariant("A").Command.Native; got != "auto-merge-squash" {
		t.Errorf("unarmed cursor: native = %q, want auto-merge-squash", got)
	}
	if got := m.actionVariant("M").Label; got != "Mark ready" {
		t.Errorf("draft cursor: label = %q, want Mark ready", got)
	}

	ps.prs[0].AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
	ps.prs[0].IsDraft = false
	if got := m.actionVariant("A").Command.Native; got != "auto-merge-off" {
		t.Errorf("armed cursor: native = %q, want auto-merge-off", got)
	}
	if got := m.actionVariant("M").Label; got != "Convert to draft" {
		t.Errorf("ready cursor: label = %q, want Convert to draft", got)
	}
}

func TestOtherActionsAreNotStateDependent(t *testing.T) {
	m := newTestModelWideWithPR(t)
	for _, k := range []string{"m", "r", "u", "L", "y", "o"} {
		if got, want := m.actionVariant(k), m.actions[k]; got.Command.Native != want.Command.Native {
			t.Errorf("actionVariant(%q) changed a non-toggle action", k)
		}
	}
}

func TestMixedSelectionTakesOneDirectionFromTheCursor(t *testing.T) {
	m := newTestModelWithRows(t)
	ps := m.section.(*PRSection)
	if ps.Len() < 3 {
		t.Skip("fixture too small")
	}
	// Two armed, one not; cursor on the unarmed one.
	ps.prs[ps.shown[0]].AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
	ps.prs[ps.shown[1]].AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
	ps.prs[ps.shown[2]].AutoMergeRequest = nil
	m.cursor = 2
	for i := 0; i < 3; i++ {
		m.sel.toggle(i)
	}
	if got := m.actionVariant("A").Command.Native; got != "auto-merge-squash" {
		t.Fatalf("native = %q, want auto-merge-squash for every row — the cursor decides", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestActionVariant|TestOtherActions|TestMixedSelection' -v`
Expected: FAIL to compile — `m.actionVariant undefined`.

- [ ] **Step 3: Add the reverse Action definitions**

In `internal/action/defaults.go`, add a second map beside `DefaultPRActions`:

```go
// ReversePRActions are the opposite-direction variants of the toggleable keys.
// Kept as separate Action values so Action stays a plain struct with static
// strings — the UI picks the variant from the cursor PR's state.
func ReversePRActions() map[string]Action {
	return map[string]Action{
		"A": {Key: "A", Label: "Disable auto-merge",
			Command: Command{Native: "auto-merge-off"},
			Scope:   "per-selected", ConfirmOthers: true, Refresh: true,
			Progress: "Disabling auto-merge", Past: "Auto-merge off", Fail: "Disable auto-merge failed"},
		"M": {Key: "M", Label: "Convert to draft",
			Command: Command{Native: "mark-draft"},
			Scope:   "per-selected", ConfirmOthers: true, Refresh: true,
			Progress: "Converting to draft", Past: "Converted to draft", Fail: "Convert to draft failed"},
	}
}
```

- [ ] **Step 4: Add `actionVariant`**

In `internal/ui/actions.go`:

```go
// actionVariant resolves a key to the Action that will actually run. For the two
// toggleable keys the direction comes from the CURSOR PR's state and applies to
// the whole selection: predictable, and the panel label announces it. Flipping
// each selected row independently would make one keystroke produce several
// outcomes with no way to undo them.
func (m Model) actionVariant(key string) action.Action {
	a := m.actions[key]
	rev, ok := action.ReversePRActions()[key]
	if !ok || m.mode != "pr" {
		return a
	}
	ps, isPR := m.section.(*PRSection)
	if !isPR || ps.Len() == 0 {
		return a
	}
	cur := min(max(m.cursor, 0), ps.Len()-1)
	p := ps.prAt(cur)
	switch key {
	case "A":
		if p.AutoMergeEnabled() {
			return rev
		}
	case "M":
		if !p.IsDraft {
			return rev
		}
	}
	return a
}
```

- [ ] **Step 5: Route the new natives**

In `internal/ui/actions.go`, add the two natives to the stale-node-id guard list at line ~218:

```go
		case "merge-squash", "auto-merge-squash", "auto-merge-off", "mark-ready", "mark-draft", "update-branch", "approve":
```

and add the two cases to the dispatch switch:

```go
	case "auto-merge-off":
		if !p.AutoMergeEnabled() {
			return func() error { return nil }, true // already disarmed: a benign no-op, matching mark-ready
		}
		return func() error { return src.DisableAutoMerge(p.ID) }, true
	case "mark-draft":
		if p.State != "OPEN" {
			err := fmt.Errorf("PR #%d is not open", p.Number)
			return func() error { return err }, true
		}
		if p.IsDraft {
			return func() error { return nil }, true // already a draft
		}
		return func() error { return src.ConvertToDraft(p.ID) }, true
```

- [ ] **Step 6: Dispatch through the variant**

Find every place that looks up `m.actions[key]` to run a key (the direct key handler and the actions menu). Replace those lookups with `m.actionVariant(key)`. Use `rg -n 'm\.actions\[' internal/ui/` to find them all; leave lookups that only enumerate the table (like `actionOrder` iteration in Task 3) alone for now.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/ui/ -run 'TestActionVariant|TestOtherActions|TestMixedSelection' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/action/ internal/ui/
git commit -m "feat(actions): A and M toggle based on the cursor PR's state

One direction per keystroke, chosen by the cursor and applied to the
whole selection. Kept as separate Action values so Action stays a plain
struct with static strings.

Refs #90"
```

---

### Task 3: The panel shows the direction

**Files:**
- Modify: `internal/ui/prlist.go` (`actionHints` ~line 2643, `defaultActionHints`)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `actionVariant` (Task 2).
- Produces: hint labels that follow the cursor.

- [ ] **Step 1: Write the failing test**

```go
func TestPanelLabelFollowsCursorStateBeforeThePress(t *testing.T) {
	m := newTestModelWideWithPR(t)
	ps := m.section.(*PRSection)

	ps.prs[0].AutoMergeRequest = nil
	if got := stripANSIForTest(m.actionHints(120)); !strings.Contains(got, "Auto-merge") {
		t.Errorf("unarmed cursor: hints missing the arm verb:\n%s", got)
	}

	ps.prs[0].AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}
	got := stripANSIForTest(m.actionHints(120))
	if !strings.Contains(got, "Disable auto-merge") {
		t.Errorf("armed cursor: hints must announce the disarm direction:\n%s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestPanelLabelFollows -v`
Expected: FAIL — the hint still reads `Auto-merge (squash)` on an armed cursor.

- [ ] **Step 3: Resolve hints through the variant**

In `internal/ui/prlist.go`, in both `actionHints` and `defaultActionHints`, replace the `m.actions[k]` lookup inside the `actionOrder` loop with `m.actionVariant(k)`.

Note both functions iterate `actionOrder`, which keys off the key string — so the display order is unchanged and only the label and progress text move.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestPanelLabel|TestActionHints|TestGridHints' -v`
Expected: PASS. `gridhints_test.go` asserts hint layout differentially; if a width assertion now fails, it is because `Disable auto-merge` is longer than `Auto-merge (squash)` — update the expected cell width rather than shortening the label.

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): panel announces which way A and M will fire

A state-dependent key is only safe if the direction is visible before
the press.

Refs #90"
```

---

### Task 4: Optimistic paint reverses

**Files:**
- Modify: `internal/ui/prlist.go` (`applyOptimisticAction` ~line 231)
- Test: `internal/ui/perf_actions_test.go` or `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: natives from Task 2.
- Produces: `applyOptimisticAction` handling `auto-merge-off` and `mark-draft`.

- [ ] **Step 1: Write the failing test**

```go
func TestOptimisticPaintClearsTheAutoMergeGlyph(t *testing.T) {
	m := newTestModelWideWithPR(t)
	ps := m.section.(*PRSection)
	n := ps.prAt(0).Number
	ps.prs[0].AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}

	m.applyOptimisticAction("auto-merge-off", []int{n})

	if ps.prs[0].AutoMergeRequest != nil {
		t.Error("auto-merge glyph stays lit after a successful disarm")
	}
}

func TestOptimisticPaintSetsTheDraftState(t *testing.T) {
	m := newTestModelWideWithPR(t)
	ps := m.section.(*PRSection)
	n := ps.prAt(0).Number
	ps.prs[0].IsDraft = false

	m.applyOptimisticAction("mark-draft", []int{n})

	if !ps.prs[0].IsDraft {
		t.Error("row does not read as a draft after a successful convert")
	}
}
```

Match `applyOptimisticAction`'s real signature — read it first and adapt the calls; the argument shape above is a guess at "native plus affected PR numbers".

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestOptimisticPaint -v`
Expected: FAIL — the fields are unchanged, because the switch has no arm for either native.

- [ ] **Step 3: Add the reverse arms**

In `applyOptimisticAction`, beside the existing `auto-merge-squash` and `mark-ready` arms:

```go
	case "auto-merge-off":
		// Mirrors the arm-path upsert at the top of this switch. Without it the
		// glyph stays lit until the next background refresh, so the board
		// contradicts the action that just reported success.
		p.AutoMergeRequest = nil
	case "mark-draft":
		p.IsDraft = true
```

Match the surrounding arms' mutation idiom exactly — if they go through `s.updatePR(number, func(*gh.PR))`, use that rather than assigning to a local.

- [ ] **Step 4: Invalidate the row cache**

Confirm the existing arms bump `m.rowGen` (or whatever invalidates `rowSig`) after mutating. If they do, the new arms inherit it via shared code; if each arm does it individually, add it to both. A stale `rowText` would keep painting the old glyph even with the model corrected.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/ -run 'TestOptimistic' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/
git commit -m "fix(ui): un-paint the glyph when a toggle fires in reverse

Disarming cleared nothing, so the auto-merge glyph stayed lit until the
next refresh and contradicted the action that had just succeeded.

Refs #90"
```

---

### Task 5: Verify by hand

**Files:** none — verification gate.

- [ ] **Step 1: Build and run**

```bash
go build -o /tmp/prdash-90 . && cd ~/Data/git/factify-inc/mono && /tmp/prdash-90
```

- [ ] **Step 2: Confirm the label leads the action**

Move the cursor onto one of your own unarmed PRs. The panel must read `Auto-merge (squash)`. Press `A`, confirm the glyph lights. **Without moving the cursor**, confirm the panel now reads `Disable auto-merge`. Press `A` again and confirm the glyph clears immediately, not after the next refresh.

- [ ] **Step 3: Confirm draft round-trips**

On one of your own draft PRs the panel must read `Mark ready`. Press `M`, confirm the gutter glyph changes and the label flips to `Convert to draft`. Press `M` again and confirm it returns to draft.

- [ ] **Step 4: Confirm the mixed-selection rule**

Select three of your own PRs with `space`, two armed and one not, leaving the cursor on the **unarmed** one. Confirm the panel reads the arm verb, press `A`, and confirm all three end up armed — not that two flipped off.

- [ ] **Step 5: Confirm the confirmation still fires**

On someone else's PR, confirm `A` and `M` both still prompt in the reverse direction.

---

## Self-Review

**Spec coverage.** Verified API surface (Task 1), the reversibility audit (spec only — no code, correctly, since `R` and `L` are out of scope), one-key state-dependent dispatch (Task 2), panel label (Task 3), cursor-decides for mixed selections (Task 2's `TestMixedSelectionTakesOneDirectionFromTheCursor` plus Task 5 Step 4), `ConfirmOthers` in both directions (inherited via the `ReversePRActions` definitions, verified in Task 5 Step 5), optimistic un-paint (Task 4).

**Type consistency.** `DisableAutoMerge` / `ConvertToDraft` are named identically in Task 1's implementation, the `MutationSource` interface, and Task 2's dispatch. Natives `auto-merge-off` and `mark-draft` are introduced in Task 2 Step 3 and used with those exact strings in Steps 5 and 6 and in Task 4.

**Two steps carry explicit uncertainty rather than a guess:** Task 1 Step 1 (the test-double helper name) and Task 4 Step 1 (`applyOptimisticAction`'s signature) both instruct the implementer to read the existing code and match it, because inventing a fixture shape there would produce a test that compiles against nothing real.

**Not a dependency, but a collision:** Task 4 edits the same optimistic-paint switch as #88 Task 4, which moves the draft glyph into gutter cell 1. Whichever merges second should re-run the other's tests.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-04-reversible-actions.md`.
