# Open the Linked Issue with `O` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `O` on the PR board opens the issue the row's head branch names — a GitHub issue URL for `#213`, or `linear issue view -w ENG-7659` for a Linear ticket.

**Architecture:** A pure resolver turns `(goos, ticket, prURL)` into an argv, so both destinations share one detached-spawn path and the choice is unit-testable without spawning anything. `O` is a `Native: "open-issue"` action reusing the existing per-selected bulk runner. Nothing new is fetched: the ticket comes from `ticketID(p.HeadRefName)`, already computed for the row's ticket column.

**Tech Stack:** Go, Bubble Tea v2, `os/exec`. No new dependencies.

Spec: `docs/superpowers/specs/2026-09-09-open-linked-issue-design.md`
Issue: [#133](https://github.com/noamsto/prdash/issues/133)
Branch: `feat/133-open-linked-issue` (worktree already created)

## Global Constraints

- No new Go module dependencies. No `LINEAR_API_KEY`, no Linear API client, no config file.
- Never block the TUI on a subprocess: spawn with `cmd.Start()` and reap in a goroutine, exactly as `openURL` does.
- `O` is bound on the PR board only. `DefaultIssueActions()` must not gain an `O` entry.
- The ticket shown in a row's ticket column and the target `O` opens must come from the same `ticketID()` call — they can never diverge.
- Both failure modes (no ticket parsed; opener binary absent) settle to a visible hint carried in `actionStat.err`+`fail`, never `ok` — `ok` renders a green `✓`, so a failure placed there paints as success. `O` must never silently do nothing, and never open a URL whose opener it hasn't confirmed.
- A partly resolvable selection must name the rows it skipped. The bulk runner clears the selection on success, so an unreported skip is unrecoverable as well as invisible.
- The two failure modes are not equivalent. No ticket is benign and only demotes the wording to a count; a missing opener is an actionable config error and takes the fail arm even when other rows opened. Successes still run either way.
- Run `gofmt` on every file touched. The repo has a pre-commit hook chain (`typos`, `trim-trailing-whitespace`, `check-merge-conflicts`) that will reject a commit otherwise.

## File Structure

| File | Responsibility |
|---|---|
| `internal/ui/issuelink.go` (create) | `linkedIssueArgv` + `prIssueURL` — pure ticket→argv resolution |
| `internal/ui/issuelink_test.go` (create) | table tests for both |
| `internal/ui/browser.go` (modify) | extract `spawnDetached`; `openURL` becomes a one-line caller |
| `internal/action/action.go` (modify) | `Vars.Ticket` field |
| `internal/action/defaults.go` (modify) | the `O` entry in `DefaultPRActions()` |
| `internal/ui/section.go:244-249` (modify) | `PRSection.VarsAt` fills `Ticket` |
| `internal/ui/actions.go` (modify) | `open-issue` arm in `runBulkNative` + the hint path |
| `internal/ui/prlist.go:2843,2909` (modify) | legend entry + `actionOrder` |
| `KEYMAP.md` (modify) | Board archetype line |

---

### Task 1: Pure ticket→argv resolution

**Files:**
- Create: `internal/ui/issuelink.go`
- Create: `internal/ui/issuelink_test.go`
- Modify: `internal/ui/browser.go`

**Interfaces:**
- Consumes: `browserArgv(goos string) []string` from `internal/ui/browser.go`.
- Produces: `linkedIssueArgv(goos, ticket, prURL string) []string` — nil when nothing is openable. `spawnDetached(argv []string) error`. Task 3 calls both.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/issuelink_test.go`:

```go
package ui

import (
	"slices"
	"testing"
)

func TestLinkedIssueArgv(t *testing.T) {
	const prURL = "https://github.com/noamsto/prdash/pull/117"
	for _, tc := range []struct {
		name, ticket, prURL string
		want                []string
	}{
		// GitHub id: the owner/repo prefix comes from the PR's own URL, so no
		// repo plumbing is needed.
		{"github", "#213", prURL,
			[]string{"xdg-open", "https://github.com/noamsto/prdash/issues/213"}},
		{"github other repo", "#7", "https://github.com/noamsto/lazytmux/pull/236",
			[]string{"xdg-open", "https://github.com/noamsto/lazytmux/issues/7"}},
		// Linear id: delegate wholesale, so no workspace slug is ever needed.
		{"linear", "ENG-7659", prURL,
			[]string{"linear", "issue", "view", "-w", "ENG-7659"}},
		{"linear short team", "PD-8", prURL,
			[]string{"linear", "issue", "view", "-w", "PD-8"}},
		// Nothing to open.
		{"no ticket", "", prURL, nil},
		// A URL with no /pull/ segment can't locate an issue: the issue board
		// (where O is unbound) and an empty URL both land here.
		{"issue board url", "#213", "https://github.com/noamsto/prdash/issues/133", nil},
		{"empty url", "#213", "", nil},
	} {
		got := linkedIssueArgv("linux", tc.ticket, tc.prURL)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: linkedIssueArgv(%q, %q) = %v, want %v",
				tc.name, tc.ticket, tc.prURL, got, tc.want)
		}
	}
}

func TestLinkedIssueArgvUsesDarwinOpener(t *testing.T) {
	got := linkedIssueArgv("darwin", "#213", "https://github.com/noamsto/prdash/pull/117")
	want := []string{"open", "https://github.com/noamsto/prdash/issues/213"}
	if !slices.Equal(got, want) {
		t.Errorf("linkedIssueArgv(darwin) = %v, want %v", got, want)
	}
}

// The Linear arm must not depend on the host opener: the CLI opens the browser
// itself, so goos is irrelevant there.
func TestLinkedIssueArgvLinearIgnoresGOOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		got := linkedIssueArgv(goos, "ENG-1", "https://github.com/o/r/pull/1")
		want := []string{"linear", "issue", "view", "-w", "ENG-1"}
		if !slices.Equal(got, want) {
			t.Errorf("goos=%s: got %v, want %v", goos, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestLinkedIssueArgv -v`
Expected: FAIL — `undefined: linkedIssueArgv`

- [ ] **Step 3: Write the implementation**

Create `internal/ui/issuelink.go`:

```go
package ui

import "strings"

// linkedIssueArgv is the command that opens the issue a PR row names, or nil
// when there is nothing openable. A GitHub id becomes a URL for the host opener;
// a Linear id is handed to the linear CLI, which knows the workspace this
// machine is authenticated to — the urlKey is not derivable from a branch name.
func linkedIssueArgv(goos, ticket, prURL string) []string {
	if ticket == "" {
		return nil
	}
	if num, ok := strings.CutPrefix(ticket, "#"); ok {
		url, ok := prIssueURL(prURL, num)
		if !ok {
			return nil
		}
		return append(browserArgv(goos), url)
	}
	return []string{"linear", "issue", "view", "-w", ticket}
}

// prIssueURL rewrites a PR URL into a sibling issue URL, so the owner/repo pair
// rides along on the row we already have instead of needing separate plumbing.
func prIssueURL(prURL, num string) (string, bool) {
	base, _, ok := strings.Cut(prURL, "/pull/")
	if !ok {
		return "", false
	}
	return base + "/issues/" + num, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestLinkedIssueArgv -v`
Expected: PASS — all three tests

- [ ] **Step 5: Extract the detached spawn from openURL**

`openURL` already has the exact spawn-and-reap behaviour `O` needs. Pull it out rather than duplicating it. Replace lines 17-28 of `internal/ui/browser.go` with:

```go
// spawnDetached starts argv and returns without waiting. The child is reaped in
// a goroutine so a short-lived opener doesn't linger as a zombie in this
// long-running TUI, and so the UI never blocks on process startup.
func spawnDetached(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// openURL opens url in the default browser.
func openURL(url string) error {
	return spawnDetached(append(browserArgv(runtime.GOOS), url))
}
```

- [ ] **Step 6: Verify the refactor changed nothing**

Run: `gofmt -l internal/ui/ && go test ./internal/ui/`
Expected: no gofmt output, all tests PASS (the existing browser tests still cover `browserArgv`)

- [ ] **Step 7: Commit**

```bash
git add internal/ui/issuelink.go internal/ui/issuelink_test.go internal/ui/browser.go
git commit -m "feat: resolve a PR row's linked issue to an opener argv"
```

---

### Task 2: Carry the ticket id on Vars

**Files:**
- Modify: `internal/action/action.go:9-23`
- Modify: `internal/ui/section.go:244-249`
- Test: `internal/ui/section_test.go` (append)

**Interfaces:**
- Consumes: `ticketID(branch string) string` from `internal/ui/ticket.go`.
- Produces: `action.Vars.Ticket string` — the derived id (`"#213"`, `"ENG-7659"`) or `""`. Task 3 reads it via `m.section.VarsAt(i).Ticket`.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/section_test.go`:

```go
func TestPRSectionVarsAtCarriesTicket(t *testing.T) {
	for _, tc := range []struct{ branch, want string }{
		{"feat/213-id-seed-avatars", "#213"},
		{"eng-7659-must-differ-guard", "ENG-7659"},
		{"agents/no-id-here", ""},
	} {
		s := NewPRSection("is:open")
		s.SetPRs([]gh.PR{{Number: 1, HeadRefName: tc.branch}})
		if got := s.VarsAt(0).Ticket; got != tc.want {
			t.Errorf("branch %q: Ticket = %q, want %q", tc.branch, got, tc.want)
		}
	}
}
```

`SetPRs` is `internal/ui/section.go:80`; `TestBulkWritesPerItem` in `internal/ui/actions_test.go` populates a section the same way.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestPRSectionVarsAtCarriesTicket -v`
Expected: FAIL — `v.Ticket undefined (type action.Vars has no field or method Ticket)`

- [ ] **Step 3: Add the field**

In `internal/action/action.go`, add to the `Vars` struct immediately after the `ID` field:

```go
	// Ticket is the issue this row's branch names — "#213" (a GitHub issue in
	// the same repo) or "ENG-7659" (Linear) — and "" when the branch names
	// none, which is common: agent branches carry no id by construction. Same
	// derivation as the row's ticket column, so `O` and the column can't diverge.
	Ticket string
```

- [ ] **Step 4: Fill it in**

In `internal/ui/section.go`, replace `PRSection.VarsAt` (lines 244-249) with:

```go
func (s *PRSection) VarsAt(i int) action.Vars {
	p := s.prs[s.shown[i]]
	return action.Vars{Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login, Branch: p.HeadRefName,
		ID: p.ID, Ticket: ticketID(p.HeadRefName)}
}
```

`IssueSection.VarsAt` (line 645) is deliberately left alone: a row there *is* the issue, so it has no linked ticket and `O` is unbound on that board.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestPRSectionVarsAtCarriesTicket -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/action/action.go internal/ui/section.go internal/ui/section_test.go
git commit -m "feat: carry the derived ticket id on action.Vars"
```

---

### Task 3: The `O` action and its two hint paths

**Files:**
- Modify: `internal/action/defaults.go:3-44` (the `DefaultPRActions` map)
- Modify: `internal/ui/actions.go` (the `runBulkNative` loop and its empty-calls return)
- Test: `internal/ui/actions_test.go` (append)

**Interfaces:**
- Consumes: `linkedIssueArgv`, `spawnDetached` (Task 1); `action.Vars.Ticket` (Task 2); existing `actionStat`, `clearStatusCmd`, `statForBulk`.
- Produces: the `"O"` key in `DefaultPRActions()` with `Command{Native: "open-issue"}`, `Scope: "per-selected"`. Task 4 renders it.

`O` is `Scope: "per-selected"` to match `o`, so it routes through `runBulkNative` — which today returns `nil` when no call was built. That silent no-op is exactly what the spec forbids, so this task adds a hint alongside the new arm.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/actions_test.go`:

```go
// A branch that names no ticket must settle to a hint, not silently no-op.
func TestOpenIssueNoTicketHints(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "agents/no-id-here",
		URL: "https://github.com/noamsto/prdash/pull/7"}})
	a := action.Action{Key: "O", Label: "Open linked issue",
		Command: action.Command{Native: "open-issue"}, Scope: "per-selected"}

	cmd := m.runBulk(a)
	if m.actionStatus == nil {
		t.Fatal("no ticket must set a status, not leave it nil")
	}
	if !m.actionStatus.settled {
		t.Error("hint status should be settled — nothing is in flight")
	}
	if !strings.Contains(m.actionStatus.ok, "no linked issue") {
		t.Errorf("status = %q, want it to mention \"no linked issue\"", m.actionStatus.ok)
	}
	if cmd == nil {
		t.Error("hint must return a clear-status cmd so it doesn't stick")
	}
}

// A Linear ticket with no linear CLI on PATH must name the missing binary
// rather than opening a URL we never confirmed.
func TestOpenIssueMissingCLIHints(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no linear, no xdg-open
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 8, HeadRefName: "eng-7659-must-differ-guard",
		URL: "https://github.com/noamsto/prdash/pull/8"}})
	a := action.Action{Key: "O", Label: "Open linked issue",
		Command: action.Command{Native: "open-issue"}, Scope: "per-selected"}

	m.runBulk(a)
	if m.actionStatus == nil {
		t.Fatal("missing CLI must set a status")
	}
	if !strings.Contains(m.actionStatus.ok, "linear") {
		t.Errorf("status = %q, want it to name the linear CLI", m.actionStatus.ok)
	}
	if !strings.Contains(m.actionStatus.ok, "ENG-7659") {
		t.Errorf("status = %q, want it to name the ticket", m.actionStatus.ok)
	}
}

func TestDefaultPRActionsHasOpenIssue(t *testing.T) {
	a, ok := action.DefaultPRActions()["O"]
	if !ok {
		t.Fatal("O missing from DefaultPRActions")
	}
	if a.Command.Native != "open-issue" {
		t.Errorf("Native = %q, want open-issue", a.Command.Native)
	}
	if a.Scope != "per-selected" {
		t.Errorf("Scope = %q, want per-selected (matching o)", a.Scope)
	}
	if _, ok := action.DefaultIssueActions()["O"]; ok {
		t.Error("O must not be bound on the issue board — a row there IS the issue")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestOpenIssue|TestDefaultPRActionsHasOpenIssue' -v`
Expected: FAIL — `TestDefaultPRActionsHasOpenIssue` reports "O missing from DefaultPRActions"; the two hint tests fail on a nil `m.actionStatus`.

- [ ] **Step 3: Add the action**

In `internal/action/defaults.go`, add to the `DefaultPRActions()` map immediately after the `"o"` entry:

```go
		// O, not a chord: vim already reads o/O as a paired "open, other
		// target". The Linear arm delegates to the linear CLI, which knows the
		// workspace urlKey — a branch name yields only the team key.
		"O": {Key: "O", Label: "Open linked issue",
			Command: Command{Native: "open-issue"}, Scope: "per-selected"},
```

- [ ] **Step 4: Add the open-issue arm to runBulkNative**

In `internal/ui/actions.go`, inside the `runBulkNative` loop, add this arm immediately after the existing `open-web` arm (which ends `continue`). Declare `var hint string` alongside the existing `var calls []func() error` at the top of the function:

```go
		if a.Command.Native == "open-issue" {
			v := m.section.VarsAt(i)
			argv := linkedIssueArgv(runtime.GOOS, v.Ticket, v.URL)
			if argv == nil {
				hint = "no linked issue"
				continue
			}
			// Pre-flight the opener not for visibility -- spawnDetached does
			// return Start's ENOENT -- but for a message that names the binary,
			// and to keep an unopenable row out of the success count.
			if _, err := exec.LookPath(argv[0]); err != nil {
				hint = fmt.Sprintf("%s not found — can't open %s", argv[0], v.Ticket)
				continue
			}
			calls = append(calls, func() error { return spawnDetached(argv) })
			continue
		}
```

Then replace the loop's existing empty-calls guard:

```go
	if len(calls) == 0 {
		return nil
	}
```

with:

```go
	if len(calls) == 0 {
		if hint != "" {
			m.actionStatus = &actionStat{err: errors.New(hint), fail: hint, settled: true}
			return clearStatusCmd()
		}
		return nil
	}
```

A hint goes in `err`+`fail`, never `ok`. `statusBadge` (`prlist.go:2730`)
renders a settled stat with no error as `✓ <ok>` in pass styling, so putting a
failure in `ok` paints an actionable error green.

Count the skips too, and fold them into the success wording after the existing
`m.actionStatus = statForBulk(a, n)`:

```go
	// A mixed selection partly succeeded: name the remainder, or the skipped
	// rows vanish with the selection that m.sel.clear() is about to consume.
	if skipped > 0 {
		m.actionStatus.ok = fmt.Sprintf("%s · %d skipped", m.actionStatus.ok, skipped)
	}
```

Declare `skipped int` beside `hint` and increment it in both `continue` paths
of the arm. It stays zero for every other native action, so no guard is needed.
Add `"errors"` to the file's imports.

Add `"os/exec"` and `"runtime"` to the file's import block if absent (`fmt` is already imported — `statForBulk` uses it).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ ./internal/action/ -run 'TestOpenIssue|TestDefaultPRActionsHasOpenIssue' -v`
Expected: PASS — all three tests

- [ ] **Step 6: Commit**

```bash
git add internal/action/defaults.go internal/ui/actions.go internal/ui/actions_test.go
git commit -m "feat: open the linked issue with O"
```

---

### Task 4: Surface `O` in the legend, panel, and keymap

**Files:**
- Modify: `internal/ui/prlist.go:2843` (legend) and `:2909` (`actionOrder`)
- Modify: `KEYMAP.md`

**Interfaces:**
- Consumes: the `"O"` entry in `DefaultPRActions()` (Task 3).
- Produces: nothing further; this is the last task.

`actionOrder` drives both the docked panel (`actionHints`) and the height `computeLayout` reserves for it (`defaultActionHints` → `panelContentRows`). Adding an action can therefore change the reserved panel height, so this task ends by running the **whole** suite, not just the ui package.

- [ ] **Step 1: Add O to actionOrder**

In `internal/ui/prlist.go`, replace line 2909:

```go
var actionOrder = []string{"enter", "m", "A", "r", "u", "M", "L", "W", "y", "Y", "b", "o"}
```

with:

```go
var actionOrder = []string{"enter", "m", "A", "r", "u", "M", "L", "W", "y", "Y", "b", "o", "O"}
```

- [ ] **Step 2: Add O to the legend overlay**

In `internal/ui/prlist.go`, the shared `actions` slice at line 2843 covers both boards, so `O` belongs in the PR-only append just below it. Replace:

```go
	if m.mode == "pr" {
		actions = append(actions, keyHint{"m", "merge", nil}, keyHint{"r", "rerun", nil}, keyHint{"u", "update", nil},
			keyHint{"M", "ready", nil}, keyHint{"L", "approve", nil}, keyHint{"X", "cleanup branch", nil})
	}
```

with:

```go
	if m.mode == "pr" {
		actions = append(actions, keyHint{"O", "issue", nil}, keyHint{"m", "merge", nil}, keyHint{"r", "rerun", nil},
			keyHint{"u", "update", nil}, keyHint{"M", "ready", nil}, keyHint{"L", "approve", nil},
			keyHint{"X", "cleanup branch", nil})
	}
```

`O issue` leads the PR-only group so it sits as close as the two-group split allows to the shared `o open` — the pairing is the discoverability.

- [ ] **Step 3: Update KEYMAP.md**

In the "Archetype: Board" section, replace:

```
- Bare letter keys are actions while the box is blurred (e.g. prdash's `m`
  merge / `r` rerun / `o` open / `a` actions palette; wtc's `d` delete / `D`
```

with:

```
- Bare letter keys are actions while the box is blurred (e.g. prdash's `m`
  merge / `r` rerun / `o` open PR / `O` open linked issue / `a` actions
  palette; wtc's `d` delete / `D`
```

- [ ] **Step 4: Run the full suite**

Run: `gofmt -l . && go build ./... && go test ./...`
Expected: no gofmt output, build clean, all packages PASS.

If a layout or panel-geometry test fails (`layout_test.go`, `overflow_test.go`, `gridhints_test.go`), the cause is the extra action changing reserved panel height. Read the failure, confirm the new expected geometry is right, and update the expectation — do not drop `O` from `actionOrder` to make a test green.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go KEYMAP.md
git commit -m "feat: show O in the legend, panel, and keymap"
```

- [ ] **Step 6: Verify by hand**

The automated tests deliberately never spawn a browser, so the happy paths are unverified until now. Run `go run .` in a repo with open PRs and check:

1. A PR whose branch is `feat/<n>-...` → `O` opens `github.com/<owner>/<repo>/issues/<n>`.
2. A PR whose branch is `<team>-<n>-...` → `O` opens the right Linear ticket.
3. A PR whose branch names no ticket → status reads `no linked issue`, nothing opens.
4. `o` still opens the PR itself, and `O issue` appears next to `o open` in the footer and in `?`.

Note: prdash's `prefix+p` tmux popup has a known clipboard limitation on tmux 3.6, but browser opening is unaffected — either launch path is fine for this check.

- [ ] **Step 7: Ship it**

Push the branch and open a PR assigned to yourself, titled `feat: open the linked issue with \`O\``, with a body that closes #133 and covers:

- GitHub ids (`#213`) build a sibling issue URL from the PR's own URL — no repo plumbing.
- Linear ids (`ENG-7659`) delegate to `linear issue view -w`, so no workspace slug, no config, and no `LINEAR_API_KEY`.
- Both failure modes settle to a status hint: no ticket parsed, or the opener binary absent.
- A link to `docs/superpowers/specs/2026-09-09-open-linked-issue-design.md`.

Run the `deslop` skill before pushing — the repo's pre-push hook requires it.
