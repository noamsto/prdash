# Responsive two-line list rows + ⚠ flag alignment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the cramped inline label chips with two responsive list-row modes (height-driven) and fix the misaligned `⚠` flag glyph.

**Architecture:** A row renders in one of two modes chosen by available content height. Two-line mode puts the title (full-width) on line 1 and label pills + head branch on an indented line 2; single-line mode is the current dense line minus chips. The mode is computed in `computeLayout`, carried on `Layout.TwoLine`, and passed to rows via `RowOpts.TwoLine`. `renderList` counts each row's actual line height so the cursor/scroll math stays correct with variable-height rows.

**Tech Stack:** Go, Bubble Tea, Lipgloss v2 (`charm.land/lipgloss/v2`). Tests use the standard `testing` package with `github.com/charmbracelet/x/ansi` for stripping styles.

Spec: `docs/superpowers/specs/2026-07-22-list-row-two-line-layout-design.md` · Issue [#48](https://github.com/noamsto/prdash/issues/48)

## Global Constraints

- Every rendered list line must be **exactly `o.Width` display cells** wide (existing width-contract tests assert `lipgloss.Width(line) == w`). Two-line rows satisfy this per line.
- Follow existing style: no new comments unless they explain a non-obvious WHY; match surrounding naming and error handling.
- Run all UI tests with `go test ./internal/ui/...` from the repo root (`/home/noams/Data/git/.worktrees/noamsto/prdash/feat-48-two-line-rows`).
- `min`/`max` builtins (Go 1.21+) are already used in this package — use them, don't reimplement.

---

### Task 1: `warnGlyph` const + flag alignment fix

**Files:**
- Modify: `internal/ui/theme.go` (add const near the other glyph consts, ~line 245-268)
- Modify: `internal/ui/preview.go:445-457` (`flagGlyph`) and `internal/ui/preview.go:437` (`"⚠ no reviewers"`)
- Modify: `internal/ui/prlist.go:1738` (legend entry)
- Test: `internal/ui/theme_test.go`

**Interfaces:**
- Produces: `const warnGlyph = "⚠︎"` — a 1-cell warning glyph used wherever the raw `"⚠"` appeared.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/theme_test.go`:

```go
func TestWarnGlyphIsSingleCell(t *testing.T) {
	if got := lipgloss.Width(warnGlyph); got != 1 {
		t.Fatalf("warnGlyph must be one cell, got %d (%q)", got, warnGlyph)
	}
	if !strings.Contains(flagGlyph(gh.PRDetail{MergeStateStatus: "DIRTY"}, true), warnGlyph) {
		t.Fatalf("flagGlyph(DIRTY) should render warnGlyph")
	}
}
```

Ensure `theme_test.go` imports `charm.land/lipgloss/v2`, `strings`, and `github.com/noamsto/prdash/internal/gh` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestWarnGlyphIsSingleCell -v`
Expected: FAIL — `undefined: warnGlyph`.

- [ ] **Step 3: Add the const**

In `internal/ui/theme.go`, next to `mergedGlyph`/`closedGlyph` (~line 260):

```go
// warnGlyph is the conflict/behind flag. U+FE0E (VS15) forces text presentation
// so it occupies one terminal cell like ✓/✗/●; the bare U+26A0 defaults to a
// ~2-cell emoji that shoves the row's columns off the monospace grid.
const warnGlyph = "⚠︎"
```

- [ ] **Step 4: Use it at every call site**

In `internal/ui/preview.go`, `flagGlyph`:

```go
	switch {
	case d.MergeStateStatus == "DIRTY" || d.Mergeable == "CONFLICTING":
		return failStyle.Render(warnGlyph)
	case d.MergeStateStatus == "BEHIND":
		return pendStyle.Render(warnGlyph)
	default:
		return ""
	}
```

In `internal/ui/preview.go:437`, replace `pendStyle.Render("⚠ no reviewers")` with:

```go
		return pendStyle.Render(warnGlyph + " no reviewers")
```

In `internal/ui/prlist.go:1738`, replace the legend entry `{"⚠", "conflict / behind base"}` with:

```go
			{warnGlyph, "conflict / behind base"},
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/ -run 'TestWarnGlyph|TestFlag' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/theme.go internal/ui/preview.go internal/ui/prlist.go internal/ui/theme_test.go
git commit -m "fix(ui): render ⚠ flag as one cell with VS15 (#48)"
```

---

### Task 2: Two-line row rendering in `renderItemRow`

**Files:**
- Modify: `internal/ui/section.go` — `RowOpts` (~line 27), delete the `chipRow*` consts (~line 16-24), rewrite `renderItemRow` (~line 338-421), update `PRSection.RenderRow` (~line 95-114) and `IssueSection.RenderRow` (~line 293-297)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: `renderChips(labels []gh.Label, maxW int) string`, `rowBgWrap(line, bg string) string`, `truncate(s string, w int) string` (all unchanged, in `section.go`).
- Produces:
  - `RowOpts` gains `TwoLine bool`.
  - `renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review, auto, sub string, labels []gh.Label) string` — **new `sub` param** (secondary meta for line 2, before `labels`). Returns 1 line when `!o.TwoLine` or the row has neither labels nor `sub`; otherwise `line1 + "\n" + line2`. Every returned line is exactly `o.Width` cells.

- [ ] **Step 1: Write the failing tests**

Replace `TestListRowChipsAppearOnWideRow` and `TestListRowChipsTransitionAtMinWidth` in `internal/ui/section_test.go` with:

```go
// TestRenderRowSingleLineHasNoChips: default (single-line) rows drop labels
// entirely and stay one line at the full width.
func TestRenderRowSingleLineHasNoChips(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	for _, w := range []int{72, 96, 120, 160} {
		row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw})
		if strings.Contains(row, "\n") {
			t.Fatalf("w=%d single-line row must be one line: %q", w, row)
		}
		if strings.Contains(ansi.Strip(row), "bug") {
			t.Errorf("w=%d single-line row must not show chips: %q", w, ansi.Strip(row))
		}
		if got := lipgloss.Width(row); got != w {
			t.Errorf("w=%d single-line row width = %d, want %d", w, got, w)
		}
	}
}

// TestRenderRowTwoLine: with TwoLine on, chips + branch move to an indented
// second line; line 1 carries the title but no chips; both lines are w wide.
func TestRenderRowTwoLine(t *testing.T) {
	p := labeledPR()
	p.HeadRefName = "feat/responsive-rail"
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{p})
	nw := columnWidths(s)
	const w = 100
	row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw, TwoLine: true})
	lines := strings.Split(row, "\n")
	if len(lines) != 2 {
		t.Fatalf("two-line row must be 2 lines, got %d: %q", len(lines), row)
	}
	if strings.Contains(ansi.Strip(lines[0]), "bug") {
		t.Errorf("line 1 must not carry chips: %q", ansi.Strip(lines[0]))
	}
	l2 := ansi.Strip(lines[1])
	if !strings.Contains(l2, "bug") {
		t.Errorf("line 2 must carry chips: %q", l2)
	}
	if !strings.Contains(l2, "feat/responsive-rail") {
		t.Errorf("line 2 must carry the head branch: %q", l2)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Errorf("line %d width = %d, want %d", i, got, w)
		}
	}
}

// TestRenderRowTwoLineNoLabelsNoBranch: a two-line-mode row with nothing for
// line 2 collapses back to a single line (no blank second line).
func TestRenderRowTwoLineNoLabelsNoBranch(t *testing.T) {
	s := NewIssueSection("is:open")
	s.SetIssues([]gh.Issue{{Number: 5, Title: "no labels here"}})
	row := s.RenderRow(0, RowOpts{Width: 100, NumWidth: 4, TwoLine: true})
	if strings.Contains(row, "\n") {
		t.Fatalf("labelless issue row must stay single line in two-line mode: %q", row)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/ui/ -run 'TestRenderRowSingleLineHasNoChips|TestRenderRowTwoLine' -v`
Expected: FAIL — `RowOpts` has no field `TwoLine` (compile error).

- [ ] **Step 3: Add `TwoLine` to `RowOpts` and drop the chip consts**

In `internal/ui/section.go`, delete the const block at lines 16-24 (`chipRowMinWidth`, `chipRowMaxW`, `chipRowMinTitle` and their comment). Add the field to `RowOpts`:

```go
type RowOpts struct {
	Width    int
	NumWidth int // cell width for the right-aligned number column (0 = natural)
	Focused  bool
	Selected bool
	Draft    bool   // dim the title; drafts sort last (see prRank)
	Flag     string // pre-rendered ! column glyph (conflict/behind), "" when unknown
	TwoLine  bool   // render labels + branch on an indented second line
}
```

- [ ] **Step 4: Rewrite `renderItemRow`**

Replace the whole function body (`internal/ui/section.go:338-421`):

```go
func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review, auto, sub string, labels []gh.Label) string {
	w := o.Width
	if w < 24 {
		w = 24 // floor keeps truncation sane before the first WindowSizeMsg
	}
	bar, mark := " ", " "
	if o.Focused {
		bar = focusBarStyle.Render("▎")
	}
	if o.Selected {
		mark = selMarkStyle.Render("●")
	}
	flag := o.Flag
	if flag == "" {
		flag = " "
	}
	if ci == "" {
		ci = dimStyle.Render("·")
	}
	if review == "" {
		review = dimStyle.Render("·")
	}
	if auto == "" {
		auto = " "
	}
	numCell := num
	if o.NumWidth > 0 {
		numCell = padNum(num, o.NumWidth)
	}
	left := bar + mark + " " + ci + " " + review + " " + auto + " " + flag + " " + numStyle.Render(numCell) + " "
	right := authorStyle(author).Render(author) + dimStyle.Render(fmt.Sprintf("  %3s", age))
	leftW, rightW := lipgloss.Width(left), lipgloss.Width(right)

	// The title owns the whole flexible middle — chips no longer share it.
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
	// A draft dims the whole row but paints its tag in the draft accent (peach),
	// so the one thing that stands out on a receded row is what it is.
	draftTag := ""
	if o.Draft {
		const tag = " [draft]"
		draftTag = draftTagStyle.Render(tag)
		if titleRoom -= lipgloss.Width(tag); titleRoom < 1 {
			titleRoom = 1
		}
	}
	titleTxt := titleSt.Render(truncate(title, titleRoom)) + draftTag

	gap := w - leftW - lipgloss.Width(titleTxt) - rightW
	if gap < 1 {
		gap = 1
	}
	line1 := left + titleTxt + strings.Repeat(" ", gap) + right

	// Single-line mode, or a row with nothing to show below, stays one dense line.
	if !o.TwoLine || (len(labels) == 0 && sub == "") {
		if o.Focused {
			line1 = rowBgWrap(line1, theme.RowBg)
		}
		return line1
	}

	// Two-line mode: label pills at full width + a dim secondary (head branch),
	// indented under the title.
	indent := leftW
	avail := w - indent
	subTxt := ""
	if sub != "" {
		subTxt = dimStyle.Render(truncate(sub, max(0, avail/2)))
	}
	subW := lipgloss.Width(subTxt)
	sepW := 0
	if subW > 0 {
		sepW = 2
	}
	chips := ""
	if budget := avail - subW - sepW; budget >= 3 {
		chips = renderChips(labels, budget)
	}
	chipW := lipgloss.Width(chips)
	if chipW == 0 {
		sepW = 0 // no chips → drop the chip/sub separator
	}
	pad := w - indent - chipW - sepW - subW
	if pad < 0 {
		pad = 0
	}
	line2 := strings.Repeat(" ", indent) + chips + strings.Repeat(" ", sepW) + subTxt + strings.Repeat(" ", pad)
	if o.Focused {
		line1 = rowBgWrap(line1, theme.RowBg)
		line2 = rowBgWrap(line2, theme.RowBg)
	}
	return line1 + "\n" + line2
}
```

- [ ] **Step 5: Update the two `RenderRow` callers**

`PRSection.RenderRow` (last statement, ~line 112) — pass the head branch as `sub`:

```go
	return renderItemRow(o, accentStyle, fmt.Sprintf("#%d", p.Number), p.Title,
		"", age, status, reviewDot(p.ReviewDecision), auto, p.HeadRefName, p.Labels)
```

`IssueSection.RenderRow` (~line 295) — issues have no branch, so `sub` is empty (the author stays on line 1):

```go
	return renderItemRow(o, issueAccentStyle, fmt.Sprintf("#%d", is.Number), is.Title,
		is.Author.Login, ageString(is.UpdatedAt), "", "", "", "", is.Labels)
```

- [ ] **Step 6: Fix the direct-call test signature**

`TestRenderItemRowIsSingleLine` in `internal/ui/section_test.go` calls `renderItemRow` directly — add the new `sub` argument (empty) before `nil`:

```go
	row := renderItemRow(o, accentStyle, "#7", "hello world", "alice", "2d",
		ciGlyph("fail"), reviewDot("APPROVED"), autoMergeGlyph(true), "", nil)
```

- [ ] **Step 7: Run the section tests**

Run: `go test ./internal/ui/ -run 'TestRenderRow|TestRenderItemRow|TestRenderChips|TestListRow' -v`
Expected: PASS (the old `TestListRowChips*` names are gone; the new `TestRenderRow*` pass).

- [ ] **Step 8: Full package build + vet**

Run: `go build ./... && go vet ./internal/ui/`
Expected: no output (any other caller of the old `renderItemRow` signature would fail here — there are none outside `section.go`).

- [ ] **Step 9: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): two-line list rows with labels + branch on line 2 (#48)"
```

---

### Task 3: Height-driven `TwoLine` in the layout

**Files:**
- Modify: `internal/ui/layout.go` — add `twoLineMinRows` const (~line 8-19 area), `Layout.TwoLine` field (~line 30-43), set it in both `computeLayout` returns (~line 74-76)
- Test: `internal/ui/layout_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Layout.TwoLine bool`, true when `ContentHeight >= twoLineMinRows` (20).

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/layout_test.go`:

```go
func TestComputeLayoutTwoLineThreshold(t *testing.T) {
	if l := computeLayout(100, 12); l.TwoLine {
		t.Errorf("short window (h=12) should not be two-line: ContentHeight=%d", l.ContentHeight)
	}
	if l := computeLayout(100, 44); !l.TwoLine {
		t.Errorf("tall window (h=44) should be two-line: ContentHeight=%d", l.ContentHeight)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run TestComputeLayoutTwoLineThreshold -v`
Expected: FAIL — `l.TwoLine` undefined (compile error).

- [ ] **Step 3: Add the const**

In `internal/ui/layout.go`, next to the other layout consts (after `footerMinWidth`, ~line 20):

```go
	// twoLineMinRows is the list content height at or above which rows render
	// two lines (title, then labels + branch). Below it, rows stay single-line
	// and drop labels — there isn't the vertical room to spend on a second line.
	twoLineMinRows = 20
```

- [ ] **Step 4: Add the field**

In the `Layout` struct add:

```go
	TwoLine       bool
```

- [ ] **Step 5: Set it in both returns**

In `computeLayout`, after `ch` is finalized (after the `if ch < 1 { ch = 1 }` clamp, ~line 72), compute once and include in both returned structs:

```go
	twoLine := ch >= twoLineMinRows
	if !showSide {
		return Layout{ShowSide: false, ShowFooter: footer, ShowPanel: showPanel, PanelRows: pr, ListWidth: w, ContentHeight: ch, TwoLine: twoLine}
	}
	return Layout{ShowSide: true, ShowFooter: footer, ShowPanel: showPanel, PanelRows: pr, ListWidth: list, SideWidth: side, Gap: gap, ContentHeight: ch, TwoLine: twoLine}
```

- [ ] **Step 6: Run the test**

Run: `go test ./internal/ui/ -run TestComputeLayoutTwoLineThreshold -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/layout.go internal/ui/layout_test.go
git commit -m "feat(ui): compute height-driven TwoLine mode in layout (#48)"
```

---

### Task 4: Wire the mode into the list + fix scrolling for variable-height rows

**Files:**
- Modify: `internal/ui/prlist.go` — `Model` struct add `cursorRows` field (near `cursorLine`, ~line 31 block), `renderList` (~line 203-264), `scrollToCursor` (~line 268-281)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `Layout.TwoLine` (Task 3), `RowOpts.TwoLine` (Task 2).
- Produces: `Model.cursorRows int` (display height of the cursor's row); `renderList` passes `TwoLine: l.TwoLine` and increments its line counter by each row's real height.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/prlist_test.go`:

```go
func TestRenderListTwoLineRowHeight(t *testing.T) {
	p := labeledPR()
	p.HeadRefName = "feat/x"
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 44 // tall → two-line mode
	m.setPRs([]gh.PR{p})
	m.renderList()
	if m.cursorRows != 2 {
		t.Fatalf("labeled PR in two-line mode should be 2 rows tall, got %d", m.cursorRows)
	}
}

func TestRenderListSingleLineRowHeight(t *testing.T) {
	p := labeledPR()
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 100, 12 // short → single-line mode
	m.setPRs([]gh.PR{p})
	m.renderList()
	if m.cursorRows != 1 {
		t.Fatalf("row in single-line mode should be 1 row tall, got %d", m.cursorRows)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/ui/ -run 'TestRenderListTwoLineRowHeight|TestRenderListSingleLineRowHeight' -v`
Expected: FAIL — `m.cursorRows` undefined (compile error).

- [ ] **Step 3: Add the `cursorRows` field**

In `internal/ui/prlist.go`, in the `Model` struct add `cursorRows` beside `cursorLine`:

```go
	cursorLine    int
	cursorRows    int
```

- [ ] **Step 4: Update `renderList`**

In the loop body (`internal/ui/prlist.go:230-243`), replace the cursor bookkeeping + row write so the row is rendered first, its height measured, and the counter advanced by that height:

```go
		flag := ""
		if isPR && ps.prAt(i).State == "OPEN" {
			d, cached := m.detail[ps.prAt(i).Number]
			flag = flagGlyph(d, cached)
		}
		row := m.section.RenderRow(i, RowOpts{
			Width: innerW, NumWidth: numW, Focused: i == m.cursor, Selected: m.sel.has(i), Flag: flag, TwoLine: l.TwoLine,
		})
		rowH := strings.Count(row, "\n") + 1
		if i == m.cursor {
			m.cursorLine = line
			m.cursorRows = rowH
		}
		b.WriteString(row)
		b.WriteString("\n")
		line += rowH
```

Delete the old `if i == m.cursor { m.cursorLine = line }` block that sat before the `RenderRow` call. In the empty-list branch (`m.section.Len() == 0`), set `m.cursorRows = 1` alongside `m.cursorLine = 0`. Confirm `strings` is already imported in `prlist.go` (it is).

- [ ] **Step 5: Update `scrollToCursor`**

Replace `internal/ui/prlist.go:268-281` so it keeps the whole (possibly 2-line) cursor row on screen:

```go
func (m *Model) scrollToCursor() {
	top := m.cursorLine
	rows := m.cursorRows
	if rows < 1 {
		rows = 1
	}
	bottom := top + rows - 1
	off := m.vp.YOffset()
	switch {
	case top < off:
		off = top
	case bottom >= off+m.vp.Height():
		off = bottom - m.vp.Height() + 1
	}
	if off < 0 {
		off = 0
	}
	m.vp.SetYOffset(off)
}
```

- [ ] **Step 6: Run the new tests**

Run: `go test ./internal/ui/ -run 'TestRenderList.*RowHeight' -v`
Expected: PASS.

- [ ] **Step 7: Run the full UI suite**

Run: `go test ./internal/ui/...`
Expected: PASS. If `layout_sweep_regression_test.go` or `overflow_test.go` fail, it means a row exceeded its width contract — re-check the `pad`/`gap` math in Task 2 step 4; do not loosen the tests.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "feat(ui): render two-line rows in the list with correct scroll math (#48)"
```

---

### Task 5: Manual verification + cleanup

**Files:** none (verification only) — plus any small fix the checks surface.

- [ ] **Step 1: Build the binary**

Run: `go build -o /tmp/prdash ./cmd/prdash` (confirm the main package path with `ls cmd/`; adjust if different).
Expected: builds clean.

- [ ] **Step 2: Full test + vet + fmt**

Run: `go test ./... && go vet ./... && gofmt -l internal/ui`
Expected: tests pass, vet silent, `gofmt -l` prints nothing.

- [ ] **Step 3: Eyeball a tall and a short terminal**

Run `/tmp/prdash` in a tall window (≥ ~46 rows): rows show title on line 1, chips + branch indented on line 2, and the `⚠` flag sits on the same column as `✓ ✗ ●`. Shrink the window below the threshold: rows collapse to one line with no chips, title using the full width. Move the cursor onto a two-line row near the bottom edge and confirm both of its lines stay visible when scrolling.

- [ ] **Step 4: Commit any fixes**

Only if step 1-3 surfaced a fix:

```bash
git add -A
git commit -m "fix(ui): <what the manual pass caught> (#48)"
```

---

## Self-Review

**Spec coverage:**
- Two-line mode (title line + indented chips/branch) → Task 2 (render) + Task 4 (list wiring).
- Single-line tight mode, chips removed → Task 2.
- Height trigger `twoLineMinRows` on `Layout.TwoLine` via `RowOpts.TwoLine` → Task 3 (compute) + Task 4 (pass-through).
- `warnGlyph` at all four sites → Task 1.
- Line accounting + `scrollToCursor` for 2-line rows → Task 4.
- `rowBgWrap` covering both focused lines → handled inside `renderItemRow` (Task 2 step 4), which wraps each line before joining — no separate `rowBgWrap` change needed.
- Remove `chipRow*` consts → Task 2 step 3.
- Tests (alignment, two-line, tight, scroll, regressions) → Tasks 1-4.

**Spec deviation (intentional):** the spec floated "issues: author moves to line 2." To avoid variable line-1 content and extra churn, issues keep the author on line 1 (`sub == ""`), so a labelless issue stays single-line even in two-line mode (Task 2, `TestRenderRowTwoLineNoLabelsNoBranch`). PRs always have a head branch, so PR rows are always two lines in two-line mode. This is simpler and matches the mockup, where only PRs are shown.

**Placeholder scan:** none — every code step carries complete code.

**Type consistency:** `renderItemRow` signature (added `sub string` before `labels`) is used identically in both `RenderRow` callers (Task 2 step 5) and the direct test (step 6). `RowOpts.TwoLine`, `Layout.TwoLine`, and `Model.cursorRows` names are consistent across Tasks 2-4.
