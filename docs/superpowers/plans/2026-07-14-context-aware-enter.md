# Context-aware Enter + in-app check log viewer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Enter drill into the focused entity — a worktree for a PR, the job logs for a highlighted check — and give the Checks tab an in-app, theme-colored log viewer with copy.

**Architecture:** The expanded view already tracks a hovered check (`m.checkCursor`) on the Checks tab (`expandedTab == 2`). We add a log sub-view (`m.logView`) layered over it: Enter on a check fetches its job log via `gh run view --job <id> --log[-failed]` (async, cached per job+variant), parses the tab-delimited output into steps, and renders it in a bordered box that reuses the existing viewport. A single line cursor drives three copy targets. The Checks tab's `o`/`Y` keys retarget to the hovered check's `DetailsUrl`.

**Tech Stack:** Go, `charm.land/bubbletea/v2` (Elm-style Update/View), `charm.land/lipgloss/v2`, `gh` CLI via the `gh.Runner` seam.

## Global Constraints

- All `gh` invocations go through `gh.Runner.Run(dir string, args ...string) ([]byte, error)` — never `exec.Command("gh", …)` directly. The one exception is the browser opener (`openURL`), which execs the OS opener, not `gh`.
- New pure helpers get unit tests; stateful wiring is tested by driving `Model.Update`/`updateExpanded` with `NewModel(...)` + `setPRs(...)` and asserting state, following the existing `internal/ui/*_test.go` style (`recordRunner`/`stubRunner` for the runner seam).
- Style with the existing theme styles (`dimStyle`, `passStyle`, `failStyle`, `focusBarStyle`, `titleStyle`, `accentStyle`); never hardcode colors.
- Copy uses the established path: prefer a native clipboard tool (`clipboardArgv()` + `writeClipboard`), fall back to `tea.SetClipboard` (OSC 52). See the comment in `runAction` re: tmux 3.6.
- No forward references between tasks: each task's code compiles and tests pass on its own.
- Commit after each task with a conventional-commit message.

---

### Task 1: `action.JobLog` — fetch a job's log via gh

**Files:**
- Modify: `internal/action/builtin.go`
- Test: `internal/action/builtin_test.go` (create if absent)

**Interfaces:**
- Produces: `func JobLog(r gh.Runner, dir, jobID string, failedOnly bool) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

Add to `internal/action/builtin_test.go`:

```go
package action

import (
	"reflect"
	"testing"
)

type argRunner struct{ args []string }

func (r *argRunner) Run(_ string, args ...string) ([]byte, error) {
	r.args = args
	return []byte("log-bytes"), nil
}

func TestJobLogArgs(t *testing.T) {
	r := &argRunner{}
	out, err := JobLog(r, "/repo", "123", true)
	if err != nil {
		t.Fatalf("JobLog: %v", err)
	}
	if string(out) != "log-bytes" {
		t.Fatalf("out = %q", out)
	}
	want := []string{"run", "view", "--job", "123", "--log-failed"}
	if !reflect.DeepEqual(r.args, want) {
		t.Fatalf("failedOnly args = %v, want %v", r.args, want)
	}

	r2 := &argRunner{}
	JobLog(r2, "/repo", "123", false)
	wantAll := []string{"run", "view", "--job", "123", "--log"}
	if !reflect.DeepEqual(r2.args, wantAll) {
		t.Fatalf("full args = %v, want %v", r2.args, wantAll)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/action && go test -run TestJobLogArgs ./...`
Expected: FAIL — `undefined: JobLog`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/action/builtin.go`:

```go
// JobLog fetches one Actions job's log. failedOnly limits it to failed steps
// (gh --log-failed); otherwise the whole job log.
func JobLog(r gh.Runner, dir, jobID string, failedOnly bool) ([]byte, error) {
	flag := "--log"
	if failedOnly {
		flag = "--log-failed"
	}
	return r.Run(dir, "run", "view", "--job", jobID, flag)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/action && go test -run TestJobLogArgs ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/action/builtin.go internal/action/builtin_test.go
git commit -m "feat(action): JobLog fetches an Actions job log via gh"
```

---

### Task 2: `parseJobLog` — parse gh log output into steps

**Files:**
- Create: `internal/ui/logview.go`
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Produces:
  - `type logStep struct { name string; failed bool; lines []string }`
  - `func parseJobLog(raw []byte, failedOnly bool) []logStep`
  - `func stripTimestamp(s string) string`

**Background — gh log format:** `gh run view --log[-failed]` prints one line per log
line, tab-delimited as `job⇥step⇥<RFC3339 timestamp> <content>`. We group by the
step field (2nd column), strip the leading timestamp from the content, and
preserve step order. In `--log-failed` mode every returned step is a failed step,
so `failedOnly` sets `failed` on each.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/logview_test.go`:

```go
package ui

import (
	"reflect"
	"testing"
)

func TestStripTimestamp(t *testing.T) {
	in := "2024-01-02T03:04:05.1234567Z hello world"
	if got := stripTimestamp(in); got != "hello world" {
		t.Fatalf("stripTimestamp = %q", got)
	}
	if got := stripTimestamp("no timestamp here"); got != "no timestamp here" {
		t.Fatalf("non-timestamp line altered: %q", got)
	}
}

func TestParseJobLogGroupsByStep(t *testing.T) {
	raw := []byte(
		"build\tRun tests\t2024-01-02T03:04:05.0Z FAIL foo_test.go:42\n" +
			"build\tRun tests\t2024-01-02T03:04:06.0Z expected 3 got 4\n" +
			"build\tSet up job\t2024-01-02T03:04:00.0Z Requested labels\n")
	steps := parseJobLog(raw, true)
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].name != "Run tests" || !steps[0].failed {
		t.Fatalf("step 0 = %+v", steps[0])
	}
	wantLines := []string{"FAIL foo_test.go:42", "expected 3 got 4"}
	if !reflect.DeepEqual(steps[0].lines, wantLines) {
		t.Fatalf("step 0 lines = %v, want %v", steps[0].lines, wantLines)
	}
	if steps[1].name != "Set up job" {
		t.Fatalf("step 1 name = %q", steps[1].name)
	}
}

func TestParseJobLogFullNotFailed(t *testing.T) {
	raw := []byte("build\tSet up job\t2024-01-02T03:04:00.0Z ok\n")
	if steps := parseJobLog(raw, false); steps[0].failed {
		t.Fatal("full-log steps should not be marked failed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestStripTimestamp|TestParseJobLog' ./...`
Expected: FAIL — `undefined: stripTimestamp` / `parseJobLog`

- [ ] **Step 3: Write minimal implementation**

Create `internal/ui/logview.go`:

```go
package ui

import "strings"

// logStep is one Actions step's log: its name, whether it's a failed step
// (true for every step in --log-failed output), and its content lines.
type logStep struct {
	name   string
	failed bool
	lines  []string
}

// parseJobLog turns `gh run view --log[-failed]` output into ordered steps.
// The output is tab-delimited as job⇥step⇥<timestamp> <content>; we group by the
// step column, preserving first-seen order, and strip the leading timestamp.
func parseJobLog(raw []byte, failedOnly bool) []logStep {
	var steps []logStep
	idx := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		step, content := "", line
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
			step = parts[1]
			content = stripTimestamp(parts[2])
		}
		i, seen := idx[step]
		if !seen {
			i = len(steps)
			idx[step] = i
			steps = append(steps, logStep{name: step, failed: failedOnly})
		}
		steps[i].lines = append(steps[i].lines, content)
	}
	return steps
}

// stripTimestamp drops gh's leading RFC3339 timestamp ("2024-01-02T03:04:05Z ")
// from a log line, leaving the message. Lines without one are returned as-is.
func stripTimestamp(s string) string {
	i := strings.IndexByte(s, ' ')
	if i < 20 || s[4] != '-' || s[10] != 'T' {
		return s
	}
	return s[i+1:]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestStripTimestamp|TestParseJobLog' ./...`
Expected: PASS

- [ ] **Step 5: Verify the real gh format matches (adjust fixture if not)**

The parser assumes `job⇥step⇥timestamp content`. Confirm against a real failing run
before trusting it. In a repo with a failed check:

```bash
gh run view --job <a-real-job-id> --log-failed | head -3 | cat -A | head
```

Expected: each line shows two `^I` (tab) separators, then an RFC3339 timestamp and
the message. If the shape differs, update the fixture in Task 2's tests and the
`SplitN`/`stripTimestamp` logic to match, then re-run Step 4.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/logview.go internal/ui/logview_test.go
git commit -m "feat(ui): parse gh job-log output into steps"
```

---

### Task 3: Log data helpers — flatten, classify, copy

**Files:**
- Modify: `internal/ui/logview.go`
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Consumes: `logStep` (Task 2)
- Produces:
  - `type logLine struct { text string; step int; header bool }`
  - `func flattenLog(steps []logStep) []logLine`
  - `type lineKind int` with `kindPlain, kindError, kindPass`
  - `func classifyLogLine(text string) lineKind`
  - `func copyLine(lines []logLine, cursor int) string`
  - `func copyStep(steps []logStep, lines []logLine, cursor int) string`
  - `func copyWhole(steps []logStep) string`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/logview_test.go`:

```go
func TestFlattenLog(t *testing.T) {
	steps := []logStep{
		{name: "A", lines: []string{"a1", "a2"}},
		{name: "B", lines: []string{"b1"}},
	}
	lines := flattenLog(steps)
	// header A, a1, a2, header B, b1
	if len(lines) != 5 {
		t.Fatalf("want 5 flat lines, got %d", len(lines))
	}
	if !lines[0].header || lines[0].text != "A" || lines[0].step != 0 {
		t.Fatalf("line 0 = %+v", lines[0])
	}
	if lines[2].header || lines[2].text != "a2" || lines[2].step != 0 {
		t.Fatalf("line 2 = %+v", lines[2])
	}
	if lines[4].step != 1 || lines[4].text != "b1" {
		t.Fatalf("line 4 = %+v", lines[4])
	}
}

func TestClassifyLogLine(t *testing.T) {
	cases := map[string]lineKind{
		"FAIL foo_test.go":  kindError,
		"an error occurred": kindError,
		"all tests passed":  kindPass,
		"ok  	pkg  0.1s":    kindPass,
		"compiling main.go": kindPlain,
	}
	for in, want := range cases {
		if got := classifyLogLine(in); got != want {
			t.Errorf("classifyLogLine(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCopyHelpers(t *testing.T) {
	steps := []logStep{{name: "Run tests", lines: []string{"x", "y"}}}
	lines := flattenLog(steps) // [header, x, y]
	if got := copyLine(lines, 1); got != "x" {
		t.Fatalf("copyLine = %q", got)
	}
	if got := copyStep(steps, lines, 2); got != "Run tests\nx\ny" {
		t.Fatalf("copyStep = %q", got)
	}
	if got := copyWhole(steps); got != "Run tests\nx\ny" {
		t.Fatalf("copyWhole = %q", got)
	}
	if copyLine(lines, 99) != "" {
		t.Fatal("out-of-range copyLine should be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestFlattenLog|TestClassifyLogLine|TestCopyHelpers' ./...`
Expected: FAIL — `undefined: flattenLog` etc.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/ui/logview.go`:

```go
// logLine is one rendered/navigable line: either a step header or a content
// line. step indexes into the []logStep it came from (headers included), so copy
// can target the whole step from any line within it.
type logLine struct {
	text   string
	step   int
	header bool
}

func flattenLog(steps []logStep) []logLine {
	var out []logLine
	for i, s := range steps {
		out = append(out, logLine{text: s.name, step: i, header: true})
		for _, ln := range s.lines {
			out = append(out, logLine{text: ln, step: i})
		}
	}
	return out
}

type lineKind int

const (
	kindPlain lineKind = iota
	kindError
	kindPass
)

// classifyLogLine buckets a content line so the renderer can color it. Errors
// win over passes when a line somehow matches both.
func classifyLogLine(text string) lineKind {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, "error") || strings.Contains(l, "fail") || strings.Contains(text, "✗"):
		return kindError
	case strings.Contains(l, "pass") || strings.Contains(text, "✓") || strings.HasPrefix(l, "ok"):
		return kindPass
	default:
		return kindPlain
	}
}

func copyLine(lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	return lines[cursor].text
}

func copyStep(steps []logStep, lines []logLine, cursor int) string {
	if cursor < 0 || cursor >= len(lines) {
		return ""
	}
	s := steps[lines[cursor].step]
	return s.name + "\n" + strings.Join(s.lines, "\n")
}

func copyWhole(steps []logStep) string {
	var parts []string
	for _, s := range steps {
		parts = append(parts, s.name+"\n"+strings.Join(s.lines, "\n"))
	}
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestFlattenLog|TestClassifyLogLine|TestCopyHelpers' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/logview.go internal/ui/logview_test.go
git commit -m "feat(ui): log-view flatten, classify, and copy helpers"
```

---

### Task 4: Model state, entering the log view, async fetch + cache

**Files:**
- Modify: `internal/ui/prlist.go` (Model struct + `NewModel` + `logFetchedMsg` handling in `Update`)
- Modify: `internal/ui/messages.go` (new message)
- Modify: `internal/ui/logview.go` (state helpers, enter, fetch cmd)
- Modify: `internal/ui/expanded.go` (`enter` case on Checks tab)
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Consumes: `action.JobLog` (T1), `parseJobLog`/`flattenLog` (T2/T3)
- Produces:
  - Model fields: `logView bool`, `logJobID string`, `logLabel string`, `logShowAll bool`, `logLoading bool`, `logErr error`, `logSteps []logStep`, `logLines []logLine`, `logCursor int`, `logCache map[string][]logStep`
  - `type logFetchedMsg struct { job string; all bool; raw []byte; err error }`
  - `func (m Model) fetchJobLogCmd(job string, all bool) tea.Cmd`
  - `func (m *Model) setLogSteps(steps []logStep)`
  - `func logCacheKey(job string, all bool) string`
  - `func (m Model) enterLogView() (tea.Model, tea.Cmd)`
  - `func (m Model) hoveredCheck() (gh.Check, bool)`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/logview_test.go` (add imports `tea "charm.land/bubbletea/v2"` and `"github.com/noamsto/prdash/internal/gh"`). Keys use `tea.KeyPressMsg` — the constructor the suite already uses (`tea.KeyPressMsg{Code: 'j', Text: "j"}` for character keys, `{Code: tea.KeyEnter}` / `{Code: tea.KeyEscape}` for special keys); `updateExpanded`/`updateLogView` take `tea.KeyMsg`, which `tea.KeyPressMsg` satisfies:

```go
func logViewModel(t *testing.T) Model {
	t.Helper()
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("x")
	m.SetRunner(stubRunner{})
	m.width, m.height = 120, 40
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "test", DetailsUrl: "https://github.com/x/actions/runs/1/job/99"},
	}}})
	m.expanded = true
	m.expandedTab = 2 // Checks
	m.checkCursor = 0
	m.renderExpanded()
	return m
}

func TestEnterOpensLogView(t *testing.T) {
	m := logViewModel(t)
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if !m.logView {
		t.Fatal("enter on a check with a job ID should open the log view")
	}
	if m.logJobID != "99" || !m.logLoading {
		t.Fatalf("logJobID=%q loading=%v", m.logJobID, m.logLoading)
	}
	if cmd == nil {
		t.Fatal("expected a fetch command")
	}
}

func TestLogFetchedPopulatesSteps(t *testing.T) {
	m := logViewModel(t)
	u, _ := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	raw := []byte("build\tRun tests\t2024-01-02T03:04:05Z FAIL x\n")
	u, _ = m.Update(logFetchedMsg{job: "99", all: false, raw: raw})
	m = u.(Model)
	if m.logLoading {
		t.Fatal("loading should clear on fetch")
	}
	if len(m.logSteps) != 1 || m.logSteps[0].name != "Run tests" {
		t.Fatalf("steps = %+v", m.logSteps)
	}
	if _, ok := m.logCache[logCacheKey("99", false)]; !ok {
		t.Fatal("fetched log should be cached")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestEnterOpensLogView|TestLogFetched' ./...`
Expected: FAIL — `m.logView undefined` / `undefined: logFetchedMsg`

- [ ] **Step 3a: Add Model fields + NewModel init**

In `internal/ui/prlist.go`, add to the `Model` struct (near `checkCursor`):

```go
	logView      bool   // the check-log sub-view is active (over the Checks tab)
	logJobID     string // Actions job ID whose log is shown
	logLabel     string // hovered check's label, for the log box title
	logShowAll   bool   // full job log vs failed-steps-only
	logLoading   bool   // a log fetch is in flight
	logErr       error  // last log fetch error
	logSteps     []logStep
	logLines     []logLine
	logCursor    int                 // line cursor within logLines
	logCache     map[string][]logStep // keyed by logCacheKey(job, all)
```

In `NewModel`'s returned `Model{...}` literal, add:

```go
		logCache: map[string][]logStep{},
```

- [ ] **Step 3b: Add the message**

In `internal/ui/messages.go`:

```go
// logFetchedMsg carries a fetched job log back to the log sub-view. all
// distinguishes the full-log variant from failed-only so a stale in-flight
// fetch for the other variant is ignored.
type logFetchedMsg struct {
	job string
	all bool
	raw []byte
	err error
}
```

- [ ] **Step 3c: Add state helpers + enter + fetch (logview.go)**

Append to `internal/ui/logview.go` (add imports `fmt`, `tea "charm.land/bubbletea/v2"`, `"github.com/noamsto/prdash/internal/action"`, `"github.com/noamsto/prdash/internal/gh"`):

```go
func logCacheKey(job string, all bool) string { return fmt.Sprintf("%s|%t", job, all) }

// hoveredCheck returns the check under the Checks-tab cursor.
func (m Model) hoveredCheck() (gh.Check, bool) {
	ps, ok := m.section.(*PRSection)
	if !ok {
		return gh.Check{}, false
	}
	checks := ps.prAt(m.cursor).Checks()
	if m.checkCursor < 0 || m.checkCursor >= len(checks) {
		return gh.Check{}, false
	}
	return checks[m.checkCursor], true
}

// enterLogView opens the log sub-view for the hovered check. A cached log paints
// instantly; otherwise it kicks an async fetch. External checks (no job ID) get
// a notice here — Task 7 upgrades that to open the browser.
func (m Model) enterLogView() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok {
		return m, nil
	}
	job := c.JobID()
	if job == "" {
		m.actionStatus = &actionStat{fail: "external check — no job logs", settled: true,
			err: fmt.Errorf("external check %q has no job logs", c.Label())}
		return m, clearStatusCmd()
	}
	m.logView = true
	m.logJobID = job
	m.logLabel = c.Label()
	m.logCursor = 0
	m.logShowAll = false
	m.logErr = nil
	if steps, hit := m.logCache[logCacheKey(job, false)]; hit {
		m.logLoading = false
		m.setLogSteps(steps)
		return m, nil
	}
	m.logLoading = true
	m.logSteps, m.logLines = nil, nil
	return m, tea.Batch(m.fetchJobLogCmd(job, false), m.startSpinner())
}

// fetchJobLogCmd fetches a job log off the UI thread and reports it back.
func (m Model) fetchJobLogCmd(job string, all bool) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		out, err := action.JobLog(r, dir, job, !all)
		return logFetchedMsg{job: job, all: all, raw: out, err: err}
	}
}

// setLogSteps swaps in freshly parsed steps, clamps the cursor, and re-renders.
func (m *Model) setLogSteps(steps []logStep) {
	m.logSteps = steps
	m.logLines = flattenLog(steps)
	if m.logCursor >= len(m.logLines) {
		m.logCursor = max(0, len(m.logLines)-1)
	}
	m.setLogContent()
}
```

> `setLogContent` is defined in Task 5. To keep Task 4 compiling on its own, add a
> temporary stub at the end of `logview.go` now and replace it in Task 5:
>
> ```go
> // replaced in Task 5 (render)
> func (m *Model) setLogContent() {}
> ```

- [ ] **Step 3d: Wire the Checks-tab `enter` and the fetch message**

In `internal/ui/expanded.go`, change the `enter` case in `updateExpanded`:

```go
	case "enter":
		if m.expandedTab == 2 { // Checks: drill into the hovered check's logs
			return m.enterLogView()
		}
		if a, ok := m.actions["enter"]; ok {
			return m, m.runAction(a)
		}
		return m, nil
```

In `internal/ui/prlist.go` `Update`, add a case alongside the other msg cases (e.g. after `prDetailMsg`):

```go
	case logFetchedMsg:
		if !m.logView || msg.job != m.logJobID || msg.all != m.logShowAll {
			return m, nil // stale: view closed or variant switched
		}
		m.logLoading = false
		if msg.err != nil {
			m.logErr = msg.err
			m.setLogContent()
			return m, nil
		}
		m.logErr = nil
		steps := parseJobLog(msg.raw, !msg.all)
		m.logCache[logCacheKey(msg.job, msg.all)] = steps
		m.setLogSteps(steps)
		return m, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestEnterOpensLogView|TestLogFetched' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/prlist.go internal/ui/messages.go internal/ui/logview.go internal/ui/expanded.go internal/ui/logview_test.go
git commit -m "feat(ui): enter on a check fetches its job log (state + async)"
```

---

### Task 5: Render the log view + View dispatch + footer

**Files:**
- Modify: `internal/ui/logview.go` (replace the `setLogContent` stub; add render + footer + box sizing + cursor-visibility)
- Modify: `internal/ui/prlist.go` (`render()` dispatch)
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Consumes: Task 4 state; `truncate`, `titledBox`, `indentLines`, `headerStyle`, `statusBarStyle`, `dimStyle`, `failStyle`, `passStyle`, `focusBarStyle`, `titleStyle`, `m.statusBadge()`, `m.expandedBoxWidth()`.
- Produces:
  - `func (m *Model) setLogContent()`
  - `func (m Model) renderLogBody(w int) string`
  - `func (m *Model) keepLogCursorVisible()`
  - `func (m Model) logBoxHeight() int`
  - `func (m Model) logFooter() string`
  - `func (m Model) logViewRender() string`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/logview_test.go` (import `"github.com/charmbracelet/x/ansi"`):

```go
func TestRenderLogBody(t *testing.T) {
	m := logViewModel(t)
	m.logSteps = []logStep{{name: "Run tests", failed: true, lines: []string{"FAIL foo", "ok done"}}}
	m.logLines = flattenLog(m.logSteps)
	m.logCursor = 1 // "FAIL foo"
	out := m.renderLogBody(80)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "Run tests") || !strings.Contains(plain, "FAIL foo") {
		t.Fatalf("body missing content: %q", plain)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[1], "▎") { // cursor gutter on the focused line
		t.Fatalf("cursor gutter not on focused line: %q", lines)
	}
}

func TestRenderLogBodyLoadingAndEmpty(t *testing.T) {
	m := logViewModel(t)
	m.logLoading = true
	if !strings.Contains(ansi.Strip(m.renderLogBody(80)), "Loading") {
		t.Fatal("loading state should say Loading")
	}
	m.logLoading = false
	m.logSteps, m.logLines = nil, nil
	if !strings.Contains(ansi.Strip(m.renderLogBody(80)), "No logs") {
		t.Fatal("empty state should say No logs")
	}
}

func TestLogViewRenderDispatch(t *testing.T) {
	m := logViewModel(t)
	m.logView = true
	m.logLabel = "test (ubuntu)"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"x"}}}
	m.logLines = flattenLog(m.logSteps)
	m.setLogContent()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "test (ubuntu)") {
		t.Fatalf("log box title (check label) missing: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestRenderLogBody|TestLogViewRenderDispatch' ./...`
Expected: FAIL — `m.renderLogBody undefined`

- [ ] **Step 3a: Replace the stub and add render helpers (logview.go)**

Delete the temporary `func (m *Model) setLogContent() {}` stub and add (add import `"strings"` already present; ensure `tea` import remains):

```go
// logBoxHeight is the OUTER height of the log box: the frame minus the header
// and footer (the log view has no metadata line).
func (m Model) logBoxHeight() int {
	h := m.height - 2
	if h < 3 {
		h = 3
	}
	return h
}

// setLogContent (re)fills the viewport with the rendered log at the current
// geometry and keeps the cursor line on screen.
func (m *Model) setLogContent() {
	w := m.expandedBoxWidth() - 2
	rows := m.logBoxHeight() - 2
	if w < 1 {
		w = 1
	}
	if rows < 1 {
		rows = 1
	}
	m.vp.SetWidth(w)
	m.vp.SetHeight(rows)
	m.vp.SetContent(m.renderLogBody(w))
	m.keepLogCursorVisible()
}

// keepLogCursorVisible scrolls the viewport so the cursor line stays in view.
// One logLine renders to exactly one display line (each is truncated to width),
// so the cursor index is its display row.
func (m *Model) keepLogCursorVisible() {
	h := m.vp.Height()
	off := m.vp.YOffset()
	switch {
	case m.logCursor < off:
		m.vp.SetYOffset(m.logCursor)
	case m.logCursor >= off+h:
		m.vp.SetYOffset(m.logCursor - h + 1)
	}
}

// renderLogBody paints the flattened log: dim step headers (red for failed
// steps), content lines colored by classifyLogLine, cursor line gutter-marked.
func (m Model) renderLogBody(w int) string {
	switch {
	case m.logLoading:
		return dimStyle.Render("  Loading…")
	case m.logErr != nil:
		return failStyle.Render("  " + m.logErr.Error())
	case len(m.logLines) == 0:
		return dimStyle.Render("  No logs.")
	}
	var b strings.Builder
	for i, ln := range m.logLines {
		gutter := "  "
		if i == m.logCursor {
			gutter = focusBarStyle.Render("▎") + " "
		}
		text := truncate(ln.text, w-2)
		var styled string
		switch {
		case ln.header:
			if m.logSteps[ln.step].failed {
				styled = failStyle.Bold(true).Render(text)
			} else {
				styled = dimStyle.Render(text)
			}
		default:
			switch classifyLogLine(ln.text) {
			case kindError:
				styled = failStyle.Render(text)
			case kindPass:
				styled = passStyle.Render(text)
			default:
				styled = titleStyle.Render(text)
			}
		}
		b.WriteString(gutter + styled + "\n")
	}
	return b.String()
}

// logFooter is the log view's key hint line; `a` toggles the log scope.
func (m Model) logFooter() string {
	word := "all steps"
	if m.logShowAll {
		word = "failed only"
	}
	return "  j/k move · y line · s step · Y all · a " + word + " · esc back"
}

// logViewRender is the full-screen log view: header, the log framed in a titled
// box (the check label as title), and the key hint line — centered like the
// expanded view.
func (m Model) logViewRender() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	bw := m.expandedBoxWidth()
	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	head += m.statusBadge()
	foot := statusBarStyle.Render(m.logFooter())
	box := titledBox(m.vp.View(), bw, m.logBoxHeight(), m.logLabel)
	out := strings.Join([]string{head, box, foot}, "\n")
	if bw < m.width {
		out = indentLines(out, (m.width-bw)/2)
	}
	return out
}
```

- [ ] **Step 3b: Dispatch in render()**

In `internal/ui/prlist.go` `render()`, add the log view before the expanded branch:

```go
func (m Model) render() string {
	if m.logView {
		return m.logViewRender()
	}
	if m.expanded {
		return m.expandedView()
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestRenderLogBody|TestLogViewRenderDispatch' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/logview.go internal/ui/prlist.go internal/ui/logview_test.go
git commit -m "feat(ui): render the in-app check log view"
```

---

### Task 6: Log-view navigation + copy keys

**Files:**
- Modify: `internal/ui/logview.go` (`updateLogView`, `copyLogText`)
- Modify: `internal/ui/prlist.go` (`Update` KeyMsg dispatch)
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Consumes: Task 4/5 state and helpers; `clipboardArgv`, `writeClipboard`, `actionStat`, `actionDoneMsg`, `clearStatusCmd`, `startSpinner`.
- Produces:
  - `func (m Model) updateLogView(msg tea.KeyMsg) (tea.Model, tea.Cmd)`
  - `func (m Model) copyLogText(text, ok string) (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/logview_test.go`:

```go
func loadedLogModel(t *testing.T) Model {
	t.Helper()
	m := logViewModel(t)
	m.logView = true
	m.logJobID = "99"
	m.logSteps = []logStep{{name: "Run tests", lines: []string{"x", "y"}}}
	m.logLines = flattenLog(m.logSteps) // [header, x, y]
	m.setLogContent()
	return m
}

func TestLogViewCursorMoves(t *testing.T) {
	m := loadedLogModel(t)
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = u.(Model)
	if m.logCursor != 1 {
		t.Fatalf("j should move cursor to 1, got %d", m.logCursor)
	}
	u, _ = m.updateLogView(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if u.(Model).logCursor != 0 {
		t.Fatal("k should move cursor back to 0")
	}
}

func TestLogViewEscClosesView(t *testing.T) {
	m := loadedLogModel(t)
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: tea.KeyEscape})
	if u.(Model).logView {
		t.Fatal("esc should close the log view")
	}
}

func TestLogViewToggleRefetches(t *testing.T) {
	m := loadedLogModel(t)
	u, cmd := m.updateLogView(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = u.(Model)
	if !m.logShowAll || !m.logLoading || cmd == nil {
		t.Fatalf("a should switch to full log and refetch: all=%v loading=%v", m.logShowAll, m.logLoading)
	}
}

func TestLogViewCopyStep(t *testing.T) {
	m := loadedLogModel(t)
	m.logCursor = 1 // "x", in step "Run tests"
	u, _ := m.updateLogView(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = u.(Model)
	if m.actionStatus == nil {
		t.Fatal("s should set a copy status")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestLogView' ./...`
Expected: FAIL — `m.updateLogView undefined`

- [ ] **Step 3a: Add updateLogView + copyLogText (logview.go)**

```go
// updateLogView handles keys while the check-log sub-view is open.
func (m Model) updateLogView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "h", "left":
		m.logView = false
		m.renderExpanded() // restore the Checks tab into the viewport
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.logCursor < len(m.logLines)-1 {
			m.logCursor++
			m.setLogContent()
		}
		return m, nil
	case "k", "up":
		if m.logCursor > 0 {
			m.logCursor--
			m.setLogContent()
		}
		return m, nil
	case "a": // toggle failed-only ↔ full job log
		m.logShowAll = !m.logShowAll
		m.logCursor = 0
		m.logErr = nil
		if steps, hit := m.logCache[logCacheKey(m.logJobID, m.logShowAll)]; hit {
			m.logLoading = false
			m.setLogSteps(steps)
			return m, nil
		}
		m.logLoading = true
		m.logSteps, m.logLines = nil, nil
		m.setLogContent()
		return m, tea.Batch(m.fetchJobLogCmd(m.logJobID, m.logShowAll), m.startSpinner())
	case "y":
		return m.copyLogText(copyLine(m.logLines, m.logCursor), "Copied line")
	case "s":
		return m.copyLogText(copyStep(m.logSteps, m.logLines, m.logCursor), "Copied step")
	case "Y":
		return m.copyLogText(copyWhole(m.logSteps), "Copied log")
	}
	return m, nil
}

// copyLogText copies text via the native clipboard tool, falling back to OSC 52,
// mirroring runAction's copy path. Returns the mutated model so callers avoid the
// return-value evaluation-order trap.
func (m Model) copyLogText(text, ok string) (tea.Model, tea.Cmd) {
	if text == "" {
		return m, nil
	}
	if argv := clipboardArgv(); argv != nil {
		m.actionStatus = &actionStat{run: "Copying", ok: ok, fail: "Copy failed"}
		return m, tea.Batch(func() tea.Msg {
			return actionDoneMsg{err: writeClipboard(argv, text)}
		}, m.startSpinner())
	}
	m.actionStatus = &actionStat{ok: ok, fail: "Copy failed", settled: true}
	return m, tea.Batch(tea.SetClipboard(text), clearStatusCmd())
}
```

- [ ] **Step 3b: Dispatch keys in Update**

In `internal/ui/prlist.go` `Update`, in the `case tea.KeyMsg:` block, dispatch the log view before the expanded branch:

```go
	case tea.KeyMsg:
		if m.logView {
			return m.updateLogView(msg)
		}
		if m.expanded {
			return m.updateExpanded(msg)
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestLogView' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/logview.go internal/ui/prlist.go internal/ui/logview_test.go
git commit -m "feat(ui): log-view navigation and per-line/step/all copy"
```

---

### Task 7: Checks-tab retargeting — open/copy job URL + external fallback

**Files:**
- Create: `internal/ui/browser.go`
- Modify: `internal/ui/expanded.go` (`o`/`Y` cases on Checks tab; `esc`/`enter` already handled)
- Modify: `internal/ui/logview.go` (`enterLogView` external branch → open browser)
- Test: `internal/ui/browser_test.go`, `internal/ui/logview_test.go`

**Interfaces:**
- Produces:
  - `func browserArgv(goos string) []string`
  - `func openURL(url string) error`
  - `func (m Model) openHoveredCheck() (tea.Model, tea.Cmd)`
  - `func (m Model) copyHoveredCheckURL() (tea.Model, tea.Cmd)`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/browser_test.go`:

```go
package ui

import (
	"reflect"
	"testing"
)

func TestBrowserArgv(t *testing.T) {
	if got := browserArgv("darwin"); !reflect.DeepEqual(got, []string{"open"}) {
		t.Fatalf("darwin argv = %v", got)
	}
	if got := browserArgv("linux"); !reflect.DeepEqual(got, []string{"xdg-open"}) {
		t.Fatalf("linux argv = %v", got)
	}
}
```

Add to `internal/ui/logview_test.go`:

```go
func TestCheckTabOpenSetsStatus(t *testing.T) {
	m := logViewModel(t) // hovered check has a DetailsUrl
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = u.(Model)
	if m.actionStatus == nil || cmd == nil {
		t.Fatal("o on the Checks tab should open the check URL and set a status")
	}
	if m.logView {
		t.Fatal("o should not enter the log view")
	}
}

func TestCheckTabCopyURL(t *testing.T) {
	m := logViewModel(t)
	u, _ := m.updateExpanded(tea.KeyPressMsg{Code: 'Y', Text: "Y"})
	if u.(Model).actionStatus == nil {
		t.Fatal("Y on the Checks tab should copy the check URL")
	}
}

func TestExternalCheckEnterOpensBrowser(t *testing.T) {
	m := logViewModel(t)
	// Replace the check with an external one (no /job/ in the URL → JobID "").
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Context: "ci/ext", DetailsUrl: "https://ci.example.com/build/7"},
	}}})
	m.expanded, m.expandedTab, m.checkCursor = true, 2, 0
	m.renderExpanded()
	u, cmd := m.updateExpanded(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = u.(Model)
	if m.logView {
		t.Fatal("external check has no job logs; enter should not open the log view")
	}
	if m.actionStatus == nil || cmd == nil {
		t.Fatal("external check enter should open the browser with a status")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run 'TestBrowserArgv|TestCheckTab|TestExternalCheck' ./...`
Expected: FAIL — `undefined: browserArgv` / `openHoveredCheck`

- [ ] **Step 3a: Create the opener (browser.go)**

```go
package ui

import (
	"os/exec"
	"runtime"
)

// browserArgv is the OS command that opens a URL. Split out (like clipboardArgv)
// so the choice is unit-testable without spawning anything.
func browserArgv(goos string) []string {
	if goos == "darwin" {
		return []string{"open"}
	}
	return []string{"xdg-open"} // linux and the rest
}

// openURL opens url in the default browser. Fire-and-forget: the opener detaches.
func openURL(url string) error {
	argv := append(browserArgv(runtime.GOOS), url)
	return exec.Command(argv[0], argv[1:]...).Start()
}
```

- [ ] **Step 3b: Add the check-URL action helpers (logview.go)**

```go
// openHoveredCheck opens the hovered check's details URL in the browser.
func (m Model) openHoveredCheck() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok {
		return m, nil
	}
	if c.DetailsUrl == "" {
		m.actionStatus = &actionStat{fail: "no URL for this check", settled: true,
			err: fmt.Errorf("check %q has no URL", c.Label())}
		return m, clearStatusCmd()
	}
	url := c.DetailsUrl
	m.actionStatus = &actionStat{run: "Opening", ok: "Opened in browser", fail: "Open failed"}
	return m, tea.Batch(func() tea.Msg {
		return actionDoneMsg{err: openURL(url)}
	}, m.startSpinner())
}

// copyHoveredCheckURL copies the hovered check's details URL.
func (m Model) copyHoveredCheckURL() (tea.Model, tea.Cmd) {
	c, ok := m.hoveredCheck()
	if !ok || c.DetailsUrl == "" {
		return m, nil
	}
	return m.copyLogText(c.DetailsUrl, "Copied URL")
}
```

- [ ] **Step 3c: Retarget the external-check enter branch (logview.go)**

In `enterLogView`, replace the `job == ""` branch body with:

```go
	if job == "" { // external check: no job log — open its page instead
		return m.openHoveredCheck()
	}
```

- [ ] **Step 3d: Wire `o` and `Y` on the Checks tab (expanded.go)**

In `updateExpanded`, add cases (place near the `r`/`R` Checks-tab cases):

```go
	case "o": // Checks tab: open the hovered check in the browser
		if m.expandedTab == 2 {
			return m.openHoveredCheck()
		}
	case "Y": // Checks tab: copy the hovered check's URL
		if m.expandedTab == 2 {
			return m.copyHoveredCheckURL()
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run 'TestBrowserArgv|TestCheckTab|TestExternalCheck' ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/browser.go internal/ui/browser_test.go internal/ui/logview.go internal/ui/expanded.go internal/ui/logview_test.go
git commit -m "feat(ui): Checks tab open/copy job URL; external checks open in browser"
```

---

### Task 8: Footer & legend hints for the Checks tab

**Files:**
- Modify: `internal/ui/expanded.go` (`expandedFooter`)
- Test: `internal/ui/logview_test.go`

**Interfaces:**
- Consumes: `expandedFooter` (existing).

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/logview_test.go`:

```go
func TestChecksFooterShowsLogKeys(t *testing.T) {
	m := logViewModel(t)
	foot := m.expandedFooter()
	for _, want := range []string{"logs", "open", "rerun"} {
		if !strings.Contains(foot, want) {
			t.Fatalf("Checks footer missing %q: %q", want, foot)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd internal/ui && go test -run TestChecksFooterShowsLogKeys ./...`
Expected: FAIL — footer still says "r rerun · R rerun all …" without "logs"/"open"

- [ ] **Step 3: Update the Checks-tab footer**

In `internal/ui/expanded.go`, change `expandedFooter`'s Checks branch:

```go
func (m Model) expandedFooter() string {
	if m.expandedTab == 2 {
		return "  ↵ logs · o open · Y url · r rerun · R all · j/k move · esc back"
	}
	return "  j/k scroll · h/l tabs · J/K PR · ↵ worktree · esc back"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd internal/ui && go test -run TestChecksFooterShowsLogKeys ./...`
Expected: PASS

- [ ] **Step 5: Full suite + build**

Run: `go build ./... && go test ./...`
Expected: build clean, all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/expanded.go internal/ui/logview_test.go
git commit -m "feat(ui): Checks-tab footer advertises log/open/copy keys"
```

---

## Self-Review

**Spec coverage:**
- Enter drills into focused entity → Task 4 (Checks→logs) + unchanged worktree elsewhere (Task 4 leaves the non-Checks `enter` path intact). ✓
- Checks retargets `o`/`Y`/`r` → Task 7 (`o`/`Y`); `r` pre-existing (`rerunHoveredCheck`, noted in spec). ✓
- In-app viewer: async, cached per job, failed-only default + `a` full log → Tasks 1/4/6. ✓
- Theme coloring (grey ts stripped, red errors, failed-step header) → Tasks 2/3/5. ✓
- Line cursor + `y`/`s`/`Y` copy → Tasks 3/6. ✓
- External checks → browser fallback, `Y` copies URL → Task 7. ✓
- Pending/empty → "No logs" / "Loading…" notices → Task 5; error → Task 4/5. ✓
- Footer hints → Task 8. ✓

**Placeholder scan:** The only intentional stub is `setLogContent` in Task 4, explicitly removed in Task 5 Step 3a. No TBD/TODO left in delivered code.

**Type consistency:** `logStep{name,failed,lines}`, `logLine{text,step,header}`, `logFetchedMsg{job,all,raw,err}`, `logCacheKey(job,all)`, `fetchJobLogCmd(job,all)`, `JobLog(r,dir,jobID,failedOnly)` — names/signatures match across Tasks 1–8. `copyLogText` and the `openHovered…`/`copyHovered…` helpers all return `(tea.Model, tea.Cmd)` to dodge the return-value evaluation-order trap.

**Open verification points flagged in-plan:** (1) real `gh` log format — Task 2 Step 5; (2) the exact `tea.KeyMsg` constructor the suite uses — Task 4 Step 1 note.
