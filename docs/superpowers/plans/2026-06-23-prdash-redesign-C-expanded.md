# prdash Redesign — Phase C: Expanded tabbed view Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `→` expands the focused PR into an immersive, scrollable detail view with a `Conversation · Reviews · Checks · Diff` tab strip; the triage card deep-links straight to the relevant tab; `j/k` step PRs without leaving, `esc` returns to the list.

**Architecture:** A new expanded mode on the model (`expanded bool`, `expandedTab int`) reuses the existing `bubbles/viewport` (the list isn't shown while expanded) to scroll per-tab content. Pure tab renderers consume data already fetched in Phase B (`PRDetail.LatestReviews`, `Diffstat()`/`Files`, the PR's `StatusCheckRollup` via a new `Check.Result()`); entering expanded reads the triage card's `JumpTab` to pick the starting tab.

**Tech Stack:** Go 1.26, bubbletea v1, `bubbles/viewport`, lipgloss. Spec: `docs/superpowers/specs/2026-06-23-prdash-tui-redesign-design.md` (§"Expanded view", §"The triage ladder" JumpTab). Builds on Phases A + B.

---

## Conventions

- Work on `feat/redesign-phase-c` (already created from `main`). TDD; one logical change per commit. After each task: `go build ./... && go vet ./... && gofmt -l .` clean.
- Reuse Phase B data — no new `gh` fetch fields. The only `gh` change is a per-check state helper.

## File structure (Phase C)

- `internal/gh/prs.go` — **modify.** Add `Check.Result()` (per-check pass/fail/pending), and refactor `CIState()` to reuse it (DRY).
- `internal/gh/prs_test.go` — **modify.** Add `TestCheckResult`.
- `internal/ui/expanded.go` — **new.** Tab list, `jumpTabIndex`, `tabStrip`, the three tab renderers (`renderReviews`/`renderChecks`/`renderDiffstat`), `expandedBody`, `enterExpanded`/`renderExpanded`/`updateExpanded`/`expandedView`.
- `internal/ui/expanded_test.go` — **new.**
- `internal/ui/prlist.go` — **modify.** Model gains `expanded`/`expandedTab`; `Update` routes to `updateExpanded` and binds `→`/`l` to enter; `View` renders `expandedView` when expanded; status bar hint.

---

## Task 1: `gh.Check.Result()` (per-check state)

**Files:**
- Modify: `internal/gh/prs.go`, `internal/gh/prs_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/gh/prs_test.go`:

```go
func TestCheckResult(t *testing.T) {
	cases := map[string]string{"SUCCESS": "pass", "FAILURE": "fail", "PENDING": "pending", "": "pending"}
	for state, want := range cases {
		if got := (Check{State: state}).Result(); got != want {
			t.Errorf("Check{State:%q}.Result() = %q, want %q", state, got, want)
		}
	}
	if got := (Check{Conclusion: "FAILURE"}).Result(); got != "fail" {
		t.Errorf("conclusion fallback: %q", got)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/gh/ -run TestCheckResult`
Expected: FAIL — `Result` undefined.

- [ ] **Step 3: Implement `Check.Result()` and refactor `CIState()` in `internal/gh/prs.go`**

Add the method (place it next to `CIState`):

```go
// Result collapses one check to pass/fail/pending, resolving state→conclusion.
func (c Check) Result() string {
	s := c.State
	if s == "" {
		s = c.Conclusion
	}
	switch s {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
		return "fail"
	case "PENDING", "QUEUED", "IN_PROGRESS", "":
		return "pending"
	default:
		return "pass"
	}
}
```

Refactor `CIState()` to reuse it (replace its per-check switch):

```go
func (p PR) CIState() string {
	if len(p.StatusCheckRollup) == 0 {
		return "none"
	}
	pending, failed := false, false
	for _, c := range p.StatusCheckRollup {
		switch c.Result() {
		case "fail":
			failed = true
		case "pending":
			pending = true
		}
	}
	switch {
	case failed:
		return "fail"
	case pending:
		return "pending"
	default:
		return "pass"
	}
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/gh/ -v`
Expected: PASS — `TestCheckResult` plus existing `TestCIState` (behavior unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/gh/
git commit -m "feat(gh): Check.Result() per-check state; CIState reuses it"
```

---

## Task 2: Tab renderers + tab strip + deep-link mapping (pure)

**Files:**
- Create: `internal/ui/expanded.go`, `internal/ui/expanded_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/ui/expanded_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestJumpTabIndex(t *testing.T) {
	cases := map[string]int{"reviews": 1, "checks": 2, "diff": 3, "conversation": 0, "": 0}
	for jump, want := range cases {
		if got := jumpTabIndex(jump); got != want {
			t.Errorf("jumpTabIndex(%q) = %d, want %d", jump, got, want)
		}
	}
}

func TestRenderChecksListsByName(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "FAILURE", Name: "lint"},
		{State: "SUCCESS", Name: "build"},
	}}
	out := renderChecks(pr, 60)
	if !strings.Contains(out, "lint") || !strings.Contains(out, "build") {
		t.Fatalf("checks not listed by name: %q", out)
	}
}

func TestRenderDiffstatTotals(t *testing.T) {
	d := gh.PRDetail{Files: []gh.DiffFile{
		{Path: "a.go", Additions: 10, Deletions: 2},
		{Path: "b.go", Additions: 1, Deletions: 1},
	}}
	out := renderDiffstat(d, 60)
	if !strings.Contains(out, "2 files") || !strings.Contains(out, "a.go") {
		t.Fatalf("diffstat missing totals/files: %q", out)
	}
}

func TestRenderReviewsEmpty(t *testing.T) {
	if !strings.Contains(renderReviews(gh.PRDetail{}, 60), "No reviews") {
		t.Fatal("empty reviews should say so")
	}
}

func TestTabStripMarksActive(t *testing.T) {
	out := tabStrip(2)
	for _, name := range expandedTabs {
		if !strings.Contains(out, name) {
			t.Fatalf("tab %q missing from strip: %q", name, out)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/ui/ -run 'TestJumpTabIndex|TestRenderChecks|TestRenderDiffstat|TestRenderReviews|TestTabStrip'`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement the pure parts of `internal/ui/expanded.go`**

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

var expandedTabs = []string{"Conversation", "Reviews", "Checks", "Diff"}

// jumpTabIndex maps a triage card's JumpTab to a tab index (default Conversation).
func jumpTabIndex(jump string) int {
	switch jump {
	case "reviews":
		return 1
	case "checks":
		return 2
	case "diff":
		return 3
	default:
		return 0
	}
}

func tabStrip(active int) string {
	parts := make([]string, len(expandedTabs))
	for i, t := range expandedTabs {
		if i == active {
			parts[i] = accentStyle.Render(t)
		} else {
			parts[i] = dimStyle.Render(t)
		}
	}
	return "  " + strings.Join(parts, "   ")
}

func renderReviews(d gh.PRDetail, w int) string {
	if len(d.LatestReviews) == 0 {
		return dimStyle.Render("  No reviews yet.")
	}
	var b strings.Builder
	for _, r := range d.LatestReviews {
		hdr := "@" + r.Author.Login
		if r.State != "" {
			hdr += " · " + r.State
		}
		b.WriteString(accentStyle.Render(hdr) + "\n")
		if r.Body != "" {
			body, err := preview.Render(r.Body, w)
			if err != nil {
				body = r.Body
			}
			b.WriteString(body)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderChecks(pr gh.PR, w int) string {
	if len(pr.StatusCheckRollup) == 0 {
		return dimStyle.Render("  No checks.")
	}
	var b strings.Builder
	for _, c := range pr.StatusCheckRollup {
		b.WriteString("  " + ciGlyph(c.Result()) + " " + truncate(c.Label(), w-4) + "\n")
	}
	return b.String()
}

func renderDiffstat(d gh.PRDetail, w int) string {
	s := d.Diffstat()
	if s.Files == 0 {
		return dimStyle.Render("  No file changes.")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %d files  %s  %s\n\n", s.Files,
		passStyle.Render(fmt.Sprintf("+%d", s.Additions)), failStyle.Render(fmt.Sprintf("-%d", s.Deletions))))
	for _, f := range d.Files {
		b.WriteString(fmt.Sprintf("  %s  %s %s\n", truncate(f.Path, w-16),
			passStyle.Render(fmt.Sprintf("+%d", f.Additions)), failStyle.Render(fmt.Sprintf("-%d", f.Deletions))))
	}
	return b.String()
}
```

- [ ] **Step 4: Run, verify pass**

Run: `go test ./internal/ui/ -run 'TestJumpTabIndex|TestRenderChecks|TestRenderDiffstat|TestRenderReviews|TestTabStrip' -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): expanded tab renderers (reviews/checks/diffstat) + deep-link map"
```

---

## Task 3: Expanded mode — state, enter/update/render, View wiring

**Files:**
- Modify: `internal/ui/expanded.go` (add the model-bound methods)
- Modify: `internal/ui/prlist.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/expanded_test.go`:

```go
import (
	// add to the existing import block:
	"github.com/noamsto/prdash/internal/triage"
)

func TestEnterExpandedDeepLinks(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, StatusCheckRollup: []gh.Check{{State: "FAILURE", Name: "lint"}}}})
	// detail with BLOCKED so the card is "checks failing" → JumpTab "checks" (index 2)
	m.detail[7] = gh.PRDetail{MergeStateStatus: "BLOCKED"}

	m.enterExpanded()
	if !m.expanded {
		t.Fatal("enterExpanded should set expanded")
	}
	if m.expandedTab != 2 {
		t.Fatalf("deep-link to Checks tab expected (2), got %d", m.expandedTab)
	}
	// sanity: the triage card for this PR really is checks-failing
	if triage.Compute(gh.PR{StatusCheckRollup: []gh.Check{{State: "FAILURE"}}}, gh.PRDetail{MergeStateStatus: "BLOCKED"}).JumpTab != "checks" {
		t.Fatal("precondition: expected checks JumpTab")
	}
}

func TestExpandedViewShowsTabStrip(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.setPRs([]gh.PR{{Number: 7, Title: "hi"}})
	m.detail[7] = gh.PRDetail{}
	m.enterExpanded()
	out := m.expandedView()
	if !strings.Contains(out, "Conversation") || !strings.Contains(out, "Checks") {
		t.Fatalf("expanded view should show the tab strip: %q", out)
	}
	if !strings.Contains(out, "#7") {
		t.Fatalf("expanded view should show the PR number: %q", out)
	}
}
```

- [ ] **Step 2: Run, verify fail**

Run: `go test ./internal/ui/ -run 'TestEnterExpanded|TestExpandedView'`
Expected: FAIL — `m.enterExpanded`, `m.expanded`, `m.expandedView` undefined.

- [ ] **Step 3: Add model fields in `internal/ui/prlist.go`**

Add to the `Model` struct (after `previewN int`):

```go
	expanded    bool
	expandedTab int
```

- [ ] **Step 4: Add the model-bound methods to `internal/ui/expanded.go`**

Append:

```go
// enterExpanded opens the focused PR's detail, deep-linking to the tab the
// triage card points at (when its detail is already cached).
func (m *Model) enterExpanded() {
	m.expanded = true
	m.expandedTab = 0
	if v, ok := m.cursorVars(); ok {
		if d, cached := m.detail[v.Number]; cached {
			if ps, ok := m.section.(*PRSection); ok {
				m.expandedTab = jumpTabIndex(triage.Compute(ps.prAt(m.cursor), d).JumpTab)
			}
		}
	}
	m.renderExpanded()
}

// expandedBody renders the active tab's content for the focused PR.
func (m Model) expandedBody(w int) string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return dimStyle.Render("  Loading…")
	}
	switch m.expandedTab {
	case 1:
		return renderReviews(d, w)
	case 2:
		if ps, ok := m.section.(*PRSection); ok {
			return renderChecks(ps.prAt(m.cursor), w)
		}
		return ""
	case 3:
		return renderDiffstat(d, w)
	default:
		items := preview.Timeline(d)
		return renderTimeline(items, len(items), w, true)
	}
}

// renderExpanded fills the viewport with the active tab's content, scroll reset.
func (m *Model) renderExpanded() {
	l := computeLayout(m.width, m.height)
	m.vp.Width = m.width
	m.vp.Height = l.ContentHeight - 1 // tab strip takes one row
	m.vp.SetContent(m.expandedBody(m.width))
	m.vp.SetYOffset(0)
}

// updateExpanded handles keys while in expanded mode.
func (m Model) updateExpanded(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "left", "h":
		m.expanded = false
		m.renderList()
		return m, nil
	case "tab", "right", "l":
		m.expandedTab = (m.expandedTab + 1) % len(expandedTabs)
		m.renderExpanded()
		return m, nil
	case "shift+tab":
		m.expandedTab = (m.expandedTab + len(expandedTabs) - 1) % len(expandedTabs)
		m.renderExpanded()
		return m, nil
	case "1", "2", "3", "4":
		m.expandedTab = int(msg.String()[0] - '1')
		m.renderExpanded()
		return m, nil
	case "j":
		if m.cursor < m.section.Len()-1 {
			m.cursor++
		}
		m.renderExpanded()
		return m, m.detailCmdForCursor()
	case "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.renderExpanded()
		return m, m.detailCmdForCursor()
	case "enter":
		if a, ok := m.actions["enter"]; ok {
			return m, m.runAction(a)
		}
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg) // up/down/pgup/pgdn scroll the content
	return m, cmd
}

// expandedView is the full-screen detail: header, tab strip, scrollable body, keys.
func (m Model) expandedView() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	foot := statusBarStyle.Render("  ↑↓ scroll · tab view · j/k PR · ↵ worktree · esc back")
	return head + "\n" + tabStrip(m.expandedTab) + "\n" + m.vp.View() + "\n" + foot
}
```

Add the imports `tea "github.com/charmbracelet/bubbletea"` and `"github.com/noamsto/prdash/internal/triage"` to `expanded.go`'s import block.

- [ ] **Step 5: Wire `Update` + `View` in `internal/ui/prlist.go`**

In `Update`'s `case tea.KeyMsg:`, add the expanded router as the FIRST check (before `if m.pending != nil`):

```go
		if m.expanded {
			return m.updateExpanded(msg)
		}
```

In the normal-mode `switch msg.String()` (the one with "a"/"/"/"q"/" "/"V"/"tab"), add a case to enter expanded BEFORE the `default:`:

```go
		case "right", "l":
			m.enterExpanded()
			return m, m.detailCmdForCursor()
```

In `View`, add the expanded branch as the FIRST check (before `if m.pending != nil`):

```go
	if m.expanded {
		return m.expandedView()
	}
```

- [ ] **Step 6: Run the full UI suite + build**

Run: `go test ./internal/ui/ -v && go build ./... && go vet ./... && gofmt -l .`
Expected: PASS — new expanded tests + all pre-existing UI tests; build/vet/fmt clean.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go internal/ui/prlist.go
git commit -m "feat(ui): expanded tabbed view + deep-link from triage card"
```

---

## Task 4: Live smoke test

**Files:** none (manual verification)

- [ ] **Step 1: Build + run; exercise the expanded view**

Build (`go build -o /tmp/prdash-c .`) and run in a real terminal against PRs with comments/reviews/checks/files. Verify:
- `→` (or `l`) on a failing-CI PR opens expanded **on the Checks tab** (deep-link), listing checks by name with glyphs.
- `tab` cycles Conversation → Reviews → Checks → Diff; `1`–`4` jump directly; content scrolls with `↑↓`.
- `j`/`k` step to the next/prev PR without leaving expanded (content + PR number update; detail lazily fetches).
- `esc` returns to the list at the same cursor; `↵` opens the worktree (exits-TUI).
- Diff tab shows the diffstat (file list + `+/−` + totals); Reviews shows review summaries (or "No reviews yet.").

- [ ] **Step 2: Fix any scroll/width/deep-link issues found; commit**

```bash
git add -A
git commit -m "polish(ui): expanded view tuning from live test"
```

---

## Self-review (done)

- **Spec coverage (Phase C slice):** expanded view with `Conversation · Reviews · Checks · Diff` tabs ✓ (T2/T3), `→` enters + `esc` back + `tab`/`1-4` switch ✓ (T3), `j/k` step PRs without collapsing ✓ (T3), deep-link from the card's `JumpTab` ✓ (T3 `enterExpanded`), Diff = diffstat (not full pager, per spec out-of-scope) ✓ (T2), Checks lists per-check names via `Check.Label()`/`Result()` ✓ (T1/T2), Reviews from `LatestReviews` ✓ (T2). This completes the redesign spec (Phases A+B+C).
- **Placeholders:** none — every code step shows code; T4 is explicit manual verification.
- **Type consistency:** `gh.Check.Result()`, `expandedTabs`, `jumpTabIndex`, `tabStrip`, `renderReviews/renderChecks/renderDiffstat`, `m.expanded`/`m.expandedTab`, `enterExpanded`/`expandedBody`/`renderExpanded`/`updateExpanded`/`expandedView` consistent across tasks. Reuses Phase B's `triage.Compute`, `PRDetail.{LatestReviews,Diffstat,Files}`, `Check.Label()`, and Phase A's `m.vp`/`computeLayout`/`truncate`/theme styles.

## Next

This is the final phase of the redesign. After Phase C the locked spec is fully implemented. Remaining project work is the cross-repo **lazytmux integration** (the worktree fan-out, Plan 2 T9 / Plan 3 T6) and the interactive verification of the Go-side items flagged across reviews (OSC52 copy, fetch debounce, detail-error scoping) — all tracked in project memory.
