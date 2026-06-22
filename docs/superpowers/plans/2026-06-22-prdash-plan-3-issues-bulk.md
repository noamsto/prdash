# prdash Plan 3 — Issues + bulk worktree fan-out

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add a my-issues section and the headline workflow — select N items, explode each into its own worktree + tmux window in the same session.

**Architecture:** Generalize the PR-specific list into a `Section` (kind + fetch + columns + actions). Add the `issue` kind (`gh issue list --search`) with branch derivation per Noam's `type/id-desc` convention. Add multi-select and a `per-selected` bulk action `W` that writes one handoff line per selected item; the `prdash-apply` orchestrator (Plan 2 T9) is hardened to be idempotent.

**Tech Stack:** Go 1.23, bubbletea v1, stdlib. Builds on Plans 1–2. Spec: `2026-06-22-prdash-design.md`.

---

## File structure
- `internal/gh/issues.go` — Issue struct, fetch
- `internal/issue/branch.go` — `type/id-desc` derivation
- `internal/ui/section.go` — `Section` (generalizes PR/issue list)
- `internal/ui/select.go` — multi-select state
- lazytmux `prdash-apply.sh` — idempotency hardening

---

## Task 1: Issue fetch

**Files:** Create `internal/gh/issues.go`, `internal/gh/issues_test.go`

- [ ] **Step 1: Failing test**

```go
package gh

import "testing"

func TestIssueListArgs(t *testing.T) {
	args := IssueListArgs("assignee:@me", 20)
	if args[0] != "issue" || args[1] != "list" || args[2] != "--search" {
		t.Fatalf("args = %v", args)
	}
}

func TestParseIssues(t *testing.T) {
	is, err := ParseIssues([]byte(`[{"number":4,"title":"bug","labels":[{"name":"fix"}]}]`))
	if err != nil || len(is) != 1 || is[0].Number != 4 || is[0].Labels[0].Name != "fix" {
		t.Fatalf("parsed=%+v err=%v", is, err)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/gh/issues.go`**

```go
package gh

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var issueFields = []string{"number", "title", "author", "labels", "assignees", "url", "updatedAt"}

type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Author    struct{ Login string `json:"login"` } `json:"author"`
	Labels    []Label   `json:"labels"`
	Assignees []struct{ Login string `json:"login"` } `json:"assignees"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func IssueListArgs(filter string, limit int) []string {
	return []string{
		"issue", "list", "--search", filter,
		"-L", strconv.Itoa(limit), "--json", strings.Join(issueFields, ","),
	}
}

func FetchIssues(r Runner, dir, filter string, limit int) ([]Issue, error) {
	out, err := r.Run(dir, IssueListArgs(filter, limit)...)
	if err != nil {
		return nil, err
	}
	return ParseIssues(out)
}

func ParseIssues(b []byte) ([]Issue, error) {
	var is []Issue
	if err := json.Unmarshal(b, &is); err != nil {
		return nil, err
	}
	return is, nil
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(gh): issue fetch`.

---

## Task 2: Issue → branch derivation

**Files:** Create `internal/issue/branch.go`, `internal/issue/branch_test.go`

- [ ] **Step 1: Failing test**

```go
package issue

import "testing"

func TestBranchDefaultType(t *testing.T) {
	got := Branch(213, "Seed avatars by id", nil)
	if got != "feat/213-seed-avatars-by-id" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchLabelOverride(t *testing.T) {
	got := Branch(8, "Crash on launch!", []string{"bug", "fix"})
	if got != "fix/8-crash-on-launch" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchSlugTrims(t *testing.T) {
	long := "This is a very long issue title that should be truncated nicely here"
	got := Branch(1, long, nil)
	if len(got) > 4+1+1+1+40 { // "feat/" + "1" + "-" + ≤40
		t.Fatalf("slug too long: %q", got)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/issue/branch.go`**

```go
package issue

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	typeOrder = []string{"feat", "fix", "chore", "docs", "refactor"}
)

// Branch derives "{type}/{number}-{slug}" per Noam's convention. type defaults
// to feat, overridden by the first matching commit-type label; slug ≤40 chars.
func Branch(number int, title string, labels []string) string {
	typ := "feat"
	for _, want := range typeOrder {
		for _, l := range labels {
			if strings.EqualFold(l, want) {
				typ = want
			}
		}
	}
	slug := nonSlug.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return fmt.Sprintf("%s/%d-%s", typ, number, slug)
}
```

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(issue): branch derivation (type/id-desc)`.

---

## Task 3: Section generalization

**Files:** Create `internal/ui/section.go`, `internal/ui/section_test.go`; refactor `prlist.go`

- [ ] **Step 1: Failing test (a Section exposes kind, rows, vars)**

```go
package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

func TestPRSectionRows(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{Number: 7, Title: "x"}})
	rows := s.Rows()
	if len(rows) != 1 || rows[0][0] != "#7" {
		t.Fatalf("rows=%v", rows)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/ui/section.go`**

Define a `Section` interface and PR/issue implementations. (Refactor the row-building out of `Model` into the section; `Model` holds the active `Section`.)

```go
package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"

	"github.com/noamsto/prdash/internal/action"
	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/issue"
)

type Section interface {
	Kind() string
	Filter() string
	Columns() []table.Column
	Rows() []table.Row     // from the current (filtered) items
	Len() int
	VarsAt(i int) action.Vars
	Haystacks() []string   // for fuzzy filter
	SetShown(idx []int)    // indices (post-filter) the table currently shows
}

// --- PR section ---
type PRSection struct {
	filter string
	prs    []gh.PR
	shown  []int
}

func NewPRSection(filter string) *PRSection { return &PRSection{filter: filter} }
func (s *PRSection) Kind() string           { return "pr" }
func (s *PRSection) Filter() string         { return s.filter }
func (s *PRSection) SetPRs(p []gh.PR)        { s.prs = p; s.shown = allIdx(len(p)) }
func (s *PRSection) Len() int               { return len(s.shown) }
func (s *PRSection) SetShown(idx []int)      { s.shown = idx }

func (s *PRSection) Columns() []table.Column {
	return []table.Column{{Title: "#", Width: 6}, {Title: "Title", Width: 50},
		{Title: "Author", Width: 14}, {Title: "CI", Width: 8}}
}
func (s *PRSection) Rows() []table.Row {
	rows := make([]table.Row, 0, len(s.shown))
	for _, i := range s.shown {
		p := s.prs[i]
		rows = append(rows, table.Row{fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login, p.CIState()})
	}
	return rows
}
func (s *PRSection) VarsAt(i int) action.Vars {
	p := s.prs[s.shown[i]]
	return action.Vars{Number: p.Number, Title: p.Title, HeadRefName: p.HeadRefName,
		BaseRefName: p.BaseRefName, URL: p.URL, Author: p.Author.Login, Branch: p.HeadRefName}
}
func (s *PRSection) Haystacks() []string {
	h := make([]string, len(s.prs))
	for i, p := range s.prs {
		h[i] = haystack(p)
	}
	return h
}

// --- Issue section ---
type IssueSection struct {
	filter string
	issues []gh.Issue
	shown  []int
}

func NewIssueSection(filter string) *IssueSection { return &IssueSection{filter: filter} }
func (s *IssueSection) Kind() string              { return "issue" }
func (s *IssueSection) Filter() string            { return s.filter }
func (s *IssueSection) SetIssues(is []gh.Issue)   { s.issues = is; s.shown = allIdx(len(is)) }
func (s *IssueSection) Len() int                  { return len(s.shown) }
func (s *IssueSection) SetShown(idx []int)         { s.shown = idx }

func (s *IssueSection) Columns() []table.Column {
	return []table.Column{{Title: "#", Width: 6}, {Title: "Title", Width: 50},
		{Title: "Author", Width: 14}, {Title: "Labels", Width: 20}}
}
func (s *IssueSection) Rows() []table.Row {
	rows := make([]table.Row, 0, len(s.shown))
	for _, i := range s.shown {
		is := s.issues[i]
		rows = append(rows, table.Row{fmt.Sprintf("#%d", is.Number), is.Title, is.Author.Login, labelNames(is.Labels)})
	}
	return rows
}
func (s *IssueSection) VarsAt(i int) action.Vars {
	is := s.issues[s.shown[i]]
	return action.Vars{Number: is.Number, Title: is.Title, Author: is.Author.Login,
		URL: is.URL, Branch: issue.Branch(is.Number, is.Title, labelSlice(is.Labels))}
}
func (s *IssueSection) Haystacks() []string {
	h := make([]string, len(s.issues))
	for i, is := range s.issues {
		h[i] = fmt.Sprintf("#%d %s %s %s", is.Number, is.Title, is.Author.Login, labelNames(is.Labels))
	}
	return h
}

func allIdx(n int) []int { r := make([]int, n); for i := range r { r[i] = i }; return r }
func labelNames(ls []gh.Label) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return joinSpace(out)
}
func labelSlice(ls []gh.Label) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}
func joinSpace(s []string) string { return fmt.Sprint(s) } // simple; refine rendering later
```

(Refactor `Model` to hold `section Section`, and route `setPRs`/`applyFilter`/`cursorPR` through it. The fuzzy filter now uses `section.Haystacks()` + `section.SetShown(matchedIdx)`.)

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `refactor(ui): Section interface; add issue section`.

---

## Task 4: Multi-select

**Files:** Create `internal/ui/select.go`, `internal/ui/select_test.go`

- [ ] **Step 1: Failing test**

```go
package ui

import "testing"

func TestSelectionToggle(t *testing.T) {
	s := selection{}
	s.toggle(2)
	s.toggle(5)
	s.toggle(2) // off again
	if s.has(2) || !s.has(5) {
		t.Fatalf("selection state wrong: %+v", s.set)
	}
	if n := s.count(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `internal/ui/select.go`**

```go
package ui

type selection struct{ set map[int]bool }

func (s *selection) toggle(i int) {
	if s.set == nil {
		s.set = map[int]bool{}
	}
	if s.set[i] {
		delete(s.set, i)
	} else {
		s.set[i] = true
	}
}
func (s *selection) has(i int) bool { return s.set[i] }
func (s *selection) count() int     { return len(s.set) }
func (s *selection) indices() []int {
	out := make([]int, 0, len(s.set))
	for i := range s.set {
		out = append(out, i)
	}
	return out
}
func (s *selection) clear() { s.set = nil }
```

Wire `space` (toggle current cursor row) and `V` (select all shown) in `Update`; mark selected rows in `Rows()` (e.g. a leading `●`).

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(ui): multi-select`.

---

## Task 5: Bulk fan-out action `W`

**Files:** Modify `internal/ui/actions.go`, `internal/ui/actions_test.go`

- [ ] **Step 1: Failing test (per-selected writes one handoff line per item)**

```go
func TestBulkWritesPerItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "actions")
	t.Setenv("PRDASH_ACTION_FILE", p)
	m := NewModel("/repo", "is:open", nil) // PR section
	sec := NewPRSection("is:open")
	sec.SetPRs([]gh.PR{{Number: 7}, {Number: 9}, {Number: 11}})
	m.section = sec
	m.sel.toggle(0)
	m.sel.toggle(2)

	a := action.Action{Key: "W", Command: action.Command{Argv: []string{"wt", "switch", "pr:{{.Number}}"}}, ExitsTUI: true, Scope: "per-selected"}
	quit := m.runBulk(a)
	if quit == nil {
		t.Fatal("bulk exits-tui must quit")
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Fatalf("want 2 handoff lines, got %d: %q", n, b)
	}
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `runBulk` in `internal/ui/actions.go`**

```go
// runBulk applies a per-selected action to each selected row (or the cursor row
// if none selected), writing one handoff line each, then quits if exits-tui.
func (m *Model) runBulk(a action.Action) tea.Cmd {
	idx := m.sel.indices()
	if len(idx) == 0 {
		idx = []int{m.table.Cursor()}
	}
	path := os.Getenv("PRDASH_ACTION_FILE")
	for _, i := range idx {
		v := m.section.VarsAt(i)
		argv, err := a.ExpandArgv(v)
		if err != nil {
			m.err = err
			continue
		}
		if a.ExitsTUI && path != "" {
			_ = action.AppendHandoff(path, a.Key, argv)
		}
	}
	if a.ExitsTUI {
		return tea.Quit
	}
	return nil
}
```

Add `W` to `DefaultPRActions` and `DefaultIssueActions` with `Scope: "per-selected"`; dispatch `scope == "per-selected"` to `runBulk` in `Update`.

- [ ] **Step 4: Run, verify pass. Step 5: Commit** — `feat(ui): bulk worktree fan-out (W)`.

---

## Task 6: Idempotent orchestrator

**Files:** Modify `lazytmux/scripts/prdash-apply.sh`

- [ ] **Step 1: Harden worktree creation**

`wt switch -c <branch>` errors if the branch/worktree exists. Make it reuse:

```bash
ensure_worktree() {
	# args after "switch": either ["pr:N"] or ["-c","<branch>"]
	if [[ ${1:-} == "-c" ]]; then
		local branch="$2"
		if "$WT" list 2>/dev/null | grep -qw "$branch"; then
			"$WT" switch "$branch" # reuse
		else
			"$WT" switch -c "$branch"
		fi
	else
		"$WT" switch "$1" # pr:N — worktrunk handles existing
	fi
}
```

Call `ensure_worktree "${argv[@]:1}"` instead of `"$WT" "${argv[@]:1}"` in the loop. For bulk, create windows detached so focus doesn't bounce across N (lazytmux post-switch hook detail; land on the first).

- [ ] **Step 2: Shellcheck**

Run: `shellcheck lazytmux/scripts/prdash-apply.sh` → clean.

- [ ] **Step 3: Manual: select 2 issues → `W` → 2 windows; re-run → reuse, no error.**

- [ ] **Step 4: Commit (lazytmux)** — `fix: idempotent prdash worktree orchestration`.

---

## Self-review
- **Spec coverage:** issue fetch ✓ T1, branch derivation (type/label/slug) ✓ T2, Section seam + issue kind ✓ T3, multi-select ✓ T4, bulk `W` per-selected → handoff lines ✓ T5, idempotent orchestrator ✓ T6. cwd-scope inherited from Plan 1.
- **Types:** `gh.Issue/IssueListArgs/ParseIssues`, `issue.Branch`, `ui.Section/PRSection/IssueSection/selection/runBulk` consistent with Plans 1–2.
- **Placeholders:** none. (`joinSpace` is a deliberately simple label-join, refined in Plan 4's rendering.)
