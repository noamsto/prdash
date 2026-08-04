# Stacked PRs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show GitHub stacked PRs as a chain hoisted to its base PR's category, with a merge-order blocker rule.

**Architecture:** The data arrives free with #88's query change. Chains are reconstructed locally by grouping on `stack.Number` and ordering by `StackPosition`, then hoisted so the whole chain renders in the category of its lowest visible member. Rendering fills the tree column #88 reserved. `unitLabel` — the seam #88 left — returns the stack for stacked rows, so `V` selects the chain.

**Tech Stack:** Go 1.24, charmbracelet/bubbletea v2, lipgloss v2, stdlib `testing`.

Spec: `docs/superpowers/specs/2026-08-04-stacked-prs-design.md`
Issue: #89 · **Depends on #88** (query fields, `RowOpts.Tree`, `unitLabel`)

## Global Constraints

- **Every stack fixture MUST have three or more links.** At `size: 2`, correct chain notation and incorrect sibling notation render identically, so a two-link fixture cannot fail. This is not a style preference — it is the difference between a test that works and one that does not.
- **Never render `stack.Number` as `#N`.** It is drawn from the repo's shared issue/PR sequence but resolves to neither a PR nor an issue, and `PullRequestStack` has no `url`. Printing `#3074` promises a page that 404s.
- **Never query `stack.entries`.** It is a connection and would add real GraphQL cost for members already present in the search result.
- Nerd glyphs are declared as constants with a `// nerd:` comment naming the intended icon; do not invent codepoints.
- Run `go test ./...` from the repo root before every commit.
- Conventional Commits with a scope, e.g. `feat(ui):`.

---

### Task 1: Hoist chains into one category

**Files:**
- Modify: `internal/ui/section.go` (`groupByCategory`, `setShownOrdered`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `gh.PR.Stack`, `gh.PR.StackPosition` (#88 Task 1); `groupByCategory(prs, idx, cats, order, state)` (#88 Task 7).
- Produces: `func stackChains(prs []gh.PR, idx []int) map[int][]int` — stack number to shown-indices in position order; `func hoistStacks(prs []gh.PR, idx []int, cats map[int]string, order []string) (map[int]string, map[int][]int)`.

- [ ] **Step 1: Write the failing test**

```go
// Three links, spanning three categories. Two links would not distinguish a
// correct implementation from a broken one.
func stackFixture() ([]gh.PR, map[int]string, []string) {
	prs := []gh.PR{
		{Number: 3065, State: "OPEN", Title: "base", HeadRefName: "agents/spicedb-rel-migrate-88ee",
			Stack: &gh.PRStack{Number: 3074, Size: 3, BaseRefName: "main"}, StackPosition: 1},
		{Number: 3073, State: "OPEN", Title: "middle", HeadRefName: "eng-7452-share-one", IsDraft: true,
			Stack: &gh.PRStack{Number: 3074, Size: 3, BaseRefName: "main"}, StackPosition: 2},
		{Number: 3099, State: "OPEN", Title: "tip", HeadRefName: "eng-7452-archer-id",
			Stack: &gh.PRStack{Number: 3074, Size: 3, BaseRefName: "main"}, StackPosition: 3},
		{Number: 3086, State: "OPEN", Title: "unstacked"},
	}
	for i := range prs {
		prs[i].Author.Login = "asaf-s-factify"
	}
	cats := map[int]string{
		3065: "Review requested",
		3073: "Others",
		3099: "Mine",
		3086: "Review requested",
	}
	return prs, cats, []string{"Review requested", "Mine", "Others"}
}

func TestChainRendersOnceUnderTheBasePRsCategory(t *testing.T) {
	prs, cats, order := stackFixture()
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)

	seen := map[int]int{}
	labels := map[int]string{}
	for i := 0; i < s.Len(); i++ {
		n := s.prAt(i).Number
		seen[n]++
		labels[n] = s.groupLabel(i)
	}
	for _, n := range []int{3065, 3073, 3099, 3086} {
		if seen[n] != 1 {
			t.Errorf("#%d appears %d times, want exactly 1", n, seen[n])
		}
	}
	// The base PR is in Review requested, so the whole chain goes there.
	for _, n := range []int{3065, 3073, 3099} {
		if labels[n] != "Review requested" {
			t.Errorf("#%d is under %q, want Review requested", n, labels[n])
		}
	}
}

func TestChainStaysContiguousInPositionOrder(t *testing.T) {
	prs, cats, order := stackFixture()
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)

	got := []int{}
	for i := 0; i < s.Len(); i++ {
		if p := s.prAt(i); p.Stack != nil {
			got = append(got, p.Number)
		}
	}
	want := []int{3065, 3073, 3099} // position ascending; the draft does NOT sink
	if !slices.Equal(got, want) {
		t.Fatalf("chain order = %v, want %v", got, want)
	}
}

func TestBaseIsLowestVISIBLEPosition(t *testing.T) {
	prs, cats, order := stackFixture()
	prs = prs[1:] // position 1 already merged, so it is absent from an is:open board
	delete(cats, 3065)
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)

	first := -1
	for i := 0; i < s.Len(); i++ {
		if s.prAt(i).Stack != nil {
			first = s.prAt(i).Number
			break
		}
	}
	if first != 3073 {
		t.Fatalf("chain root is #%d, want #3073 (lowest visible position)", first)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestChain|TestBaseIsLowest' -v`
Expected: FAIL — `#3073` and `#3099` sit under Others and Mine, because `groupByCategory` places each PR by its own category.

- [ ] **Step 3: Build the chain index**

Add to `internal/ui/section.go`:

```go
// stackChains groups shown indices by stack number, each in ascending position
// order. Reconstructed locally rather than from stack.entries: every open member
// is already in this result, and entries is a connection with real cost.
func stackChains(prs []gh.PR, idx []int) map[int][]int {
	chains := map[int][]int{}
	for _, i := range idx {
		if p := prs[i]; p.Stack != nil {
			chains[p.Stack.Number] = append(chains[p.Stack.Number], i)
		}
	}
	for sn := range chains {
		slices.SortStableFunc(chains[sn], func(a, b int) int {
			return prs[a].StackPosition - prs[b].StackPosition
		})
	}
	return chains
}

// chainHome maps each stack number to the category its chain renders in: the
// category of its lowest VISIBLE member. Once position 1 merges it leaves an
// is:open board, so "lowest visible" is not always position 1.
func chainHome(prs []gh.PR, chains map[int][]int, cats map[int]string) map[int]string {
	home := map[int]string{}
	for sn, members := range chains {
		home[sn] = cats[prs[members[0]].Number] // members are position-sorted
	}
	return home
}
```

- [ ] **Step 4: Hoist inside `groupByCategory`**

Replace the body of `groupByCategory`:

```go
func groupByCategory(prs []gh.PR, idx []int, cats map[int]string, order []string, state string) []int {
	chains := stackChains(prs, idx)
	home := chainHome(prs, chains, cats)

	// A chain is one unit placed by its root, so a member whose own category
	// differs is skipped where it would otherwise have landed.
	catOf := func(i int) string {
		if p := prs[i]; p.Stack != nil {
			return home[p.Stack.Number]
		}
		return cats[prs[i].Number]
	}

	out := make([]int, 0, len(idx))
	for _, cat := range order {
		members := make([]int, 0, len(idx))
		for _, i := range idx {
			// Only the root represents its chain during clustering; the rest are
			// spliced in after, so the chain cannot be split by cluster ordering.
			if p := prs[i]; p.Stack != nil && i != chains[p.Stack.Number][0] {
				continue
			}
			if catOf(i) == cat {
				members = append(members, i)
			}
		}
		for _, i := range groupByAuthor(prs, members, state) {
			out = append(out, i)
			if p := prs[i]; p.Stack != nil {
				out = append(out, chains[p.Stack.Number][1:]...) // chain follows its root
			}
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/ -run 'TestChain|TestBaseIsLowest' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): hoist stacked PRs into their base PR's category

Chains span categories, which no sort rule can fix. The whole chain
renders where its lowest visible member sits, in position order, and a
draft mid-chain keeps its place rather than sinking.

Refs #89"
```

---

### Task 2: Render the chain

**Files:**
- Modify: `internal/ui/theme.go`
- Modify: `internal/ui/section.go` (`PRSection.RenderRow`, add `treeGlyphFor`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `RowOpts.Tree` (#88 Task 3 Step 5); `stackChains` (Task 1).
- Produces: `func (s *PRSection) treeAt(i int) (glyph string, hiddenLinks int)`.

- [ ] **Step 1: Write the failing test**

```go
func TestChainGlyphsMarkRootMiddleAndTip(t *testing.T) {
	prs, cats, order := stackFixture()
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)

	glyphs := map[int]string{}
	for i := 0; i < s.Len(); i++ {
		if s.prAt(i).Stack == nil {
			continue
		}
		g, _ := s.treeAt(i)
		glyphs[s.prAt(i).Number] = g
	}
	if glyphs[3065] != stackRootGlyph {
		t.Errorf("root glyph = %q, want %q", glyphs[3065], stackRootGlyph)
	}
	if glyphs[3073] != chainTeeGlyph {
		t.Errorf("middle glyph = %q, want %q — a middle link must not be the tip", glyphs[3073], chainTeeGlyph)
	}
	if glyphs[3099] != chainEndGlyph {
		t.Errorf("tip glyph = %q, want %q", glyphs[3099], chainEndGlyph)
	}
}

func TestUnstackedRowsGetNoTreeGlyph(t *testing.T) {
	prs, cats, order := stackFixture()
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)
	for i := 0; i < s.Len(); i++ {
		if s.prAt(i).Number != 3086 {
			continue
		}
		if g, _ := s.treeAt(i); g != "" {
			t.Errorf("unstacked row got tree glyph %q", g)
		}
	}
}

func TestRootReportsLinksMissingFromTheBoard(t *testing.T) {
	prs, cats, order := stackFixture()
	prs = prs[:2] // size says 3, only 2 are present
	delete(cats, 3099)
	s := NewPRSection("is:open")
	s.SetCategorized(prs, cats, order)

	for i := 0; i < s.Len(); i++ {
		if s.prAt(i).Number != 3065 {
			continue
		}
		if _, hidden := s.treeAt(i); hidden != 1 {
			t.Fatalf("root reports %d hidden links, want 1 — a silently short chain is a lie", hidden)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestChainGlyphs|TestUnstacked|TestRootReports' -v`
Expected: FAIL to compile — `undefined: stackRootGlyph`, `s.treeAt`.

- [ ] **Step 3: Add the glyphs**

In `internal/ui/theme.go`:

```go
// Stack chain glyphs. The root takes a stack mark rather than a line connector:
// a ┌─ would have to align vertically with the ├─ below it, and a glyph makes no
// alignment promise it cannot keep.
const (
	stackRootGlyph = "⧉"  // nerd: nf-oct-stack
	chainTeeGlyph  = "├─" // a link with another below it
	chainEndGlyph  = "╰─" // the tip
)

// stackMark colours the chain in the header hue — it is structure, not state, so
// it must not compete with the ci/review columns beside it.
func stackMark(s string) string { return mergedStyle.Render(s) }
```

- [ ] **Step 4: Implement `treeAt`**

Add to `internal/ui/section.go`:

```go
// treeAt returns the tree-column glyph for a shown row and, for a chain root,
// how many of its links are absent from the current board. A merged link leaves
// an is:open result, so the visible chain can be shorter than Stack.Size.
func (s *PRSection) treeAt(i int) (string, int) {
	p := s.prAt(i)
	if p.Stack == nil {
		return "", 0
	}
	members := stackChains(s.prs, s.shown)[p.Stack.Number]
	pos := slices.Index(members, s.shown[i])
	switch {
	case pos == 0:
		return stackRootGlyph, p.Stack.Size - len(members)
	case pos == len(members)-1:
		return chainEndGlyph, 0
	default:
		return chainTeeGlyph, 0
	}
}
```

- [ ] **Step 5: Feed it into the row**

In `PRSection.RenderRow`, before the `return`:

```go
	tree, hidden := s.treeAt(i)
	o.Tree = stackMark(tree)
	title := p.Title
	if hidden > 0 {
		// Without this the chain looks complete when it isn't.
		title += stackMark(fmt.Sprintf(" %s+%d", stackRootGlyph, hidden))
	}
```

and pass `title` instead of `p.Title`.

Because `title` is now pre-styled in the hidden-links case, confirm `renderItemRow` does not run `truncate` over it — `truncate` is plain-text only and will slice an escape sequence. If it does, append the marker after truncation instead: pass `hidden` through `RowOpts` as `HiddenLinks int` and add the marker inside `renderItemRow` alongside the existing `landed` tag, which already solves exactly this.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/ui/ -run 'TestChainGlyphs|TestUnstacked|TestRootReports' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/ui/theme.go internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): draw stacked PRs as a chain in the tree column

Root takes the stack mark, links take tee/end connectors, and the root
reports links missing from the board so a shortened chain can't read as
a complete one.

Refs #89"
```

---

### Task 3: `D` cannot hide a stacked draft

**Files:**
- Modify: `internal/ui/section.go` (`setShownOrdered`)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: unchanged signatures.

- [ ] **Step 1: Write the failing test**

```go
func TestHideDraftsKeepsStackedDrafts(t *testing.T) {
	prs, cats, order := stackFixture() // #3073 is a draft, mid-chain
	prs = append(prs, gh.PR{Number: 2984, State: "OPEN", Title: "loose draft", IsDraft: true})
	prs[len(prs)-1].Author.Login = "shay-factify"
	cats[2984] = "Others"

	s := NewPRSection("is:open")
	s.SetHideDrafts(true)
	s.SetCategorized(prs, cats, order)

	got := map[int]bool{}
	for i := 0; i < s.Len(); i++ {
		got[s.prAt(i).Number] = true
	}
	if !got[3073] {
		t.Error("#3073 was hidden: removing a link makes the remaining chain a lie")
	}
	if got[2984] {
		t.Error("#2984 is an unstacked draft and should still be hidden")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestHideDraftsKeepsStacked -v`
Expected: FAIL — `#3073` is missing.

- [ ] **Step 3: Exempt stacked drafts**

In `setShownOrdered`, replace the `hideDrafts` filter:

```go
	if s.hideDrafts {
		idx = slices.DeleteFunc(slices.Clone(idx), func(i int) bool {
			// A stacked draft stays: dropping a link would leave the tip
			// connector claiming to be the tip when it is not.
			return s.prs[i].IsDraft && s.prs[i].Stack == nil
		})
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run TestHideDrafts -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "fix(ui): D must not hide a draft that is part of a stack

Removing a link would leave the tip connector claiming to be the tip.

Refs #89"
```

---

### Task 4: `V` selects the chain

**Files:**
- Modify: `internal/ui/section.go` (`unitLabel`)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `unitLabel` (#88 Task 11).
- Produces: `unitLabel` returning a stack-scoped key for stacked rows.

- [ ] **Step 1: Write the failing test**

```go
func TestVOnAStackedRowSelectsTheChain(t *testing.T) {
	prs, cats, order := stackFixture()
	m := newTestModelWideWithPR(t)
	ps := NewPRSection("is:open")
	ps.SetCategorized(prs, cats, order)
	m.section = ps
	m.rowGen++

	// Park the cursor on the middle link.
	for i := 0; i < ps.Len(); i++ {
		if ps.prAt(i).Number == 3073 {
			m.cursor = i
		}
	}
	m.advanceSelection()

	got := map[int]bool{}
	for i := 0; i < ps.Len(); i++ {
		if m.sel.has(i) {
			got[ps.prAt(i).Number] = true
		}
	}
	for _, n := range []int{3065, 3073, 3099} {
		if !got[n] {
			t.Errorf("#%d not selected — V inside a stack should select the chain", n)
		}
	}
	if got[3086] {
		t.Error("#3086 is unstacked and should not be selected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestVOnAStackedRow -v`
Expected: FAIL — the author cluster is selected, which here also pulls in `#3086`.

- [ ] **Step 3: Make `unitLabel` stack-aware**

Replace `unitLabel` in `internal/ui/section.go`:

```go
// unitLabel is the tightest selectable unit a row belongs to. A stack is a real
// dependency, so it wins over the author cluster, which is only an ordering.
//
// The NUL separator keeps a category named like an author from colliding.
func (s *PRSection) unitLabel(i int) string {
	p := s.prAt(i)
	if p.Stack != nil {
		return "stack\x00" + strconv.Itoa(p.Stack.Number)
	}
	return s.groupLabel(i) + "\x00" + p.Author.Login
}
```

Add `"strconv"` to the imports if absent.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestVOnAStackedRow|TestVSelectsCluster' -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/prlist_test.go
git commit -m "feat(ui): V selects the stack when the cursor is inside one

A stack is a dependency; an author cluster is only an ordering, so the
stack is the tighter unit.

Refs #89"
```

---

### Task 5: Blocked-on triage rule

**Files:**
- Modify: `internal/triage/triage.go`
- Test: `internal/triage/triage_test.go`

**Interfaces:**
- Consumes: `gh.PR.Stack`, `gh.PR.StackPosition`.
- Produces: `triage.KindStackBlocked` and a `Card` naming the blocking PR.

- [ ] **Step 1: Write the failing test**

```go
func TestStackBlockedOutranksFailingChecks(t *testing.T) {
	pr := gh.PR{
		Number: 3073, State: "OPEN",
		Stack: &gh.PRStack{Number: 3074, Size: 3, BaseRefName: "main"}, StackPosition: 2,
		StatusCheckRollup: []gh.Check{{Name: "build", State: "FAILURE", Conclusion: "FAILURE"}},
	}
	c := Preliminary(pr, "noamsto")
	if c.Kind != KindStackBlocked {
		t.Fatalf("Kind = %v, want KindStackBlocked — no amount of green makes a blocked PR mergeable", c.Kind)
	}
	if !strings.Contains(c.Detail, "stack") {
		t.Errorf("Detail should mention the stack, got %q", c.Detail)
	}
}

func TestStackRootIsNotBlocked(t *testing.T) {
	pr := gh.PR{
		Number: 3065, State: "OPEN",
		Stack: &gh.PRStack{Number: 3074, Size: 3, BaseRefName: "main"}, StackPosition: 1,
	}
	if c := Preliminary(pr, "noamsto"); c.Kind == KindStackBlocked {
		t.Error("position 1 has no parent and must not be blocked")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/triage/ -run TestStack -v`
Expected: FAIL to compile — `undefined: KindStackBlocked`.

- [ ] **Step 3: Add the kind**

In `internal/triage/triage.go`, add `KindStackBlocked` to the `Kind` constants, placed so it sorts **above** the checks and review kinds in whatever precedence mechanism the file uses.

- [ ] **Step 4: Add the rule**

At the **top** of the branch chain in both `Preliminary` and `Compute` — before the checks and review branches:

```go
	// A stacked PR below the root cannot merge until its parent lands, whatever
	// its own checks and reviews say. Position alone proves it; no extra query.
	if pr.Stack != nil && pr.StackPosition > 1 {
		c.Kind = KindStackBlocked
		c.Detail = fmt.Sprintf("blocked by the PR below it in stack (position %d of %d)",
			pr.StackPosition, pr.Stack.Size)
		return c
	}
```

Match the surrounding code's field names for `Detail`, `ActionKey` and `ActionLabel`; read the neighbouring branches and mirror them. Leave `ActionKey` empty — there is no action that unblocks it.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/triage/ -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/triage/ internal/ui/
git commit -m "feat(triage): name the blocking PR for a stacked PR

StackPosition > 1 means the parent must land first, regardless of this
PR's own checks and reviews, so it outranks a failing-CI blocker.

Refs #89"
```

---

### Task 6: Verify against the live stack

**Files:** none — verification gate.

- [ ] **Step 1: Build and run against a repo with a real stack**

```bash
go build -o /tmp/prdash-89 . && cd ~/Data/git/factify-inc/mono && /tmp/prdash-89
```

- [ ] **Step 2: Confirm hoisting**

The live stack is `3074`, with `#3065` at position 1 (Review requested) and `#3073` at position 2 (Others, draft). Confirm both render together under **Review requested**, and `#3073` does **not** also appear under Others.

- [ ] **Step 3: Confirm glyph alignment**

Confirm the CI and review glyphs on the stacked rows sit on the same columns as every unstacked row's. If they shift, the tree column has been placed before the gutter rather than after it.

- [ ] **Step 4: Confirm the blocker card**

Move the cursor to `#3073` and confirm the preview's Blocker section names the stack rather than reporting its checks.

- [ ] **Step 5: Confirm `D` and `V`**

Press `D`: `#3073` must remain. Press `V` on either link: both must select, and no unstacked PR may.

- [ ] **Step 6: Set the real glyphs**

Replace the placeholders in `stackRootGlyph`, `chainTeeGlyph`, `chainEndGlyph` with the intended Nerd Font glyphs and confirm each measures the expected cell width — `⧉` must be one cell and the connectors two, or the tree column's 3-cell budget overflows into the number.

- [ ] **Step 7: Commit any glyph corrections**

```bash
git add internal/ui/theme.go
git commit -m "chore(ui): set the real nerd glyphs for the stack chain

Refs #89"
```

---

## Self-Review

**Spec coverage.** Hoisting (Task 1), base-is-lowest-visible (Task 1), chain rendering and root mark (Task 2), missing-links marker (Task 2), stack-orders-as-one-unit including the no-sink rule for mid-chain drafts (Task 1's `TestChainStaysContiguousInPositionOrder`), `D` exemption (Task 3), `V` (Task 4), blocker rule and its precedence (Task 5), never-render-the-number (Global Constraints, and no task prints it), never-query-entries (Global Constraints; Task 1 reconstructs locally).

**Type consistency.** `stackChains` is defined in Task 1 and reused in Task 2's `treeAt`. `RowOpts.Tree` comes from #88 Task 3 Step 5 and is the only field Task 2 sets. `unitLabel` is defined in #88 Task 11 and replaced wholesale in Task 4, keeping its signature. `gh.PRStack` field names (`Number`, `Size`, `BaseRefName`) match #88 Task 1 exactly.

**One hazard flagged inline:** Task 2 Step 5 appends a pre-styled marker to the title, which `truncate` would corrupt. The step names the safe alternative (`RowOpts.HiddenLinks`, mirroring the existing `landed` tag) rather than leaving it to be discovered.

**Dependency:** every task needs #88 merged first. Task 2 in particular cannot work without `RowOpts.Tree`.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-04-stacked-prs.md`. Blocked on #88.
