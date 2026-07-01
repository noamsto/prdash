# prdash visual polish v2 — Phase B (author-cardinality grouping) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group the PR board by author when the shown set spans ≥2 authors (dim author-rule headers, handle hoisted out of the row); show a flat, handle-free list when every shown PR shares one author.

**Architecture:** Grouping is decided on the *shown* set (so it tracks filtering), and the `shown` index order is made group-contiguous when grouping is active — the cursor keeps traversing rows in display order. `renderList` interleaves header lines and captures the cursor's *line* offset (headers shift it); `scrollToCursor` scrolls to that line instead of `cursor × rowLines`. Per-row author is dropped for PR rows in both modes (it's redundant when flat, and lives in the header when grouped); issue rows are unchanged.

**Tech Stack:** Go, `charm.land/lipgloss/v2` v2.0.4, table-driven `testing`.

## Global Constraints

- Grouping is driven by **distinct author count over the shown set**: 1 → flat list, no per-row author; ≥2 → grouped under dim author-rule headers with the handle in the header.
- Within a group, rows keep the Phase-A actionability order. Groups are ordered by their **most-actionable (lowest-rank) member**, ties broken by author login.
- The cursor indexes the shown set in **display order**; header lines are visual-only and never selectable.
- PR rows never render the author inline (flat hides it; grouped puts it in the header). **Issue rows are unchanged** — they keep their inline author.
- Match existing test style: table-driven, `strings.Contains`. Commit with `PRE_COMMIT_ALLOW_NO_CONFIG=1 git commit …` (pre-commit hook aborts otherwise). IDE gopls `undefined: <symbol>` / `lipgloss.Color is not a type` / `use of internal package` warnings are workspace artifacts — trust `go build`/`go test`.
- Run `go test ./...`, `go vet ./...`, `nix build` from the worktree; commit after each task.

---

### Task 1: Grouping decision + group-contiguous ordering

**Files:**
- Modify: `internal/ui/section.go` (add `grouped bool` to `PRSection`; add `distinctAuthors`, `groupByAuthor`, `(*PRSection).setShownOrdered`; route `SetPRs` and `SetShown` through it)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `prRank(gh.PR) int` (Phase A), `gh.PR.Author.Login`, `allIdx(int) []int`, `slices` (already imported).
- Produces: `PRSection.grouped bool`; `func distinctAuthors(prs []gh.PR, idx []int) int`; `func groupByAuthor(prs []gh.PR, idx []int) []int`; `func (s *PRSection) setShownOrdered(idx []int)`. After `SetPRs`/`SetShown`, `s.shown` is in display order and `s.grouped` reflects the shown set.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestSetShownOrderedGroupsByAuthorWhenMultiple(t *testing.T) {
	a := gh.PR{Number: 1, ReviewDecision: "REVIEW_REQUIRED"} // alice, rank waiting
	a.Author.Login = "alice"
	b := gh.PR{Number: 2, ReviewDecision: "APPROVED",          // bob, rank ready
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	b.Author.Login = "bob"
	a2 := gh.PR{Number: 3, ReviewDecision: "CHANGES_REQUESTED"} // alice, rank changes
	a2.Author.Login = "alice"

	s := NewPRSection("")
	s.SetPRs([]gh.PR{a, b, a2})

	if !s.grouped {
		t.Fatal("two distinct authors should switch the section to grouped mode")
	}
	// bob's group leads (its best rank, ready=0, beats alice's best, changes=1).
	// within alice's group, changes(#3) precedes waiting(#1).
	var got []int
	for i := 0; i < s.Len(); i++ {
		got = append(got, s.prAt(i).Number)
	}
	want := []int{2, 3, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped display order = %v, want %v", got, want)
	}
}

func TestSetShownOrderedFlatWhenSingleAuthor(t *testing.T) {
	p1 := gh.PR{Number: 1, ReviewDecision: "APPROVED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	p1.Author.Login = "alice"
	p2 := gh.PR{Number: 2, ReviewDecision: "REVIEW_REQUIRED"}
	p2.Author.Login = "alice"

	s := NewPRSection("")
	s.SetPRs([]gh.PR{p2, p1}) // unsorted input

	if s.grouped {
		t.Fatal("a single distinct author must stay flat (not grouped)")
	}
	// flat actionability order: ready(#1) before waiting(#2)
	if s.prAt(0).Number != 1 || s.prAt(1).Number != 2 {
		t.Fatalf("flat order = [%d %d], want [1 2]", s.prAt(0).Number, s.prAt(1).Number)
	}
}
```

(Ensure the test file imports `"slices"` — Phase A's sort test already added it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestSetShownOrdered' -v`
Expected: FAIL — `s.grouped` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/section.go`, add `grouped` to the struct:

```go
type PRSection struct {
	filter  string
	prs     []gh.PR
	shown   []int
	grouped bool // true when the shown set spans ≥2 authors → render author headers
}
```

Route `SetPRs` and `SetShown` through the ordering helper:

```go
func (s *PRSection) SetPRs(p []gh.PR) { sortPRs(p); s.prs = p; s.setShownOrdered(allIdx(len(p))) }
func (s *PRSection) SetShown(idx []int) { s.setShownOrdered(idx) }
```

Add the helpers (near `sortPRs`):

```go
// setShownOrdered records the shown subset in display order and decides grouping.
// idx arrives in actionability order (prs is rank-sorted; idx preserves it). With
// ≥2 distinct authors the rows are regrouped contiguously by author so the cursor
// still walks them top-to-bottom; with one author the flat rank order stands.
func (s *PRSection) setShownOrdered(idx []int) {
	if distinctAuthors(s.prs, idx) >= 2 {
		s.grouped = true
		s.shown = groupByAuthor(s.prs, idx)
		return
	}
	s.grouped = false
	s.shown = idx
}

func distinctAuthors(prs []gh.PR, idx []int) int {
	seen := map[string]struct{}{}
	for _, i := range idx {
		seen[prs[i].Author.Login] = struct{}{}
	}
	return len(seen)
}

// groupByAuthor reorders idx so each author's rows are contiguous. Groups are
// ordered by their best (lowest) member rank, ties by login; within a group the
// incoming (rank) order is preserved.
func groupByAuthor(prs []gh.PR, idx []int) []int {
	groups := map[string][]int{}
	best := map[string]int{}
	for _, i := range idx {
		a := prs[i].Author.Login
		r := prRank(prs[i])
		if _, ok := groups[a]; !ok || r < best[a] {
			best[a] = r
		}
		groups[a] = append(groups[a], i)
	}
	authors := make([]string, 0, len(groups))
	for a := range groups {
		authors = append(authors, a)
	}
	slices.SortStableFunc(authors, func(x, y string) int {
		if best[x] != best[y] {
			return best[x] - best[y]
		}
		return strings.Compare(x, y)
	})
	out := make([]int, 0, len(idx))
	for _, a := range authors {
		out = append(out, groups[a]...)
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestSetShownOrdered|TestSetPRs|TestHydrate' -v`
Expected: PASS. (`distinctAuthors` over a single-author or empty set returns ≤1, so existing single-author fixtures stay flat and their order is unchanged.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): group PR board by author when the set spans multiple"
```

---

### Task 2: Drop the inline author on PR rows

**Files:**
- Modify: `internal/ui/section.go` (`PRSection.RenderRow` passes `""` for author)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `renderItemRow` (an empty author string renders nothing on the right, leaving just the age).
- Produces: PR rows never contain the author login; issue rows unchanged.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestPRRowOmitsInlineAuthor(t *testing.T) {
	p := gh.PR{Number: 1, Title: "do the thing"}
	p.Author.Login = "alice"
	s := NewPRSection("")
	s.SetPRs([]gh.PR{p})
	if row := s.RenderRow(0, RowOpts{Width: 80}); strings.Contains(row, "alice") {
		t.Fatalf("PR row must not render the author inline (it lives in the header): %q", row)
	}
}

func TestIssueRowKeepsInlineAuthor(t *testing.T) {
	is := gh.Issue{Number: 1, Title: "bug"}
	is.Author.Login = "carol"
	s := NewIssueSection("")
	s.SetIssues([]gh.Issue{is})
	if row := s.RenderRow(0, RowOpts{Width: 80}); !strings.Contains(row, "carol") {
		t.Fatalf("issue row should still show its author: %q", row)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestPRRowOmitsInlineAuthor|TestIssueRowKeepsInlineAuthor' -v`
Expected: `TestPRRowOmitsInlineAuthor` FAILs (row still contains "alice"); the issue test passes.

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/section.go`, change `PRSection.RenderRow` to pass an empty author:

```go
func (s *PRSection) RenderRow(i int, o RowOpts) string {
	p := s.prs[s.shown[i]]
	o.Draft = p.IsDraft
	// Author is dropped from the row: it's redundant in a single-author (flat)
	// view and hoisted into the group header when grouped.
	return renderItemRow(o, fmt.Sprintf("#%d", p.Number), p.Title,
		"", ageString(p.UpdatedAt),
		ciGlyph(p.CIState()), reviewDot(p.ReviewDecision))
}
```

(`IssueSection.RenderRow` is untouched — it keeps passing `is.Author.Login`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package — confirms no row test relied on the PR author being present).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): drop redundant inline author from PR rows"
```

---

### Task 3: Render author headers + cursor-line scroll mapping

**Files:**
- Modify: `internal/ui/prlist.go` (add `cursorLine int` to `Model`; rewrite `renderList` to interleave headers and capture the cursor line; rewrite `scrollToCursor` to use it; remove the now-unused `rowLines`)
- Modify: `internal/ui/section.go` (add `groupHeader(author string, width int) string`)
- Test: `internal/ui/prlist_test.go`, `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `PRSection.grouped`, `PRSection.prAt(i) gh.PR`, `computeLayout`, `columnWidths`, `flagGlyph`, `authorStyle`, `sepStyle`.
- Produces: `func groupHeader(author string, width int) string`; `Model.cursorLine int` (the display-line offset of the focused row, set by `renderList`).

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/section_test.go`:

```go
func TestGroupHeaderShowsAuthorAndRule(t *testing.T) {
	h := groupHeader("alice", 40)
	if !strings.Contains(h, "alice") {
		t.Fatalf("group header should name the author: %q", h)
	}
	if !strings.Contains(h, "─") {
		t.Fatalf("group header should draw a rule: %q", h)
	}
	if strings.Contains(h, "\n") {
		t.Fatalf("group header must be a single line: %q", h)
	}
}
```

Add to `internal/ui/prlist_test.go`:

```go
func TestGroupedRenderEmitsHeadersAndTracksCursorLine(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30

	ready := gh.PR{Number: 2, Title: "ready", ReviewDecision: "APPROVED",
		StatusCheckRollup: []gh.Check{{Conclusion: "SUCCESS"}}}
	ready.Author.Login = "bob"
	waiting := gh.PR{Number: 1, Title: "waiting", ReviewDecision: "REVIEW_REQUIRED"}
	waiting.Author.Login = "alice"
	m.setPRs([]gh.PR{waiting, ready})
	m.renderList()

	out := m.vp.View()
	if !strings.Contains(out, "bob") || !strings.Contains(out, "alice") {
		t.Fatalf("grouped board should show both author headers: %q", out)
	}
	// display lines: 0=bob header, 1=bob's #2, 2=alice header, 3=alice's #1.
	// cursor starts at shown row 0 (bob's PR) → line 1.
	if m.cursorLine != 1 {
		t.Fatalf("cursor on first row should map to line 1 (after its header), got %d", m.cursorLine)
	}
	m.moveCursor(1) // to shown row 1 (alice's PR), which sits below a second header
	if m.cursorLine != 3 {
		t.Fatalf("cursor on second group's row should map to line 3, got %d", m.cursorLine)
	}
}

func TestFlatRenderHasNoHeaders(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("r")
	m.width, m.height = 100, 30
	p1 := gh.PR{Number: 1, Title: "one"}
	p1.Author.Login = "alice"
	p2 := gh.PR{Number: 2, Title: "two"}
	p2.Author.Login = "alice"
	m.setPRs([]gh.PR{p1, p2})
	m.renderList()
	if strings.Contains(m.vp.View(), "─") {
		t.Fatalf("single-author board should render flat with no header rules: %q", m.vp.View())
	}
	if m.cursorLine != 0 {
		t.Fatalf("flat board cursor at row 0 should map to line 0, got %d", m.cursorLine)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestGroupHeader|TestGroupedRender|TestFlatRender' -v`
Expected: FAIL — `groupHeader` undefined and `m.cursorLine` undefined (compile errors).

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/section.go`, add the header renderer:

```go
// groupHeader is a dim author-rule line: the login followed by a rule filling
// the row width. It is visual-only — never a selectable cursor target.
func groupHeader(author string, width int) string {
	name := authorStyle(author).Render(author)
	rule := width - lipgloss.Width(name) - 1
	if rule < 0 {
		rule = 0
	}
	return name + " " + sepStyle.Render(strings.Repeat("─", rule))
}
```

In `internal/ui/prlist.go`, add the field to `Model` (next to `cursor`):

```go
	cursor          int // indexes the section's shown set
	cursorLine      int // display-line offset of the cursor row (headers shift it)
```

Rewrite `renderList` to interleave headers and capture the cursor's line:

```go
func (m *Model) renderList() {
	l := computeLayout(m.width, m.height)
	listW := l.ListWidth
	numW := columnWidths(m.section)
	ps, isPR := m.section.(*PRSection)
	grouped := isPR && ps.grouped
	var b strings.Builder
	line, prevAuthor := 0, ""
	for i := 0; i < m.section.Len(); i++ {
		if grouped {
			if a := ps.prAt(i).Author.Login; a != prevAuthor {
				b.WriteString(groupHeader(a, listW) + "\n")
				line++
				prevAuthor = a
			}
		}
		if i == m.cursor {
			m.cursorLine = line
		}
		flag := ""
		if isPR {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		b.WriteString(m.section.RenderRow(i, RowOpts{
			Width: listW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag,
		}))
		b.WriteString("\n")
		line++
	}
	if m.section.Len() == 0 {
		m.cursorLine = 0
	}
	m.vp.SetWidth(listW)
	m.vp.SetHeight(l.ContentHeight)
	m.vp.SetContent(b.String())
	m.scrollToCursor()
}
```

Rewrite `scrollToCursor` to scroll by the captured line, and delete the now-unused `rowLines` constant:

```go
// scrollToCursor nudges the viewport offset only when the cursor row (at its
// display line, headers included) would fall outside the visible window.
func (m *Model) scrollToCursor() {
	top := m.cursorLine
	off := m.vp.YOffset()
	switch {
	case top < off:
		off = top
	case top >= off+m.vp.Height():
		off = top - m.vp.Height() + 1
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}
```

(Delete the `const rowLines = 1` line and its doc comment — `scrollToCursor` no longer references it. Confirm with `grep -rn rowLines internal/ui` that nothing else uses it.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS (full package). Confirm `TestGroupedRenderEmitsHeadersAndTracksCursorLine`, `TestFlatRenderHasNoHeaders`, `TestGroupHeaderShowsAuthorAndRule`, and the existing scroll/debounce tests all pass.

- [ ] **Step 5: Verify no dangling `rowLines`**

Run: `grep -rn rowLines internal/ui` — expect no matches.
Run: `go vet ./internal/ui/` — expect clean.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/section.go internal/ui/prlist_test.go internal/ui/section_test.go
git commit -m "feat(ui): render author headers; scroll by display line"
```

---

### Task 4: Full verification + visual smoke

**Files:** none (verification task)

- [ ] **Step 1: Full test + vet**

Run: `go test ./... && go vet ./...`
Expected: all pass, no vet warnings.

- [ ] **Step 2: Nix build**

Run: `nix build` (from the worktree root)
Expected: builds clean (no `go.mod`/`go.sum` change in this phase).

- [ ] **Step 3: Manual smoke**

Run `./result/bin/prdash` against a repo. With the `f` filter on a multi-author view (e.g. review-requested), confirm: rows are grouped under dim `author ────` headers, no author repeats inline, the most-actionable author's group leads, and `j/k` walks rows top-to-bottom (skipping over headers) while keeping the focused row on-screen near a group boundary. Switch to a single-author view ("mine") and confirm it's flat with no headers and no inline handle.

- [ ] **Step 4: No commit** (verification only). Phase B is complete.
