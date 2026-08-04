# Board + Preview Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the PR board a single dense line carrying state and identity, move labels and branch into the preview, add diffstat and ticket-id columns, and make row order stable across refreshes.

**Architecture:** `TwoLine` is deleted outright, so `renderItemRow` collapses to one code path. Two new columns (diffstat, ticket id) and one relocated indicator (draft, into gutter cell 1) are added to that path. Ordering changes by swapping `groupByAuthor`'s cluster key from "best member rank" to "highest PR number" and composing it inside `groupByCategory`. A responsive ladder on `Layout` sheds columns by list width, extending the mechanism `computeLayout` already uses for panes.

**Tech Stack:** Go 1.24, charmbracelet/bubbletea v2, lipgloss v2, shurcooL/githubv4, stdlib `testing`.

Spec: `docs/superpowers/specs/2026-08-04-board-preview-cleanup-design.md`
Issue: #88 · Closes #62

## Global Constraints

- `schemaVer` in `internal/ui/prlist.go` MUST go from `"v4"` to `"v5"` in Task 1. Cached `[]PR` JSON lacks the new fields; without the bump, cached lists paint `+0 -0`.
- Never query a GraphQL **connection** for this work. Only leaf scalars on the search node are free. `additions`, `deletions`, `changedFiles`, `stack`, `stackEntry` are all leaves or objects-with-scalar-leaves.
- Nerd Font glyphs are declared as named constants with a `// nerd: nf-...` comment naming the intended icon. Do NOT invent glyph codepoints — use the placeholder given in the task and leave the comment; the operator sets the real value.
- The **title column and the glyph gutter never shed** under the responsive ladder.
- Row rendering tests are differential, not golden (see the comment at the top of `internal/ui/gridhints_test.go`). Assert properties, not frozen strings.
- Run `go test ./...` from the repo root before every commit.
- Commit messages use Conventional Commits with a scope, e.g. `feat(ui):`, `refactor(ui):`, `feat(gh):`.

---

### Task 1: List-query fields + cache schema bump

**Files:**
- Modify: `internal/gh/graphql.go` (`qlPR` struct ~line 63, `mapPR` ~line 150)
- Modify: `internal/gh/prs.go` (`PR` struct ~line 70)
- Modify: `internal/ui/prlist.go` (`schemaVer` ~line 2701)
- Test: `internal/gh/prs_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `gh.PR.Additions int`, `gh.PR.Deletions int`, `gh.PR.ChangedFiles int`, `gh.PR.Stack *gh.PRStack`, `gh.PR.StackPosition int`. `gh.PRStack` is `struct { Number int; Size int; BaseRefName string }`. Tasks 5 and #89's plan consume these.

- [ ] **Step 1: Write the failing test**

Add to `internal/gh/prs_test.go`:

```go
func TestMapPRCopiesDiffstatAndStack(t *testing.T) {
	g := qlPR{
		Number:       3065,
		Additions:    412,
		Deletions:    18,
		ChangedFiles: 7,
	}
	g.Stack = &qlStack{Number: 3074, Size: 2, BaseRefName: "main"}
	g.StackEntry = &qlStackEntry{Position: 1}

	p := mapPR(g)

	if p.Additions != 412 || p.Deletions != 18 || p.ChangedFiles != 7 {
		t.Fatalf("diffstat not copied: +%d -%d files=%d", p.Additions, p.Deletions, p.ChangedFiles)
	}
	if p.Stack == nil {
		t.Fatal("Stack not copied")
	}
	if p.Stack.Number != 3074 || p.Stack.Size != 2 || p.Stack.BaseRefName != "main" {
		t.Fatalf("stack wrong: %+v", *p.Stack)
	}
	if p.StackPosition != 1 {
		t.Fatalf("StackPosition = %d, want 1", p.StackPosition)
	}
}

func TestMapPRUnstackedLeavesStackNil(t *testing.T) {
	p := mapPR(qlPR{Number: 3086})
	if p.Stack != nil {
		t.Fatalf("Stack = %+v, want nil for an unstacked PR", p.Stack)
	}
	if p.StackPosition != 0 {
		t.Fatalf("StackPosition = %d, want 0", p.StackPosition)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gh/ -run 'TestMapPR(CopiesDiffstatAndStack|UnstackedLeavesStackNil)' -v`
Expected: FAIL to compile — `unknown field Additions in struct literal of type qlPR`, `undefined: qlStack`.

- [ ] **Step 3: Add the GraphQL field types**

In `internal/gh/graphql.go`, add these two types immediately above `type qlPR struct`:

```go
// qlStack / qlStackEntry are GitHub's stacked-PR fields. Both are OBJECT fields
// whose selected members are scalars — not connections — so they add no GraphQL
// cost. Deliberately omits stack.entries, which IS a connection and is
// unnecessary: every open member is already in the same search result.
type qlStack struct {
	Number      int
	Size        int
	BaseRefName string
}

type qlStackEntry struct {
	Position int
}
```

Then add these fields inside `qlPR`, after `ReviewDecision`:

```go
	Additions      int
	Deletions      int
	ChangedFiles   int
	Stack          *qlStack
	StackEntry     *qlStackEntry
```

- [ ] **Step 4: Add the domain fields**

In `internal/gh/prs.go`, add above `type PR struct`:

```go
// PRStack is the stack a PR belongs to. Number is drawn from the repo's shared
// issue/PR sequence but resolves to neither a PR nor an issue and has no url, so
// it is an internal identity only — never render it as "#N".
type PRStack struct {
	Number      int    `json:"number"`
	Size        int    `json:"size"`
	BaseRefName string `json:"baseRefName"`
}
```

Add these fields to `PR`, after `AutoMergeRequest`:

```go
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changedFiles"`
	// Stack is nil unless the PR is part of a GitHub stack. StackPosition is
	// 1-based within that stack and 0 when Stack is nil.
	Stack         *PRStack `json:"stack"`
	StackPosition int      `json:"stackPosition"`
```

- [ ] **Step 5: Copy them in `mapPR`**

In `internal/gh/graphql.go`, inside `mapPR`, add to the `PR{...}` literal after `UpdatedAt`:

```go
		Additions:      g.Additions,
		Deletions:      g.Deletions,
		ChangedFiles:   g.ChangedFiles,
```

And after the `AutoMergeRequest` block:

```go
	if g.Stack != nil {
		p.Stack = &PRStack{Number: g.Stack.Number, Size: g.Stack.Size, BaseRefName: g.Stack.BaseRefName}
	}
	if g.StackEntry != nil {
		p.StackPosition = g.StackEntry.Position
	}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/gh/ -run 'TestMapPR' -v`
Expected: PASS

- [ ] **Step 7: Bump the cache schema**

In `internal/ui/prlist.go` change:

```go
const schemaVer = "v4"
```

to:

```go
// v5 adds additions/deletions/changedFiles and the stack fields; a v4 cache
// entry has none of them and would paint "+0 -0".
const schemaVer = "v5"
```

- [ ] **Step 8: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/gh/graphql.go internal/gh/prs.go internal/gh/prs_test.go internal/ui/prlist.go
git commit -m "feat(gh): fetch diffstat and stack fields on the list query

Leaf scalars on the search node, so GraphQL cost is unchanged (measured
at 1 point for a 60-PR page). Bumps schemaVer v4 to v5 so cached lists
don't paint +0 -0.

Refs #88"
```

---

### Task 2: Ticket-id parser

**Files:**
- Create: `internal/ui/ticket.go`
- Test: `internal/ui/ticket_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func ticketID(branch string) string` — returns `"ENG-7726"`, `"#213"`, or `""`. Task 6 renders it.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/ticket_test.go`:

```go
package ui

import "testing"

func TestTicketID(t *testing.T) {
	for _, tc := range []struct{ branch, want string }{
		// Linear shape: team prefix, digits, description.
		{"eng-7726-same-value-different-evidence", "ENG-7726"},
		{"eng-7452-share-one-archer-conversation", "ENG-7452"},
		{"ops-12-rotate-keys", "OPS-12"},
		// GitHub shape: commit type, slash, issue number, description.
		{"feat/213-id-seed-avatars", "#213"},
		{"fix/208-widget-warm-deeplink", "#208"},
		{"chore/220-bump-expo-54", "#220"},
		// No id at all.
		{"agents/spicedb-rel-migrate-88ee", ""},
		{"cursor/guidance-drift-review", ""},
		{"chore-slim-agent-instructions", ""},
		{"main", ""},
		{"", ""},
		// TRAP 1: looks like a Linear key but has no number. A pattern without
		// \d+ emits a bogus "ENG-EMMETT".
		{"eng-emmett-graph-assurance", ""},
		// TRAP 2: a commit-type prefix with no slash. Without the denylist this
		// parses as "FIX-123" for a branch that references no ticket.
		{"fix-123-typo", ""},
		{"feat-42-add-thing", ""},
		{"chore-9-bump", ""},
		// Team prefix longer than 6 chars is not a Linear key.
		{"platform-12-thing", ""},
	} {
		if got := ticketID(tc.branch); got != tc.want {
			t.Errorf("ticketID(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestTicketID -v`
Expected: FAIL to compile — `undefined: ticketID`.

- [ ] **Step 3: Write the implementation**

Create `internal/ui/ticket.go`:

```go
package ui

import (
	"regexp"
	"strings"
)

// Branch shapes this machine actually produces (see AGENTS.shared.md): repos
// tracking work in Linear use Linear's generated name, personal repos use
// type/id-desc. Nothing else is recognised.
var (
	ghBranchRe     = regexp.MustCompile(`^(?:feat|fix|refactor|chore|docs)/(\d+)-`)
	linearBranchRe = regexp.MustCompile(`^([a-z]{2,6})-(\d+)-`)
)

// commitTypeWords are branch prefixes that look like a Linear team key but are
// conventional-commit types. Without this, "fix-123-typo" — a branch naming no
// ticket — parses as "FIX-123".
var commitTypeWords = map[string]bool{
	"feat": true, "fix": true, "chore": true, "docs": true, "refactor": true,
	"perf": true, "test": true, "build": true, "ci": true, "style": true,
	"revert": true,
}

// ticketID extracts a ticket reference from a head branch name, or "" when the
// branch names none — which is common: agent branches (agents/…, cursor/…) carry
// no id by construction.
func ticketID(branch string) string {
	if m := ghBranchRe.FindStringSubmatch(branch); m != nil {
		return "#" + m[1]
	}
	m := linearBranchRe.FindStringSubmatch(branch)
	if m == nil || commitTypeWords[m[1]] {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestTicketID -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ticket.go internal/ui/ticket_test.go
git commit -m "feat(ui): parse ticket ids out of head branch names

Two shapes, from this machine's documented branch conventions. Requires
digits so eng-emmett-* yields nothing, and denies commit-type words so
fix-123-typo isn't read as FIX-123.

Refs #88"
```

---

### Task 3: Delete `TwoLine`

**Files:**
- Modify: `internal/ui/section.go` (`RowOpts` ~line 17, `RenderRow` ~line 123, `renderItemRow` ~line 385-515)
- Modify: `internal/ui/layout.go` (`Layout` ~line 44, `computeLayout` ~line 79-83, `twoLineMinRows` ~line 26)
- Modify: `internal/ui/prlist.go` (`rowKey` ~line 565, `renderList` ~line 616-620)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `RowOpts` without `TwoLine`; `renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review, auto, sub string, labels []gh.Label) string` keeps its signature this task (params go unused; Task 5 and 6 rewrite it). `Layout` without `TwoLine`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestRowIsAlwaysOneLine(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{
		Number: 3065, Title: "feat(infra): add deploy-time SpiceDB migrator",
		HeadRefName: "eng-7726-thing", State: "OPEN",
		Labels: []gh.Label{{Name: "complexity:6", Color: "fab387"}, {Name: "preview:full", Color: "a6adc8"}},
	}})
	s.prs[0].Author.Login = "asaf-s-factify"
	s.SetShown([]int{0})

	row := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if strings.Contains(row, "\n") {
		t.Fatalf("row spans multiple lines:\n%s", row)
	}
	if strings.Contains(row, "complexity:6") {
		t.Error("labels must not appear on the row — they belong to the preview")
	}
	if strings.Contains(row, "eng-7726-thing") {
		t.Error("head branch must not appear on the row — it belongs to the preview")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRowIsAlwaysOneLine -v`
Expected: FAIL — the row contains a newline and the label/branch text, because `RowOpts{}` zero-values `TwoLine` to false but `RenderRow` still passes labels through and `renderItemRow` still has the two-line branch. (If it passes because `TwoLine` defaulted false, still proceed — the deletions below are what make the single path unconditional.)

- [ ] **Step 3: Remove `TwoLine` from `RowOpts`**

In `internal/ui/section.go`, delete this field from `RowOpts`:

```go
	TwoLine   bool   // render labels + branch on an indented second line
```

- [ ] **Step 4: Collapse `renderItemRow` to one path**

In `internal/ui/section.go`, replace the whole body from the `// Two-line rows lead line 2 with the author; single-line keeps it beside age.` comment through the final `return line1 + "\n" + line2` with:

```go
	right := authorStyle(author).Render(author) + dimStyle.Render(fmt.Sprintf("  %3s", age))
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)

	titleRoom := w - leftW - rightW - 2 // -2: title/right separators
	if titleRoom < 1 {
		titleRoom = 1
	}
	titleSt := titleStyle
	switch {
	case o.Focused:
		titleSt = titleSt.Bold(true) // the hovered row is always readable, even if draft
	case o.Draft:
		titleSt = dimStyle
	}
	// Without the tag, a merge glyph on the open board reads as a live PR.
	tags := ""
	if o.Landed {
		const tag = " landed"
		tags += dimStyle.Render(tag)
		if titleRoom -= lipgloss.Width(tag); titleRoom < 1 {
			titleRoom = 1
		}
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom)) + tags

	gap := w - leftW - lipgloss.Width(titleTxt) - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + titleTxt + strings.Repeat(" ", gap) + right
	if o.Focused {
		line = rowBgWrap(line, theme.RowBg)
	}
	return line
```

Note the `[draft]` tag block is deleted here — Task 4 moves draft into the gutter.

- [ ] **Step 5: Reserve the tree column**

The tree column sits **after** the glyph gutter and before the number. #89 fills
it; here it renders blank. Reserving it now keeps #89 from shifting every
responsive breakpoint in Task 8.

Add to `RowOpts` in `internal/ui/section.go`:

```go
	Tree string // stack chain glyph, rendered between the gutter and the number
```

In `renderItemRow`, change the `left` construction (currently
`left := gutter + numStyle.Render(numCell) + " "`) to:

```go
	// Tree slot: after the state glyphs, so a stacked row's ci/rv/auto/flag stay
	// on the same columns as every other row's.
	const treeW = 3
	tree := o.Tree
	if pad := treeW - lipgloss.Width(tree); pad > 0 {
		tree += strings.Repeat(" ", pad)
	}
	left := gutter + tree + numStyle.Render(numCell) + " "
```

- [ ] **Step 6: Drop the now-unused params**

Change the signature to drop `sub` and `labels`, and update its doc comment:

```go
// renderItemRow renders one dense row:
//
//   ‹bar› ‹ci› ‹rv› ‹auto› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review, auto string) string {
```

In `RenderRow` (`section.go` ~line 146-152) delete the `byAuthor` gate and always pass the author:

```go
	author := p.Author.Login
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title,
		author, age, status, review, auto)
```

Update `IssueSection.RenderRow` (~line 333) the same way — drop its label and sub arguments.

- [ ] **Step 7: Remove `TwoLine` from `Layout`**

In `internal/ui/layout.go`: delete the `TwoLine bool` field, delete the `twoLineMinRows` const and its comment, delete the `twoLine := ch >= twoLineMinRows` line, and remove `TwoLine: twoLine` from both `Layout{...}` returns.

- [ ] **Step 8: Remove it from the row cache**

In `internal/ui/prlist.go`: delete `twoLine bool` from `rowKey`, and remove `twoLine: l.TwoLine,` from the `key := rowKey{...}` literal and `TwoLine: l.TwoLine,` from the `RowOpts{...}` literal in `renderList`.

- [ ] **Step 10: Run the full suite and fix fallout**

Run: `go test ./internal/ui/ 2>&1 | head -40`
Expected: compile errors in tests that set `TwoLine:` or assert two-line output. Delete those assertions and the `TwoLine:` fields. `renderChips` and `labelSlice` may become unused — if the compiler or `deadnix` flags them, delete `renderChips`; keep `labelSlice`/`labelNames` if the preview still uses them.

Run again: `go test ./...`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/ui/
git commit -m "refactor(ui): delete TwoLine, rows are always one line

Labels and branch move to the preview, so line 2 had no content left.
Removes the field from RowOpts, Layout and rowKey plus the second-line
branch of renderItemRow.

Refs #88"
```

---

### Task 4: Draft takes gutter cell 1

**Files:**
- Modify: `internal/ui/theme.go` (near `mergedGlyph` ~line 354)
- Modify: `internal/ui/section.go` (`RenderRow` ~line 129-136)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `RowOpts` from Task 3.
- Produces: `func draftMark() string`, mirroring the existing `mergedMark()` / `closedMark()`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestDraftOverridesCIGlyphNotTitle(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{{
		Number: 3083, Title: "fix(services): canonicalize CLW JSON values",
		State: "OPEN", IsDraft: true,
		StatusCheckRollup: []gh.Check{{Name: "build", State: "SUCCESS", Conclusion: "SUCCESS"}},
	}})
	s.prs[0].Author.Login = "noamsto"
	s.SetShown([]int{0})

	row := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5})
	if strings.Contains(row, "[draft]") {
		t.Error("draft must not render as a trailing tag")
	}
	if !strings.Contains(row, draftGlyph) {
		t.Errorf("draft glyph missing from the gutter:\n%s", row)
	}
	// The draft glyph replaces the CI mark rather than joining it.
	if strings.Contains(row, ciGlyph("pass")) {
		t.Error("draft row still shows a CI glyph; it should be overridden")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestDraftOverridesCIGlyphNotTitle -v`
Expected: FAIL to compile — `undefined: draftGlyph`.

- [ ] **Step 3: Add the glyph and mark**

In `internal/ui/theme.go`, immediately after the `closedGlyph` declaration:

```go
// draftGlyph marks a draft PR in gutter cell 1, overriding the CI mark the same
// way mergedGlyph/closedGlyph do for terminal states.
const draftGlyph = "◌" // nerd: nf-oct-git_pull_request_draft

// draftMark is the dim draft indicator. Dim, not peach: the row as a whole is
// receding, and cell 1 is a state column rather than an accent.
func draftMark() string { return dimStyle.Render(draftGlyph) }
```

- [ ] **Step 4: Override cell 1 for drafts**

In `internal/ui/section.go`, extend the `switch` in `RenderRow`. Draft is checked **last** so a merged or closed draft still reads as terminal:

```go
	switch {
	case p.IsMerged():
		status, age = mergedMark(), ageString(p.MergedAt)
	case p.State == "CLOSED":
		status, age = closedMark(), ageString(p.ClosedAt)
	case p.IsDraft:
		// A draft's CI rollup is the least interesting thing about it, and every
		// other indicator on this row is a gutter glyph. Costs the CI mark; the
		// preview still has it.
		status = draftMark()
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestDraftOverridesCIGlyphNotTitle -v`
Expected: PASS

- [ ] **Step 6: Update the legend**

In `internal/ui/prlist.go`, find `legendGroups` and add `draftGlyph` to the same group as the CI marks, labelled `draft`. Adjacent cleanup for #72: confirm `✗` is not listed twice with the same label; if it is, label the CI one `checks failed` and the review one `changes requested`.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/ui/theme.go internal/ui/section.go internal/ui/section_test.go internal/ui/prlist.go
git commit -m "feat(ui): move the draft marker into gutter cell 1

Overrides the CI glyph, matching how merged/closed already work, so
every indicator on the row is a left-gutter glyph. Also disambiguates
the two legend entries for the x mark.

Refs #88, refs #72"
```

---

### Task 5: Diffstat column

**Files:**
- Modify: `internal/ui/section.go` (`renderItemRow`, `columnWidths` ~line 539)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `gh.PR.Additions` / `Deletions` from Task 1; `renderItemRow` from Task 3.
- Produces: `func diffstat(add, del int) string`, `func diffstatWidth(s Section) int`. `renderItemRow` gains a `diff string` parameter and `RowOpts` gains `DiffWidth int`.

- [ ] **Step 1: Write the failing test**

```go
func TestDiffstatFormatting(t *testing.T) {
	for _, tc := range []struct{ add, del int; want string }{
		{412, 18, "+412 -18"},
		{0, 0, "+0 -0"},
		{1600, 63, "+1.6k -63"},
		{1000, 999, "+1k -999"},
		{12345, 2000, "+12.3k -2k"},
	} {
		got := stripANSIForTest(diffstat(tc.add, tc.del))
		if got != tc.want {
			t.Errorf("diffstat(%d,%d) = %q, want %q", tc.add, tc.del, got, tc.want)
		}
	}
}

func TestDiffstatColumnWidthIsStableAcrossRows(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{
		{Number: 3087, Title: "big", State: "OPEN", Additions: 1600, Deletions: 63},
		{Number: 3084, Title: "small", State: "OPEN", Additions: 31, Deletions: 4},
	})
	s.prs[0].Author.Login = "noamsto"
	s.prs[1].Author.Login = "rubytify"
	s.SetShown([]int{0, 1})

	dw := diffstatWidth(s)
	a := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5, DiffWidth: dw})
	b := s.RenderRow(1, RowOpts{Width: 120, NumWidth: 5, DiffWidth: dw})
	if lipgloss.Width(a) != lipgloss.Width(b) {
		t.Fatalf("rows differ in width: %d vs %d", lipgloss.Width(a), lipgloss.Width(b))
	}
	// The age column is the rightmost thing; it must start at the same offset.
	if idxFromRight(a, "1m") != idxFromRight(b, "10m") {
		t.Error("age column shifted between rows — diffstat width is not fixed")
	}
}
```

If `stripANSIForTest` and `idxFromRight` do not already exist in the package's test files, add them to `internal/ui/section_test.go`:

```go
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSIForTest(s string) string { return ansiRe.ReplaceAllString(s, "") }

// idxFromRight reports how many display cells sit between the end of sub and the
// end of the line, or -1 when sub is absent.
func idxFromRight(line, sub string) int {
	plain := stripANSIForTest(line)
	i := strings.LastIndex(plain, sub)
	if i < 0 {
		return -1
	}
	return len(plain) - (i + len(sub))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestDiffstat' -v`
Expected: FAIL to compile — `undefined: diffstat`, `unknown field DiffWidth`.

- [ ] **Step 3: Implement the formatter**

Add to `internal/ui/section.go`:

```go
// abbrevCount renders a change count, shortening at 1000 so a 5-digit diff can't
// blow the column: 999 -> "999", 1000 -> "1k", 1600 -> "1.6k".
func abbrevCount(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	v := float64(n) / 1000
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + "k"
}

// diffstat renders "+412 -18" with colour on the numbers only — the signs and
// the space stay unstyled so the pair reads as one value.
func diffstat(add, del int) string {
	return passStyle.Render("+"+abbrevCount(add)) + " " + failStyle.Render("-"+abbrevCount(del))
}

// diffstatWidth is the cell width of the diffstat column: the widest rendering
// across the shown set, so the age column never shifts between rows.
func diffstatWidth(s Section) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	w := 0
	for _, i := range ps.shown {
		w = max(w, lipgloss.Width(diffstat(ps.prs[i].Additions, ps.prs[i].Deletions)))
	}
	return w
}
```

Add `"strconv"` to the file's imports.

- [ ] **Step 4: Render it**

Add to `RowOpts` in `internal/ui/section.go`:

```go
	DiffWidth int // cell width of the diffstat column; 0 hides it
```

In `renderItemRow`, add a `diff string` parameter after `age`, and build `right` from it. Replace the `right :=` line from Task 3 with:

```go
	right := authorStyle(author).Render(author)
	if o.DiffWidth > 0 {
		pad := o.DiffWidth - lipgloss.Width(diff)
		if pad < 0 {
			pad = 0
		}
		right += "  " + strings.Repeat(" ", pad) + diff // right-aligned in a fixed column
	}
	right += dimStyle.Render(fmt.Sprintf("  %3s", age))
```

In `PRSection.RenderRow`, pass it:

```go
	diff := ""
	if o.DiffWidth > 0 {
		diff = diffstat(p.Additions, p.Deletions)
	}
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title,
		author, age, diff, status, review, auto)
```

`IssueSection.RenderRow` passes `""` for `diff`.

- [ ] **Step 5: Wire the width through `renderList`**

In `internal/ui/prlist.go` `renderList`, after `numW := columnWidths(m.section)`:

```go
	diffW := diffstatWidth(m.section)
```

Add `diffW` to `rowKey` (as `diffW int`) and to both the `key := rowKey{...}` and `RowOpts{...}` literals.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestDiffstat' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go internal/ui/prlist.go
git commit -m "feat(ui): show the diffstat on each board row

Fixed-width column computed across the shown set so the age column
never shifts. Abbreviates at 1k; colour on the numbers only.

Refs #88"
```

---

### Task 6: Ticket-id column after the title

**Files:**
- Modify: `internal/ui/section.go` (`renderItemRow`, `RowOpts`)
- Modify: `internal/ui/prlist.go` (`renderList`, `rowKey`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `ticketID` (Task 2), `renderItemRow` (Task 5).
- Produces: `RowOpts.TicketWidth int`; `func ticketWidth(s Section) int`.

- [ ] **Step 1: Write the failing test**

```go
func TestTicketColumnSitsAfterTitleAndBlanksDontShiftAge(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{
		{Number: 3087, Title: "has a ticket", State: "OPEN", HeadRefName: "eng-7726-x"},
		{Number: 3065, Title: "has none", State: "OPEN", HeadRefName: "agents/spicedb-rel-migrate-88ee"},
	})
	s.prs[0].Author.Login = "noamsto"
	s.prs[1].Author.Login = "asaf-s-factify"
	s.SetShown([]int{0, 1})

	tw := ticketWidth(s)
	if tw < len("ENG-7726") {
		t.Fatalf("ticketWidth = %d, too narrow for ENG-7726", tw)
	}
	withID := s.RenderRow(0, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})
	without := s.RenderRow(1, RowOpts{Width: 120, NumWidth: 5, TicketWidth: tw})

	if !strings.Contains(stripANSIForTest(withID), "ENG-7726") {
		t.Errorf("ticket id missing:\n%s", withID)
	}
	// The id follows the title, so the number stays hard against the gutter.
	plain := stripANSIForTest(withID)
	if strings.Index(plain, "ENG-7726") < strings.Index(plain, "has a ticket") {
		t.Error("ticket id renders before the title; it must follow it")
	}
	if lipgloss.Width(withID) != lipgloss.Width(without) {
		t.Error("a blank ticket changes the row width")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestTicketColumn -v`
Expected: FAIL to compile — `unknown field TicketWidth`, `undefined: ticketWidth`.

- [ ] **Step 3: Implement the width helper**

Add to `internal/ui/section.go`:

```go
// ticketWidth is the cell width of the ticket column: the widest parsed id
// across the shown set, or 0 when none parse. Blank cells are common —
// agents/… and cursor/… branches carry no id — so the column sits after the
// title, where a gap lands against ragged text instead of reading as a hole.
func ticketWidth(s Section) int {
	ps, ok := s.(*PRSection)
	if !ok {
		return 0
	}
	w := 0
	for _, i := range ps.shown {
		w = max(w, len(ticketID(ps.prs[i].HeadRefName)))
	}
	return w
}
```

- [ ] **Step 4: Render it**

Add to `RowOpts`:

```go
	TicketWidth int // cell width of the ticket-id column; 0 hides it
```

In `renderItemRow`, add a `ticket string` parameter after `title`, and prepend it to `right` (so it sits between title and author):

```go
	right := ""
	if o.TicketWidth > 0 {
		right += sectionLabelStyle.Render(ticket) +
			strings.Repeat(" ", o.TicketWidth-lipgloss.Width(ticket)) + "  "
	}
	right += authorStyle(author).Render(author)
```

In `PRSection.RenderRow`:

```go
	ticket := ""
	if o.TicketWidth > 0 {
		ticket = ticketID(p.HeadRefName)
	}
```

and pass it. `IssueSection.RenderRow` passes `""`.

- [ ] **Step 5: Wire it through `renderList`**

Add `tktW := ticketWidth(m.section)` beside `diffW`, add `tktW int` to `rowKey`, and pass `TicketWidth: tktW` in `RowOpts`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run TestTicketColumn -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go internal/ui/prlist.go
git commit -m "feat(ui): show the ticket id in a column after the title

After the title rather than before the number: blanks are common (4 of
14 sampled branches parse to nothing) and a gap against the title's
ragged right edge doesn't read as a hole.

Refs #88"
```

---

### Task 7: Author clusters ordered by PR number (closes #62)

**Files:**
- Modify: `internal/ui/section.go` (`groupByCategory` ~line 256, `groupByAuthor` ~line 282)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `groupByAuthor(prs []gh.PR, idx []int, state string) []int` with a number-based cluster key; `groupByCategory` now clusters within each category.

- [ ] **Step 1: Write the failing test**

```go
// The #62 regression: nothing may move because a check finished.
func TestOrderIsUnchangedByCIState(t *testing.T) {
	mk := func(ci string) []gh.PR {
		prs := []gh.PR{
			{Number: 3086, State: "OPEN", Title: "a"},
			{Number: 3071, State: "OPEN", Title: "b"},
			{Number: 3084, State: "OPEN", Title: "c"},
		}
		prs[0].Author.Login = "asaf-s-factify"
		prs[1].Author.Login = "asaf-s-factify"
		prs[2].Author.Login = "rubytify"
		// Flip the first PR's rollup between the two runs.
		prs[0].StatusCheckRollup = []gh.Check{{Name: "build", State: ci, Conclusion: ci}}
		return prs
	}
	order := func(prs []gh.PR) []int {
		s := NewPRSection("is:open")
		s.SetPRs(prs)
		out := []int{}
		for i := 0; i < s.Len(); i++ {
			out = append(out, s.prAt(i).Number)
		}
		return out
	}
	before := order(mk("PENDING"))
	after := order(mk("SUCCESS"))
	if !slices.Equal(before, after) {
		t.Fatalf("order changed when CI settled: %v -> %v", before, after)
	}
}

func TestClustersAreContiguousLedByHighestNumber(t *testing.T) {
	prs := []gh.PR{
		{Number: 3015, State: "OPEN"}, {Number: 3086, State: "OPEN"},
		{Number: 3084, State: "OPEN"}, {Number: 3071, State: "OPEN"},
	}
	prs[0].Author.Login = "asaf-s-factify"
	prs[1].Author.Login = "asaf-s-factify"
	prs[2].Author.Login = "rubytify"
	prs[3].Author.Login = "asaf-s-factify"

	s := NewPRSection("is:open")
	s.SetPRs(prs)
	got := []int{}
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	// asaf leads (3086 > 3084); within the cluster, number descending.
	want := []int{3086, 3071, 3015, 3084}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestDraftsSinkWithinACluster(t *testing.T) {
	prs := []gh.PR{
		{Number: 3090, State: "OPEN", IsDraft: true},
		{Number: 3021, State: "OPEN"},
	}
	prs[0].Author.Login = "rubytify"
	prs[1].Author.Login = "rubytify"
	s := NewPRSection("is:open")
	s.SetPRs(prs)
	if s.prAt(0).Number != 3021 {
		t.Fatalf("draft did not sink: leading PR is #%d", s.prAt(0).Number)
	}
}

func TestCategoriesClusterByAuthorInside(t *testing.T) {
	prs := []gh.PR{
		{Number: 3086, State: "OPEN"}, {Number: 3084, State: "OPEN"},
		{Number: 3071, State: "OPEN"},
	}
	prs[0].Author.Login = "asaf-s-factify"
	prs[1].Author.Login = "rubytify"
	prs[2].Author.Login = "asaf-s-factify"

	s := NewPRSection("is:open")
	cats := map[int]string{3086: "Review requested", 3084: "Review requested", 3071: "Review requested"}
	s.SetCategorized(prs, cats, []string{"Review requested"})

	got := []int{}
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	want := []int{3086, 3071, 3084} // asaf's two adjacent, then rubytify
	if !slices.Equal(got, want) {
		t.Fatalf("categorized order = %v, want %v — clusters must form inside a category", got, want)
	}
}
```

Add `"slices"` to the test file's imports if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestOrderIsUnchangedByCIState|TestClustersAre|TestDraftsSink|TestCategoriesCluster' -v`
Expected: FAIL — `TestOrderIsUnchangedByCIState` and `TestCategoriesClusterByAuthorInside` both fail, because `sortPRs` ranks by CI and `groupByCategory` never clusters.

- [ ] **Step 3: Sort by number, drafts last**

In `internal/ui/section.go`, replace the `default:` arm of `sortPRs`:

```go
	default:
		// Number descending, drafts last. NOT actionability: ranking by CI state
		// means a finishing check reorders rows under the cursor (#62). The
		// categories already carry coarse urgency, and the gutter glyphs carry
		// the rest.
		slices.SortStableFunc(prs, func(a, b gh.PR) int {
			if a.IsDraft != b.IsDraft {
				if a.IsDraft {
					return 1
				}
				return -1
			}
			return b.Number - a.Number
		})
	}
```

- [ ] **Step 4: Key clusters by highest number**

Replace the rank block inside `groupByAuthor` (the `if state != "merged" && state != "closed" {` body) with:

```go
	if state != "merged" && state != "closed" {
		// A cluster leads with its newest PR, so cluster position is fixed by
		// numbers that never change — the whole point of #62.
		best := map[string]int{}
		for a, g := range groups {
			for _, i := range g {
				best[a] = max(best[a], prs[i].Number)
			}
		}
		slices.SortStableFunc(authors, func(x, y string) int {
			if best[x] != best[y] {
				return best[y] - best[x] // descending
			}
			return strings.Compare(x, y)
		})
	}
```

Update the function's doc comment: replace "leads with each author's best (lowest) member rank" with "leads with each author's highest PR number".

- [ ] **Step 5: Cluster inside each category**

Replace `groupByCategory`:

```go
// groupByCategory reorders idx so rows cluster under their category in header
// order, and clusters by author within each category. Composing the two keeps
// one ordering concept rather than adding a third.
func groupByCategory(prs []gh.PR, idx []int, cats map[int]string, order []string, state string) []int {
	out := make([]int, 0, len(idx))
	for _, cat := range order {
		members := make([]int, 0, len(idx))
		for _, i := range idx {
			if cats[prs[i].Number] == cat {
				members = append(members, i)
			}
		}
		out = append(out, groupByAuthor(prs, members, state)...)
	}
	return out
}
```

Update the caller in `setShownOrdered`:

```go
	if len(s.catOrder) > 0 {
		s.grouped = true
		s.shown = groupByCategory(s.prs, idx, s.cats, s.catOrder, s.state)
		return
	}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestOrder|TestClusters|TestDrafts|TestCategories' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS. Existing tests asserting rank order will fail — update them to the number order; `prRank` stays (Task 4's draft demotion and `triage` still reference it) but is no longer the board's sort key.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "fix(ui): order the board by PR number so nothing moves on refresh

Clusters by author within each category, clusters led by their highest
number, drafts last. A finishing check no longer reorders rows under the
cursor. Trade: urgency is no longer visible in position, only in the
gutter glyphs.

Closes #62, refs #88"
```

---

### Task 8: Responsive column ladder

**Files:**
- Modify: `internal/ui/layout.go`
- Modify: `internal/ui/prlist.go` (`renderList`)
- Test: `internal/ui/layout_test.go`, `internal/ui/layout_sweep_regression_test.go`

**Interfaces:**
- Consumes: `Layout` (Task 3), `RowOpts` (Tasks 5-6).
- Produces: `Layout.ShowDiffstat bool`, `Layout.CompactDiffstat bool`, `Layout.ShowTicket bool`, `Layout.InitialsAuthor bool`; `func columnLadder(listCells int) Layout` mutating those four.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/layout_test.go`:

```go
func TestColumnLadder(t *testing.T) {
	for _, tc := range []struct {
		cells                                  int
		diff, compact, ticket, initials        bool
	}{
		{120, true, false, true, false},
		{92, true, false, true, false},
		{91, true, true, true, false},
		{80, true, true, true, false},
		{79, true, true, true, true},
		{70, true, true, true, true},
		{69, true, true, false, true},
		{62, true, true, false, true},
		{61, false, true, false, true},
		{40, false, true, false, true},
	} {
		l := columnLadder(tc.cells)
		if l.ShowDiffstat != tc.diff || l.CompactDiffstat != tc.compact ||
			l.ShowTicket != tc.ticket || l.InitialsAuthor != tc.initials {
			t.Errorf("columnLadder(%d) = diff:%v compact:%v ticket:%v initials:%v, want %v %v %v %v",
				tc.cells, l.ShowDiffstat, l.CompactDiffstat, l.ShowTicket, l.InitialsAuthor,
				tc.diff, tc.compact, tc.ticket, tc.initials)
		}
	}
}

func TestTitleNeverStarvesAcrossTheSweep(t *testing.T) {
	for w := 40; w <= 200; w++ {
		for h := 10; h <= 60; h += 5 {
			l := computeLayout(w, h)
			fixed := 1 + 8 + 3 + 6 // rail + glyph gutter + tree + number
			if l.ShowTicket {
				fixed += 10
			}
			if l.InitialsAuthor {
				fixed += 4
			} else {
				fixed += 17
			}
			if l.ShowDiffstat {
				fixed += 12
			}
			fixed += 4 // age
			if l.ListWidth-2-fixed < 1 && l.ListWidth > fixed {
				t.Fatalf("w=%d h=%d: title starved (list=%d fixed=%d)", w, h, l.ListWidth, fixed)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestColumnLadder|TestTitleNeverStarves' -v`
Expected: FAIL to compile — `undefined: columnLadder`, `unknown field ShowDiffstat`.

- [ ] **Step 3: Add the fields and the ladder**

In `internal/ui/layout.go`, add to `Layout`:

```go
	// Column visibility, from columnLadder. The title and the glyph gutter are
	// absent here on purpose: they never shed.
	ShowDiffstat    bool
	CompactDiffstat bool // "±430" instead of "+412 -18"
	ShowTicket      bool
	InitialsAuthor  bool // 2-char initials instead of the full login
```

Add below `computeLayout`:

```go
// Column breakpoints, in LIST cells — not terminal cells. Once the preview pane
// drops, the list gets the whole terminal, so the lower steps fire less often
// than the numbers suggest.
const (
	ladderCompactDiff = 92 // below: diffstat collapses to ±N
	ladderInitials    = 80 // below: author collapses to initials
	ladderDropTicket  = 70 // below: ticket column goes
	ladderDropDiff    = 62 // below: diffstat goes
)

// columnLadder decides which optional columns survive at a given list width.
// Order is least-load-bearing first; the title and glyph gutter never shed.
func columnLadder(listCells int) Layout {
	l := Layout{ShowDiffstat: true, ShowTicket: true}
	if listCells < ladderCompactDiff {
		l.CompactDiffstat = true
	}
	if listCells < ladderInitials {
		l.InitialsAuthor = true
	}
	if listCells < ladderDropTicket {
		l.ShowTicket = false
	}
	if listCells < ladderDropDiff {
		l.ShowDiffstat = false
	}
	return l
}
```

In `computeLayout`, before each `return`, fold the ladder in. Add just above the `if !showSide` line:

```go
	cols := columnLadder(list)
	if !showSide {
		cols = columnLadder(w)
	}
```

and add to both `Layout{...}` literals:

```go
		ShowDiffstat: cols.ShowDiffstat, CompactDiffstat: cols.CompactDiffstat,
		ShowTicket: cols.ShowTicket, InitialsAuthor: cols.InitialsAuthor,
```

- [ ] **Step 4: Honour compact diffstat and initials**

In `internal/ui/section.go`, add to `RowOpts`:

```go
	CompactDiff bool // render ±N instead of +N -N
	Initials    bool // 2-char author initials instead of the login
```

Add the two renderers:

```go
// diffstatCompact is the narrow form: one signed magnitude instead of a pair.
func diffstatCompact(add, del int) string {
	return passStyle.Render("±" + abbrevCount(add+del))
}

// authorInitials is the narrow form of the author column. Two letters can
// collide (asaf-s-factify and assaflavi both give "AS"), which is why the full
// login is preferred wherever it fits — here the alternative is no author.
func authorInitials(login string) string {
	parts := strings.FieldsFunc(strings.TrimPrefix(login, "app/"), func(r rune) bool {
		return r == '-' || r == '/' || r == '_'
	})
	out := make([]rune, 0, 2)
	for _, p := range parts {
		if len(out) == 2 {
			break
		}
		out = append(out, []rune(strings.ToUpper(p))[0])
	}
	return string(out)
}
```

In `PRSection.RenderRow`, choose the forms:

```go
	diff := ""
	if o.DiffWidth > 0 {
		if o.CompactDiff {
			diff = diffstatCompact(p.Additions, p.Deletions)
		} else {
			diff = diffstat(p.Additions, p.Deletions)
		}
	}
	author := p.Author.Login
	if o.Initials {
		author = authorInitials(author)
	}
```

`diffstatWidth` must measure whichever form is in use — give it a `compact bool` parameter and select inside.

- [ ] **Step 5: Wire it through `renderList`**

```go
	diffW := 0
	if l.ShowDiffstat {
		diffW = diffstatWidth(m.section, l.CompactDiffstat)
	}
	tktW := 0
	if l.ShowTicket {
		tktW = ticketWidth(m.section)
	}
```

Add `compactDiff, initials bool` to `rowKey`, populate from `l`, and pass `CompactDiff: l.CompactDiffstat, Initials: l.InitialsAuthor` in `RowOpts`.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestColumnLadder|TestTitleNeverStarves' -v`
Expected: PASS

Run: `go test ./internal/ui/ -run TestLayoutSweep -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/layout.go internal/ui/layout_test.go internal/ui/section.go internal/ui/prlist.go
git commit -m "feat(ui): shed board columns as the list narrows

Diffstat compacts to ±N, then the author collapses to initials, then the
ticket goes, then the diffstat. Title and glyph gutter never shed.
Extends the mechanism computeLayout already uses for panes.

Refs #88"
```

---

### Task 9: Always-boxed filter bar

**Files:**
- Modify: `internal/ui/prlist.go` (`filterBar` ~line 2087)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `filterBar()` returning a 3-row bordered box in every state.

- [ ] **Step 1: Write the failing test**

```go
func TestFilterBarIsAlwaysThreeRows(t *testing.T) {
	m := newTestModelWideWithPR(t)
	for _, st := range []struct {
		name      string
		filtering bool
		query     string
	}{
		{"blurred empty", false, ""},
		{"blurred with query", false, "@asaf"},
		{"focused", true, ""},
		{"focused with query", true, "is:approved"},
	} {
		m.filtering = st.filtering
		m.filterInput.SetValue(st.query)
		if got := lipgloss.Height(m.filterBar()); got != 3 {
			t.Errorf("%s: filterBar height = %d, want 3", st.name, got)
		}
		if got := m.filterBarRows(); got != 3 {
			t.Errorf("%s: filterBarRows = %d, want 3", st.name, got)
		}
	}
}

func TestFilterBarShowsMatchCountWhenFiltered(t *testing.T) {
	m := newTestModelWithRows(t)
	m.filtering = true
	m.filterInput.SetValue("asaf")
	m.applyFilter()
	if !strings.Contains(stripANSIForTest(m.filterBar()), "→") {
		t.Error("filtered bar should show an n→m match count")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestFilterBar -v`
Expected: FAIL — height is 1 or 2 depending on state.

- [ ] **Step 3: Rewrite `filterBar`**

Replace the whole function in `internal/ui/prlist.go`:

```go
// filterBar renders the omni-filter as a bordered box. It is three rows in every
// state — the primary surface should not change height as it gains and loses
// focus, and filterBarRows measures off this render so contentHeight follows.
func (m Model) filterBar() string {
	inner := max(1, m.width-4) // border (2) + one cell of padding each side

	var body string
	switch {
	case m.filtering:
		body = m.filterInput.View()
	case m.filterInput.Value() != "":
		body = accentStyle.Render(truncate(m.filterInput.Value(), inner)) +
			dimStyle.Render("  esc clears")
	default:
		body = dimStyle.Render("filter — @user · is: · text")
	}
	body = accentStyle.Render(filterGlyph) + " " + body

	// The match count is the lowest-priority element and drops first.
	if total, shown := m.section.Len(), m.shownCount(); m.filterInput.Value() != "" {
		count := dimStyle.Render(fmt.Sprintf("%d→%d", total, shown))
		if pad := inner - lipgloss.Width(body) - lipgloss.Width(count); pad > 0 {
			body += strings.Repeat(" ", pad) + count
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.Rule)).
		Width(inner).
		Padding(0, 1).
		Render(truncate2(body, inner))
}
```

Add the glyph to `internal/ui/theme.go`:

```go
const filterGlyph = "⌕" // nerd: nf-oct-search
```

If a style-safe truncate for already-rendered strings does not exist, do not add `truncate2` — instead clamp `body` before styling and drop that call. `truncate` is documented as plain-text only and **will slice an escape sequence** if used on styled output.

- [ ] **Step 4: Add the match-count accessor**

If `shownCount` does not exist, add to `internal/ui/prlist.go`:

```go
// shownCount is the post-filter row count, for the filter bar's n→m readout.
func (m Model) shownCount() int { return m.section.Len() }
```

If `m.section.Len()` is already post-filter, capture the pre-filter total where `applyFilter` runs and store it on the model as `m.filterTotal int`, then use that as `total`.

- [ ] **Step 5: Delete the old dropdown spacer hack**

The removed code returned `bar + "\n"` to leave a clear row for the floating `@`-panel. The box now always occupies three rows, so confirm `omniDropdownY` still lands the panel below the box; if it assumed a 1-row bar, add 2 to its offset.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestFilterBar|TestOmni' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/prlist.go internal/ui/theme.go internal/ui/prlist_test.go
git commit -m "feat(ui): box the filter bar at a constant three rows

The primary surface shouldn't change height on focus. Adds a live n->m
match count inside the box.

Refs #88"
```

---

### Task 10: Preview identity block and section headers

**Files:**
- Modify: `internal/ui/preview.go` (`identityHeader` ~line 242, `sectionRule` ~line 278, `renderOverview` ~line 320)
- Modify: `internal/ui/theme.go` (section glyph constants)
- Test: `internal/ui/preview_test.go`

**Interfaces:**
- Consumes: `renderChips` (kept from Task 3 if it survived; if deleted, restore it here — the preview needs it).
- Produces: `func sectionHeader(glyph, label string, w int) string`, replacing `sectionRule`.

- [ ] **Step 1: Write the failing test**

```go
func TestIdentityHeaderCarriesBaseArrowAndLabels(t *testing.T) {
	pr := gh.PR{
		Number: 3087, Title: "fix(services): raise provenance contention",
		HeadRefName: "eng-7726-same-value", BaseRefName: "main",
		UpdatedAt: time.Now().Add(-2 * time.Hour),
		Labels:    []gh.Label{{Name: "complexity:7", Color: "fab387"}},
	}
	pr.Author.Login = "noamsto"

	got := stripANSIForTest(identityHeader(pr, 80))
	if !strings.Contains(got, "main") || !strings.Contains(got, "eng-7726-same-value") {
		t.Errorf("base and head must both appear:\n%s", got)
	}
	if !strings.Contains(got, "←") {
		t.Errorf("base <- head arrow missing:\n%s", got)
	}
	if !strings.Contains(got, "complexity:7") {
		t.Errorf("label chips must appear in the identity block:\n%s", got)
	}
}

func TestSectionHeaderHasNoRuleAndNoUppercasing(t *testing.T) {
	got := stripANSIForTest(sectionHeader(blockerGlyph, "blocker", 60))
	if strings.Contains(got, "─") {
		t.Errorf("section header must not draw a rule: %q", got)
	}
	if strings.Contains(got, "BLOCKER") {
		t.Errorf("section header must not uppercase: %q", got)
	}
	if !strings.Contains(got, "Blocker") {
		t.Errorf("section header should be Title Case: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestIdentityHeader|TestSectionHeader' -v`
Expected: FAIL to compile — `identityHeader` takes one argument, `sectionHeader` and `blockerGlyph` undefined.

- [ ] **Step 3: Add the section glyphs**

In `internal/ui/theme.go`:

```go
// Preview section glyphs. One soft accent carries all of them; the glyph is what
// distinguishes the sections, not a per-section hue.
const (
	blockerGlyph     = "⚠" // nerd: nf-oct-alert
	checksGlyph      = "✓" // nerd: nf-oct-check_circle
	reviewGlyph      = "◉" // nerd: nf-oct-code_review
	threadsGlyph     = "≡" // nerd: nf-oct-comment_discussion
	latestGlyph      = "◴" // nerd: nf-md-history
	descriptionGlyph = "▤" // nerd: nf-oct-book
	baseArrowGlyph   = "←" // base <- head
	headBranchGlyph  = "⎇" // nerd: nf-oct-git_branch
)
```

- [ ] **Step 4: Replace `sectionRule` with `sectionHeader`**

```go
// sectionHeader is a preview section divider: a glyph plus a Title Case name,
// underlined, in one accent. No rule and no uppercasing — the pane should paint
// its content, not its scaffolding.
func sectionHeader(glyph, label string, w int) string {
	name := strings.ToUpper(label[:1]) + label[1:]
	return sectionLabelStyle.Underline(true).Render(glyph + " " + name)
}
```

`w` is unused but kept so callers do not change shape; if the linter objects, name it `_ int`.

- [ ] **Step 5: Rewrite `identityHeader`**

```go
// identityHeader is the side card's top block: number + title, then author,
// base <- head and age, then the label chips. Labels live here rather than on
// the board row, and the base branch appears alongside the head so the merge
// target is visible without opening the Diff tab.
func identityHeader(pr gh.PR, w int) string {
	lines := []string{
		accentStyle.Render(fmt.Sprintf("#%d", pr.Number)) + " " + headerStyle.Render(pr.Title),
		authorStyle(pr.Author.Login).Render(pr.Author.Login) + "  " +
			dimStyle.Render(headBranchGlyph+" "+pr.BaseRefName+" "+baseArrowGlyph+" "+pr.HeadRefName) +
			dimStyle.Render("  "+ageString(pr.UpdatedAt)),
	}
	if chips := renderChips(pr.Labels, w); chips != "" {
		lines = append(lines, chips)
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 6: Update every call site**

In `previewPane`, change `identityHeader(ps.prAt(m.cursor))` to `identityHeader(ps.prAt(m.cursor), w-2)`.

In `renderOverview`, replace the `section` closure and its uses:

```go
	section := func(glyph, label, body string) string {
		return sectionHeader(glyph, label, w) + "\n" + indentLines(strings.TrimRight(body, "\n"), 2)
	}
```

and update each call: `section(descriptionGlyph, "description", body)`, `section(blockerGlyph, "blocker", card)`, `section(checksGlyph, "checks", ci)`, `section(reviewGlyph, "review", reviewLine(d))`, `section(threadsGlyph, label, body)`, `section(latestGlyph, "latest", ...)`.

Also update `issuePreviewPane` and any other `sectionRule` caller — `rg -n 'sectionRule' internal/` to find them all.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/ui/ -run 'TestIdentityHeader|TestSectionHeader|TestPreview' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/ui/preview.go internal/ui/theme.go internal/ui/preview_test.go
git commit -m "feat(ui): preview paints data, not scaffolding

Section headers become glyph + Title Case + underline in one accent, no
rule, no uppercasing. Identity block gains the label chips and shows
base <- head so the merge target is visible.

Refs #88"
```

---

### Task 11: `V` selects the cluster first

**Files:**
- Modify: `internal/ui/section.go` (add `unitLabel`)
- Modify: `internal/ui/prlist.go` (`groupRange` ~line 481, `advanceSelection` ~line 510)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: clustering from Task 7.
- Produces: `func (s *PRSection) unitLabel(i int) string` — the tightest selectable unit, currently `category + "\x00" + author`; #89 makes it return the stack. `func (m Model) unitRange() (lo, hi int)`.

- [ ] **Step 1: Write the failing test**

```go
func TestVSelectsClusterThenCategoryThenAllThenNone(t *testing.T) {
	m := newTestModelWithRows(t)
	ps := m.section.(*PRSection)
	// Park the cursor on a PR whose author has more than one row.
	m.cursor = 0
	author := ps.prAt(0).Author.Login
	inCluster := 0
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).Author.Login == author {
			inCluster++
		}
	}
	if inCluster < 2 {
		t.Skip("fixture has no multi-PR author")
	}

	m.advanceSelection()
	if got := m.sel.count(); got != inCluster {
		t.Fatalf("first V selected %d rows, want the %d-row cluster", got, inCluster)
	}

	lo, hi := m.groupRange()
	m.advanceSelection()
	if got := m.sel.count(); got != hi-lo+1 {
		t.Fatalf("second V selected %d rows, want the %d-row category", got, hi-lo+1)
	}

	m.advanceSelection()
	if got := m.sel.count(); got != ps.Len() {
		t.Fatalf("third V selected %d rows, want all %d", got, ps.Len())
	}

	m.advanceSelection()
	if got := m.sel.count(); got != 0 {
		t.Fatalf("fourth V left %d rows selected, want none", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestVSelectsCluster -v`
Expected: FAIL — the first `V` selects the whole category, so the count is the category size rather than the cluster size.

- [ ] **Step 3: Add the unit label**

In `internal/ui/section.go`:

```go
// unitLabel is the tightest selectable unit a row belongs to: its author cluster
// within its category. Distinct from groupLabel, which is the category (or the
// author, on an uncategorized board).
//
// The NUL separator keeps a category named like an author from colliding with
// one. #89 makes this return the stack for stacked rows.
func (s *PRSection) unitLabel(i int) string {
	p := s.prAt(i)
	return s.groupLabel(i) + "\x00" + p.Author.Login
}
```

- [ ] **Step 4: Add `unitRange` and a shared span walker**

In `internal/ui/prlist.go`, refactor `groupRange` so both levels share the walk:

```go
// spanOf returns the inclusive [lo, hi] shown-index span around cur for which
// label(i) is constant.
func spanOf(n, cur int, label func(int) string) (lo, hi int) {
	if n == 0 {
		return 0, -1
	}
	cur = min(max(cur, 0), n-1)
	want := label(cur)
	lo, hi = cur, cur
	for lo > 0 && label(lo-1) == want {
		lo--
	}
	for hi+1 < n && label(hi+1) == want {
		hi++
	}
	return lo, hi
}

// unitRange is the cursor's tightest selectable unit — its author cluster, or
// (once #89 lands) its stack. Falls back to the whole shown set off a PR board.
func (m Model) unitRange() (lo, hi int) {
	n := m.section.Len()
	ps, ok := m.section.(*PRSection)
	if !ok || !ps.grouped {
		return 0, n - 1
	}
	return spanOf(n, m.cursor, ps.unitLabel)
}
```

Rewrite `groupRange`'s body to use `spanOf(n, m.cursor, ps.groupLabel)`, keeping its existing early returns.

- [ ] **Step 5: Add the cluster step to the cycle**

Replace `advanceSelection`'s body:

```go
func (m *Model) advanceSelection() {
	n := m.section.Len()
	if n == 0 {
		return
	}
	full := func(lo, hi int) bool {
		for i := lo; i <= hi; i++ {
			if !m.sel.has(i) {
				return false
			}
		}
		return true
	}
	fill := func(lo, hi int) {
		for i := lo; i <= hi; i++ {
			if !m.sel.has(i) {
				m.sel.toggle(i)
			}
		}
	}
	// Tightest unit first, then its category, then everything, then nothing.
	for _, r := range [][2]int{{0, 0}, {1, 1}} {
		var lo, hi int
		if r[0] == 0 {
			lo, hi = m.unitRange()
		} else {
			lo, hi = m.groupRange()
		}
		if !full(lo, hi) {
			fill(lo, hi)
			return
		}
	}
	if m.sel.count() != n {
		fill(0, n-1)
		return
	}
	m.sel.clear()
}
```

If `m.sel` has no `clear()`, use whatever the existing final arm of `advanceSelection` used to deselect everything.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestVSelects|TestAdvanceSelection' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Update the docs**

In `README.md` and `KEYMAP.md`, change the `V` description from `select group → all → none` to `select cluster → category → all → none`.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/section.go internal/ui/prlist.go internal/ui/prlist_test.go README.md KEYMAP.md
git commit -m "feat(ui): V selects the author cluster before the category

Tightest unit the cursor occupies wins first. unitLabel is the seam #89
uses to return the stack instead.

Refs #88"
```

---

### Task 12: Branch in the narrow status bar

**Files:**
- Modify: `internal/ui/prlist.go` (`statusBar` ~line 2667)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `headBranchGlyph` (Task 10).
- Produces: `func truncateLeft(s string, w int) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestStatusBarShowsFocusedBranchWhenPreviewIsHidden(t *testing.T) {
	m := newTestModelWithRows(t)
	m.width, m.height = 80, 30 // below sideThreshold: no preview pane
	if computeLayout(m.width, m.height).ShowSide {
		t.Fatal("fixture width still shows the side pane")
	}
	v, ok := m.cursorVars()
	if !ok {
		t.Fatal("no cursor row")
	}
	if !strings.Contains(stripANSIForTest(m.statusBar()), v.HeadRefName) {
		t.Errorf("status bar should carry the focused branch %q", v.HeadRefName)
	}
}

func TestTruncateLeftKeepsTheTail(t *testing.T) {
	got := truncateLeft("eng-7726-same-value-different-evidence", 20)
	if lipgloss.Width(got) > 20 {
		t.Fatalf("truncateLeft over budget: %q is %d cells", got, lipgloss.Width(got))
	}
	if !strings.HasSuffix(got, "evidence") {
		t.Errorf("truncateLeft must keep the distinctive tail, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestStatusBarShowsFocusedBranch|TestTruncateLeft' -v`
Expected: FAIL to compile — `undefined: truncateLeft`.

- [ ] **Step 3: Implement `truncateLeft`**

In `internal/ui/section.go`, beside `truncate`:

```go
// truncateLeft shortens plain text to w cells by dropping the FRONT, prefixing
// an ellipsis. Branch names share long prefixes (eng-7726-…) and differ in the
// tail, so the tail is the part worth keeping.
func truncateLeft(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		if cand := "…" + string(r[i:]); lipgloss.Width(cand) <= w {
			return cand
		}
	}
	return "…"
}
```

- [ ] **Step 4: Add the segment**

At the end of `statusBar`, before it returns, append:

```go
	// Below the preview threshold the branch has nowhere else to live, and it is
	// what the copy and worktree actions operate on.
	if !computeLayout(m.width, m.height).ShowSide {
		if v, ok := m.cursorVars(); ok && v.HeadRefName != "" {
			bar := strings.Join(parts, statusBarSep)
			if room := m.width - lipgloss.Width(bar) - 4; room > 8 {
				parts = append(parts, dimStyle.Render(headBranchGlyph+" "+truncateLeft(v.HeadRefName, room)))
			}
		}
	}
```

Match the existing join separator — read the end of `statusBar` and reuse whatever it already uses instead of inventing `statusBarSep`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/ -run 'TestStatusBar|TestTruncateLeft' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/section.go internal/ui/prlist_test.go
git commit -m "feat(ui): carry the focused branch in the narrow status bar

Below the preview threshold labels and branch are otherwise only
reachable via the expanded view. Truncates from the left so the
distinctive tail survives.

Refs #88"
```

---

### Task 13: Verify the whole board by hand

**Files:** none — this is a verification gate.

- [ ] **Step 1: Build and run in a wide terminal**

```bash
go build -o /tmp/prdash-88 . && cd ~/Data/git/factify-inc/mono && /tmp/prdash-88
```

Confirm: rows are one line; the boxed filter bar is three rows; `#numbers` sit hard against the gutter; the diffstat and ticket columns align; blank tickets leave no visible hole; drafts show the gutter glyph with no `[draft]` tag; author clusters are contiguous.

- [ ] **Step 2: Confirm the #62 fix**

Leave it open while a PR's checks finish (or press `r` on one). Confirm no row changes position and the cursor stays on the same PR.

- [ ] **Step 3: Walk the responsive ladder**

Resize the terminal down through roughly 120 → 110 → 95 → 85 → 75 → 65 columns. Confirm: no row ever wraps or overflows the pane border, the title never disappears, and the columns drop in the documented order.

- [ ] **Step 4: Check the preview**

Confirm section headers are glyph + Title Case + underline with no rules, labels appear as chips under the identity line, and the branch reads `main ← head`.

- [ ] **Step 5: Exercise `V`**

On a PR whose author has several rows, press `V` four times and confirm cluster → category → all → none.

- [ ] **Step 6: Set the real glyphs**

The constants added in Tasks 4, 9 and 10 carry placeholder characters with `// nerd:` comments. Replace them with the intended Nerd Font glyphs, rebuild, and confirm each renders as one cell — a two-cell glyph in a one-cell slot shifts the whole gutter.

- [ ] **Step 7: Commit any glyph corrections**

```bash
git add internal/ui/theme.go
git commit -m "chore(ui): set the real nerd glyphs for draft, filter and preview sections

Refs #88"
```

---

## Self-Review

**Spec coverage.** Every spec section maps to a task: what moves (3-6), row layout (3-6), row separation (Task 3 removes the two-line path and adds nothing in its place; the "N rows renders N lines" assertion lives in Task 3's Step 1), ordering (7), ticket id (2, 6), filter bar (9), preview (10), responsive ladder (8), `V` (11), narrow terminals (12), data (1).

**One gap found and closed:** the spec's testing section asks for a guard that a board of N rows renders N lines. That is Task 3's `TestRowIsAlwaysOneLine`, which asserts no newline in a rendered row — the per-row form of the same property.

**Type consistency.** `RowOpts` accumulates `DiffWidth`, `TicketWidth`, `CompactDiff`, `Initials` across Tasks 5, 6 and 8 and is used with those exact names in each. `diffstatWidth` gains a second parameter in Task 8; Task 5's single-argument call site is updated there, in Step 5. `identityHeader` gains a width parameter in Task 10 and its only caller is updated in the same task. `renderItemRow` grows `diff` (Task 5) and `ticket` (Task 6) parameters; each task updates both `RenderRow` implementations.

**Known ordering hazard:** Tasks 5, 6 and 8 each modify `renderItemRow`'s signature and `rowKey`'s fields. They must be done in numeric order, or the later diffs will not apply.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-04-board-preview-cleanup.md`. Two execution options:

**1. Subagent-Driven (recommended)** — a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using executing-plans, batch execution with checkpoints.
