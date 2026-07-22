# Confirm auto-merge / mark-ready on others' PRs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prompt for confirmation before arming auto-merge (`A`) or marking ready (`M`) on a PR the viewer didn't author, and always prompt when the action fans out across a bulk selection.

**Architecture:** A new declarative `ConfirmOthers bool` on `action.Action` marks the two "arm and walk away" actions. The UI's `startBulk` owns the ownership check — comparing each target's author login against the cached `m.viewerLogin` — because that state is UI-local. The existing `confirmPanel` y/n dialog is reused, with wording that names the author on a foreign single target.

**Tech Stack:** Go, Bubble Tea (charmbracelet), standard `testing`.

## Global Constraints

- Applies to `A` (auto-merge, squash) and `M` (mark ready) only. Plain merge `m` (already `Confirm: true`) and lower-stakes `u`/`r` are untouched.
- Ownership signal: `m.viewerLogin` (authenticated login) vs `section.VarsAt(i).Author` (PR author login).
- Empty `m.viewerLogin` (login not yet fetched) → treat as "not mine" → prompt.
- No new dependencies.

---

### Task 1: Declare `ConfirmOthers` and wire the defaults

**Files:**
- Modify: `internal/action/action.go:26-39` (add struct field)
- Modify: `internal/action/defaults.go:12-15,33-35` (set field on `A` and `M`)
- Test: `internal/action/defaults_test.go`

**Interfaces:**
- Produces: `action.Action.ConfirmOthers bool` — consumed by Task 2 (`startBulk`) and Task 3 (`confirmPanel`).

- [ ] **Step 1: Write the failing test**

Add to `internal/action/defaults_test.go`:

```go
func TestDefaultsConfirmOthers(t *testing.T) {
	d := DefaultPRActions()
	if !d["A"].ConfirmOthers {
		t.Error("auto-merge (A) must confirm on others' PRs")
	}
	if !d["M"].ConfirmOthers {
		t.Error("mark ready (M) must confirm on others' PRs")
	}
	for _, k := range []string{"m", "u", "r", "o", "enter", "W"} {
		if d[k].ConfirmOthers {
			t.Errorf("action %q should not set ConfirmOthers", k)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/action/ -run TestDefaultsConfirmOthers -v`
Expected: FAIL — `d.ConfirmOthers undefined (type Action has no field or method ConfirmOthers)` (compile error).

- [ ] **Step 3: Add the struct field**

In `internal/action/action.go`, add to the `Action` struct after the `Confirm bool` field (line 32):

```go
	Confirm       bool
	ConfirmOthers bool // prompt before running when a target PR was authored by someone other than the viewer, or when the action spans a bulk selection
	Refresh       bool // action mutates the PR; the UI refetches list+detail on success
```

(Re-align the neighboring field comments to the widened column as gofmt dictates.)

- [ ] **Step 4: Wire the defaults**

In `internal/action/defaults.go`, set the field on the `A` action (line 12-15):

```go
		"A": {Key: "A", Label: "Auto-merge (squash)",
			Command:       Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--auto", "--squash"}},
			Scope:         "per-selected", ConfirmOthers: true,
			Progress: "Enabling auto-merge", Past: "Auto-merge on", Fail: "Auto-merge failed"},
```

and on the `M` action (line 33-35):

```go
		"M": {Key: "M", Label: "Mark ready",
			Command: Command{Argv: []string{"gh", "pr", "ready", "{{.Number}}"}}, Scope: "per-selected", Refresh: true,
			ConfirmOthers: true,
			Progress:      "Marking ready", Past: "Marked ready", Fail: "Mark-ready failed"},
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/action/ -run TestDefaultsConfirmOthers -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/action/action.go internal/action/defaults.go internal/action/defaults_test.go
git commit -m "feat(action): add ConfirmOthers, set on auto-merge and mark-ready"
```

---

### Task 2: Ownership-gated prompt in `startBulk`

**Files:**
- Modify: `internal/ui/actions.go:258-267` (`startBulk`; add `needsOthersConfirm` helper)
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `action.Action.ConfirmOthers` (Task 1); existing `m.selectedOrCursor() []int`, `m.section.VarsAt(i) action.Vars` (`.Author string`), `m.viewerLogin string`, `m.section.Len() int`.
- Produces: `func (m *Model) needsOthersConfirm(a action.Action) bool`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/ui/actions_test.go`. `automergeAction` is a local helper so each case stays a one-liner:

```go
func automergeAction() action.Action {
	return action.Action{
		Key: "A", Scope: "per-selected", ConfirmOthers: true,
		Command: action.Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--auto", "--squash"}},
	}
}

func TestConfirmOthersOwnPRRunsImmediately(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "me"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd == nil {
		t.Fatal("own PR should run without a prompt")
	}
	if m.pending != nil {
		t.Fatal("own PR must not set pending")
	}
}

func TestConfirmOthersForeignPRPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("foreign PR should defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("foreign PR must set pending")
	}
}

func TestConfirmOthersBulkAlwaysPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p1, p2 := gh.PR{Number: 7}, gh.PR{Number: 9}
	p1.Author.Login = "me"
	p2.Author.Login = "me"
	sec.SetPRs([]gh.PR{p1, p2})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("bulk should always defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("bulk must set pending even when all PRs are the viewer's")
	}
}

func TestConfirmOthersUnknownViewerPrompts(t *testing.T) {
	m := NewModel("/repo", "is:open", nil) // viewerLogin == ""
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 7}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec

	if cmd := m.startBulk(automergeAction()); cmd != nil {
		t.Fatal("unresolved viewer login should defer to a prompt (nil cmd)")
	}
	if m.pending == nil {
		t.Fatal("unresolved viewer login must set pending")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestConfirmOthers -v`
Expected: FAIL — `TestConfirmOthersForeignPRPrompts`, `TestConfirmOthersBulkAlwaysPrompts`, `TestConfirmOthersUnknownViewerPrompts` fail (auto-merge runs immediately, `pending` stays nil) because `startBulk` does not yet consult `ConfirmOthers`. `TestConfirmOthersOwnPRRunsImmediately` may already pass.

- [ ] **Step 3: Add the ownership gate to `startBulk`**

In `internal/ui/actions.go`, replace `startBulk` (lines 258-267) and add the helper below it:

```go
// startBulk runs a per-selected action, first prompting when it needs confirming,
// targets a PR the viewer didn't author or fans out across a bulk selection, or
// when it would open more than bulkWarnThreshold worktrees.
func (m *Model) startBulk(a action.Action) tea.Cmd {
	overThreshold := a.ExitsTUI && len(m.selectedOrCursor()) > bulkWarnThreshold
	if a.Confirm || overThreshold || m.needsOthersConfirm(a) {
		m.pending = &a
		return nil
	}
	return m.runBulk(a)
}

// needsOthersConfirm reports whether a ConfirmOthers action must prompt first:
// always for a bulk fan-out, and for a single target authored by someone other
// than the viewer. An unresolved viewer login counts as "not mine".
func (m *Model) needsOthersConfirm(a action.Action) bool {
	if !a.ConfirmOthers {
		return false
	}
	targets := m.selectedOrCursor()
	if len(targets) > 1 {
		return true
	}
	i := targets[0]
	if i < 0 || i >= m.section.Len() {
		return true // can't verify ownership → prompt
	}
	return m.section.VarsAt(i).Author != m.viewerLogin
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestConfirmOthers -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/actions.go internal/ui/actions_test.go
git commit -m "feat(ui): gate auto-merge/mark-ready behind an ownership + bulk confirm"
```

---

### Task 3: Name the author in the confirm wording

**Files:**
- Modify: `internal/ui/prlist.go:1550-1570` (`confirmPanel`; extract `confirmQuestion`)
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: `m.pending *action.Action` (`.Scope`, `.Label`, `.ConfirmOthers`), `m.selectedOrCursor()`, `m.cursorVars()`, `m.section.VarsAt(i)`, `m.viewerLogin`.
- Produces: `func (m Model) confirmQuestion() string`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/actions_test.go`:

```go
func TestConfirmQuestionNamesForeignAuthor(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p := gh.PR{Number: 42}
	p.Author.Login = "alice"
	sec.SetPRs([]gh.PR{p})
	m.section = sec
	a := automergeAction()
	m.pending = &a

	q := m.confirmQuestion()
	if !strings.Contains(q, "#42") || !strings.Contains(q, "alice") {
		t.Fatalf("foreign single-target wording should name the PR and author: %q", q)
	}
}

func TestConfirmQuestionBulkShowsCount(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.viewerLogin = "me"
	sec := NewPRSection("is:open")
	p1, p2 := gh.PR{Number: 7}, gh.PR{Number: 9}
	p1.Author.Login = "me"
	p2.Author.Login = "me"
	sec.SetPRs([]gh.PR{p1, p2})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(1)
	a := automergeAction()
	m.pending = &a

	q := m.confirmQuestion()
	if !strings.Contains(q, "for 2 PRs") {
		t.Fatalf("bulk wording should show the count: %q", q)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run TestConfirmQuestion -v`
Expected: FAIL — `m.confirmQuestion undefined (type Model has no field or method confirmQuestion)` (compile error).

- [ ] **Step 3: Extract `confirmQuestion` and consume it from `confirmPanel`**

In `internal/ui/prlist.go`, replace the wording block inside `confirmPanel` (lines 1551-1561) so it delegates, and add the new method above it:

```go
// confirmQuestion is the y/n prompt text for the pending action. A single target
// the viewer didn't author names its author (so an accidental keystroke on
// someone else's PR is obvious); a bulk fan-out shows the count.
func (m Model) confirmQuestion() string {
	a := m.pending
	if a.Scope != "per-selected" {
		n := 0
		if v, ok := m.cursorVars(); ok {
			n = v.Number
		}
		return fmt.Sprintf("%s #%d?", a.Label, n)
	}
	targets := m.selectedOrCursor()
	if len(targets) != 1 {
		return fmt.Sprintf("%s for %d PRs?", a.Label, len(targets))
	}
	v := m.section.VarsAt(targets[0])
	if a.ConfirmOthers && v.Author != "" && v.Author != m.viewerLogin {
		return fmt.Sprintf("%s #%d by %s?", a.Label, v.Number, v.Author)
	}
	return fmt.Sprintf("%s #%d?", a.Label, v.Number)
}

// confirmPanel is the y/n dialog for a pending action.
func (m Model) confirmPanel() string {
	q := m.confirmQuestion()
	hint := accentStyle.Render("y") + statusBarStyle.Render(" confirm   ") +
		accentStyle.Render("n") + statusBarStyle.Render(" cancel")
	body := titleStyle.Render(q) + "\n\n" + hint
	w := lipgloss.Width(q) + 6
	if w < 34 {
		w = 34
	}
	return titledBox(body, w, 5, "Confirm")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run TestConfirmQuestion -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite + vet**

Run: `go test ./... && go vet ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/actions_test.go
git commit -m "feat(ui): name the author in the auto-merge/mark-ready confirm prompt"
```

---

## Self-Review

**Spec coverage:**
- "Single target, prompt only when author ≠ viewer" → Task 2 `needsOthersConfirm` (len==1 branch) + tests `...OwnPRRunsImmediately`, `...ForeignPRPrompts`.
- "Bulk always prompts" → Task 2 (len>1 branch) + `...BulkAlwaysPrompts`.
- "Empty viewerLogin → prompt" → Task 2 (`!=` comparison + range guard) + `...UnknownViewerPrompts`.
- "Applies to A and M only" → Task 1 field + defaults + `TestDefaultsConfirmOthers` (asserts others stay false); `m` still `Confirm:true`, unaffected.
- "Wording: single-foreign names author; bulk shows count" → Task 3 `confirmQuestion` + its two tests.

**Placeholder scan:** none — every code and command step is concrete.

**Type consistency:** `ConfirmOthers bool` (Task 1) is read in Tasks 2 and 3. `needsOthersConfirm(action.Action) bool` and `confirmQuestion() string` names match between definition and test call sites. `VarsAt(i).Author`, `m.viewerLogin`, `m.selectedOrCursor()`, `m.section.Len()` verified against the current source.
