# prdash Plan 2 — Actions + action view + tmux handoff

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Config-driven actions on the selected PR — inline verbs (merge/rerun/copy/browser) run in-process; worktree/checkout/diff verbs hand off to a tmux orchestrator that runs them in the parent session.

**Architecture:** An `Action` has a command in one of three forms (`argv` templated / `builtin` / `shell`), plus `exits-tui`/`scope`/`confirm`. Inline actions run via the `gh.Runner`. `exits-tui` actions append a line to `$PRDASH_ACTION_FILE` and quit; a tmux keybinding then runs `prdash-apply` via `run-shell` in the session. An action view (`a`) lists actions fuzzily.

**Tech Stack:** Go 1.23, bubbletea v1, `sahilm/fuzzy`, `text/template`, stdlib. Spec: `2026-06-22-prdash-design.md`. Builds on Plan 1.

---

## File structure
- `internal/action/action.go` — `Action`, `Vars`, template expansion
- `internal/action/builtin.go` — `rerun-failed`, `copy` (OSC52)
- `internal/action/defaults.go` — the default action set
- `internal/action/handoff.go` — append exits-tui actions to the handoff file
- `internal/ui/actions.go` — dispatch keys → actions; result messages
- `internal/ui/actionview.go` — the `a` overlay (fuzzy action list)
- `nix/` + lazytmux changes — tmux binding + `prdash-apply` orchestrator (integration)

---

## Task 1: Action model + template expansion

**Files:** Create `internal/action/action.go`, `internal/action/action_test.go`

- [ ] **Step 1: Failing test**

```go
package action

import "testing"

func TestExpandArgv(t *testing.T) {
	a := Action{Key: "enter", Command: Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}}
	got, err := a.ExpandArgv(Vars{Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wt", "switch", "pr:7"}
	if len(got) != 3 || got[2] != want[2] {
		t.Fatalf("got %v", got)
	}
}

func TestExpandUsesBranch(t *testing.T) {
	a := Action{Command: Command{Argv: []string{"wt", "switch", "-c", "{{.Branch}}"}}}
	got, _ := a.ExpandArgv(Vars{Branch: "feat/213-x"})
	if got[3] != "feat/213-x" {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./internal/action/` → undefined.

- [ ] **Step 3: Implement `internal/action/action.go`**

```go
package action

import (
	"bytes"
	"text/template"
)

// Vars are the per-item template values. Built from a gh.PR / gh.Issue by the UI.
type Vars struct {
	Number      int
	Title       string
	HeadRefName string
	BaseRefName string
	URL         string
	Repo        string
	Author      string
	Branch      string // derived branch (issue) or HeadRefName (PR)
}

type Command struct {
	Argv    []string // templated, exec'd directly (no shell) — injection-safe
	Builtin string   // e.g. "rerun-failed", "copy"
	Shell   string   // opt-in: run through `sh -c` (user actions only)
}

type Action struct {
	Key      string
	Label    string
	Command  Command
	ExitsTUI bool
	Scope    string // "single" | "per-selected"
	Confirm  bool
}

// ExpandArgv renders each argv element as a template against v.
func (a Action) ExpandArgv(v Vars) ([]string, error) {
	out := make([]string, 0, len(a.Command.Argv))
	for _, raw := range a.Command.Argv {
		t, err := template.New("a").Parse(raw)
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, v); err != nil {
			return nil, err
		}
		out = append(out, b.String())
	}
	return out, nil
}
```

- [ ] **Step 4: Run, verify pass** — `go test ./internal/action/ -v`.

- [ ] **Step 5: Commit** — `git commit -m "feat(action): action model + argv template expansion"`.

---

## Task 2: Built-in `copy` (OSC52)

**Files:** Create `internal/action/builtin.go`, add to `internal/action/builtin_test.go`

- [ ] **Step 1: Failing test**

```go
package action

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52(t *testing.T) {
	seq := OSC52("feat/x")
	want := base64.StdEncoding.EncodeToString([]byte("feat/x"))
	if !strings.Contains(seq, want) {
		t.Fatalf("osc52 %q missing base64 %q", seq, want)
	}
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") {
		t.Fatalf("osc52 envelope wrong: %q", seq)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement in `internal/action/builtin.go`**

```go
package action

import "encoding/base64"

// OSC52 returns the terminal escape that copies s to the system clipboard.
// Survives the tmux popup / SSH when the terminal has clipboard passthrough.
func OSC52(s string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07"
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(action): OSC52 clipboard copy`.

---

## Task 3: Built-in `rerun-failed` (run-id resolution)

**Files:** Modify `internal/action/builtin.go`, `internal/action/builtin_test.go`

- [ ] **Step 1: Failing test (fake runner, two calls)**

```go
type seqRunner struct {
	calls [][]string
	outs  [][]byte
	i     int
}

func (r *seqRunner) Run(_ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	o := r.outs[r.i]
	r.i++
	return o, nil
}

func TestRerunFailedResolvesRunID(t *testing.T) {
	r := &seqRunner{outs: [][]byte{
		[]byte(`[{"databaseId":555}]`), // gh run list
		[]byte(``),                     // gh run rerun
	}}
	err := RerunFailed(r, "/repo", "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if r.calls[0][0] != "run" || r.calls[0][1] != "list" {
		t.Fatalf("first call not run list: %v", r.calls[0])
	}
	last := r.calls[1]
	if last[0] != "run" || last[1] != "rerun" || last[2] != "555" {
		t.Fatalf("rerun call wrong: %v", last)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement (uses `gh.Runner`)**

```go
// in internal/action/builtin.go
import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/noamsto/prdash/internal/gh"
)

func RerunFailed(r gh.Runner, dir, branch string) error {
	out, err := r.Run(dir, "run", "list", "--branch", branch, "-L", "1", "--json", "databaseId")
	if err != nil {
		return err
	}
	var runs []struct{ DatabaseID int `json:"databaseId"` }
	if err := json.Unmarshal(out, &runs); err != nil {
		return err
	}
	if len(runs) == 0 {
		return fmt.Errorf("no runs for branch %s", branch)
	}
	_, err = r.Run(dir, "run", "rerun", strconv.Itoa(runs[0].DatabaseID), "--failed")
	return err
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(action): rerun-failed with run-id resolution`.

---

## Task 4: Handoff file (exits-tui actions)

**Files:** Create `internal/action/handoff.go`, `internal/action/handoff_test.go`

- [ ] **Step 1: Failing test**

```go
package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendHandoff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	if err := AppendHandoff(p, "enter", []string{"wt", "switch", "pr:7"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHandoff(p, "enter", []string{"wt", "switch", "pr:9"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), b)
	}
	if !strings.HasPrefix(lines[0], "enter\t[") {
		t.Fatalf("line format: %q", lines[0])
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement**

```go
package action

import (
	"encoding/json"
	"os"
)

// AppendHandoff appends one "<key>\t<argv-json>" line to the handoff file the
// tmux orchestrator reads after the popup closes.
func AppendHandoff(path, key string, argv []string) error {
	j, err := json.Marshal(argv)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(key + "\t" + string(j) + "\n")
	return err
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(action): handoff-file append for exits-tui actions`.

---

## Task 5: Default actions

**Files:** Create `internal/action/defaults.go`, `internal/action/defaults_test.go`

- [ ] **Step 1: Failing test**

```go
package action

import "testing"

func TestDefaultsHaveEnterAndExits(t *testing.T) {
	d := DefaultPRActions()
	enter, ok := d["enter"]
	if !ok || !enter.ExitsTUI {
		t.Fatal("enter must exist and exit the TUI")
	}
	if m := d["m"]; m.ExitsTUI || !m.Confirm {
		t.Fatal("merge must be inline + confirm")
	}
	if d["r"].Command.Builtin != "rerun-failed" {
		t.Fatal("r must be the rerun-failed builtin")
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement**

```go
package action

func DefaultPRActions() map[string]Action {
	return map[string]Action{
		"enter": {Key: "enter", Label: "Open worktree",
			Command: Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}},
			ExitsTUI: true, Scope: "single"},
		"m": {Key: "m", Label: "Merge (squash)",
			Command: Command{Argv: []string{"gh", "pr", "merge", "{{.Number}}", "--squash"}},
			Confirm: true, Scope: "single"},
		"r": {Key: "r", Label: "Rerun failed",
			Command: Command{Builtin: "rerun-failed"}, Scope: "single"},
		"y": {Key: "y", Label: "Copy branch",
			Command: Command{Builtin: "copy"}, Scope: "single"},
		"o": {Key: "o", Label: "Open in browser",
			Command: Command{Argv: []string{"gh", "pr", "view", "{{.Number}}", "--web"}}, Scope: "single"},
	}
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(action): default PR action set`.

---

## Task 6: Dispatch actions in the UI

**Files:** Modify `internal/ui/prlist.go`; Create `internal/ui/actions.go`, `internal/ui/actions_test.go`

- [ ] **Step 1: Failing test (inline action runs via runner; exits-tui writes handoff)**

```go
package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func TestRunActionExitsTUIWritesHandoff(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil)
	m.setPRs([]gh.PR{{Number: 7, HeadRefName: "feat/x"}})
	a := action.Action{Key: "enter", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true}

	quit := m.runAction(a)
	if quit == nil {
		t.Fatal("exits-tui action must return tea.Quit")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("handoff file not written: %v", err)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/ui/actions.go`**

```go
package ui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
)

func (m *Model) varsFor(p gh.PR) action.Vars {
	return action.Vars{
		Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login,
		Repo: m.repo, Branch: p.HeadRefName,
	}
}

// runAction executes a single-scope action against the cursor PR. exits-tui
// actions write the handoff file and quit; inline actions run via the runner.
func (m *Model) runAction(a action.Action) tea.Cmd {
	cur := m.cursorPR()
	if cur == nil {
		return nil
	}
	v := m.varsFor(*cur)

	if a.ExitsTUI {
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		if path := os.Getenv("PRDASH_ACTION_FILE"); path != "" {
			_ = action.AppendHandoff(path, a.Key, argv)
		}
		return tea.Quit
	}

	switch a.Command.Builtin {
	case "copy":
		return func() tea.Msg { print(action.OSC52(v.Branch)); return nil }
	case "rerun-failed":
		r := m.runner
		dir, branch := m.dir, v.HeadRefName
		return func() tea.Msg {
			if err := action.RerunFailed(r, dir, branch); err != nil {
				return fetchFailedMsg{err}
			}
			return nil
		}
	default: // argv (e.g. gh pr merge)
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			return nil
		}
		r := m.runner
		dir := m.dir
		return func() tea.Msg {
			if _, err := r.Run(dir, argv[1:]...); err != nil { // argv[0]=="gh"
				return fetchFailedMsg{err}
			}
			return nil
		}
	}
}
```

Add `repo string` to `Model` (set in `main` from `gh.CurrentRepo`), a `cursorPR()` helper returning the PR under the table cursor (map `m.table.Cursor()` into the *currently shown* slice — track `m.shown []gh.PR` set in `applyFilter`), and wire default-action keys in `Update`'s non-filter `tea.KeyMsg` switch:

```go
		default:
			if a, ok := m.actions[msg.String()]; ok {
				if a.Confirm {
					m.pending = &a
					return m, nil // confirm handled in Task 8
				}
				return m, m.runAction(a)
			}
```

Add `actions map[string]action.Action` (from `action.DefaultPRActions()`) and `shown []gh.PR` to the model; populate `m.shown` inside `applyFilter`.

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(ui): dispatch inline + exits-tui actions`.

---

## Task 7: Action view (`a` overlay)

**Files:** Create `internal/ui/actionview.go`, `internal/ui/actionview_test.go`; modify `prlist.go`

- [ ] **Step 1: Failing test (fuzzy over action labels/keys)**

```go
package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/action"
)

func TestFilterActions(t *testing.T) {
	acts := action.DefaultPRActions()
	got := filterActions(acts, "merge")
	if len(got) == 0 || got[0].Key != "m" {
		t.Fatalf("merge query = %+v", got)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/ui/actionview.go`**

```go
package ui

import (
	"sort"

	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/action"
)

// filterActions fuzzy-matches "key label" haystacks; empty query → all (sorted by key).
func filterActions(acts map[string]action.Action, query string) []action.Action {
	list := make([]action.Action, 0, len(acts))
	for _, a := range acts {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	if query == "" {
		return list
	}
	hay := make([]string, len(list))
	for i, a := range list {
		hay[i] = a.Key + " " + a.Label
	}
	matches := fuzzy.Find(query, hay)
	out := make([]action.Action, 0, len(matches))
	for _, mch := range matches {
		out = append(out, list[mch.Index])
	}
	return out
}
```

- [ ] **Step 4: Run, verify pass.**

- [ ] **Step 5: Wire `a` to open the overlay**

In `prlist.go`: add `showActions bool` + an `actionFilter textinput.Model`. On `a` (non-filter mode) set `showActions=true`. While `showActions`, render `filterActions(...)` as a list; `enter` runs the highlighted action via `runAction`; `esc` closes. (Mirror the `/` filter-mode plumbing from Plan 1 Task 6.)

- [ ] **Step 6: Commit** — `feat(ui): action view overlay (fuzzy)`.

---

## Task 8: Confirm prompt (merge, default No)

**Files:** Modify `internal/ui/prlist.go`, `internal/ui/actions_test.go`

- [ ] **Step 1: Failing test**

```go
func TestConfirmDefaultNoCancels(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	a := action.Action{Key: "m", Confirm: true}
	m.pending = &a
	m.confirmAnswer(false) // default No
	if m.pending != nil {
		t.Fatal("pending should clear on No")
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement**

```go
// in actions.go
func (m *Model) confirmAnswer(yes bool) tea.Cmd {
	a := m.pending
	m.pending = nil
	if !yes || a == nil {
		return nil
	}
	return m.runAction(*a)
}
```

Wire into `Update`: when `m.pending != nil`, intercept keys — `y` → `confirmAnswer(true)`, anything else (incl. `enter`/`n`/`esc`) → `confirmAnswer(false)`. Render a `"Merge #N? y/N"` line in `View` when pending.

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(ui): destructive-action confirm (default No)`.

---

## Task 9: tmux binding + `prdash-apply` orchestrator (lazytmux integration)

**Files:** `lazytmux/scripts/prdash-apply.sh`, `lazytmux/config/tmux.conf.nix` (binding)

This runs in the **session** context after the popup closes — never inside the popup.

- [ ] **Step 1: Write `prdash-apply.sh`**

```bash
#!/usr/bin/env bash
# Reads the prdash handoff file and creates a worktree + window per line.
set -euo pipefail
file="${1:?handoff file path required}"
[[ -f $file ]] || exit 0
WT="@wt@"

while IFS=$'\t' read -r key argv_json; do
	[[ -n ${argv_json:-} ]] || continue
	# argv_json is a JSON array like ["wt","switch","pr:7"] or [...,"-c","feat/213-x"]
	mapfile -t argv < <(printf '%s' "$argv_json" | jq -r '.[]')
	"$WT" "${argv[@]:1}" # argv[0] is "wt"; post-switch hook makes the window
done <"$file"
rm -f "$file"
```

- [ ] **Step 2: Shellcheck**

Run: `shellcheck lazytmux/scripts/prdash-apply.sh`
Expected: clean (fix any findings).

- [ ] **Step 3: tmux binding (two sequential commands)**

In `tmux.conf.nix`, replace the single `display-popup` for prdash with:

```
bind-key "G" {
  display-popup -E -w 90% -h 90% -d '#{pane_current_path}' \
    -e PRDASH_ACTION_FILE=/tmp/prdash-#{pane_id} ${prdash}/bin/prdash
  run-shell -b "${prdash-apply}/bin/prdash-apply /tmp/prdash-#{pane_id}"
}
```

(`${prdash}` and `${prdash-apply}` are nix-provided store paths, like the existing `tmux-gh-dash` wiring. `@wt@` is substituted to the worktrunk binary.)

- [ ] **Step 4: Manual integration test**

Rebuild Home Manager; `prefix+G`; press `enter` on a PR → popup closes → a new tmux window for that PR's worktree appears in the session; re-running on the same PR reuses (no error — idempotency lands in Plan 3's `prdash-apply` hardening).

- [ ] **Step 5: Commit (in lazytmux repo)** — `feat: prdash popup binding + worktree orchestrator`.

---

## Self-review
- **Spec coverage:** action model (argv/builtin/shell) ✓ T1, OSC52 copy ✓ T2, rerun run-id ✓ T3, handoff ✓ T4, defaults ✓ T5, dispatch + exits-tui ✓ T6, action view ✓ T7, confirm(No) ✓ T8, tmux run-shell orchestrator (not inside popup) ✓ T9. Idempotent worktree reuse hardened in Plan 3.
- **Types:** `action.Action/Command/Vars/ExpandArgv/AppendHandoff/OSC52/RerunFailed/DefaultPRActions`, `ui.runAction/varsFor/cursorPR/confirmAnswer/filterActions` consistent.
- **Placeholders:** none.
