# UX Polish (issue #3) remaining-gaps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add colored label chips to LIST rows and a width-responsive two-column expanded rail to the prdash Bubble Tea / Lipgloss TUI, then verify with a manual UX audit.

**Architecture:** Two independent, surgical deliverables against `internal/ui`. Gap A threads a `labels []gh.Label` parameter through the pure `renderItemRow` row renderer and carves a bounded, lowest-priority chip budget from the row's flexible middle (chips elide before the title; exact-fill row invariant preserved). Gap B adds a pure geometry helper `computeExpandedLayout(w, h, isPR)` in `layout.go` (mirroring the existing `Layout`/`computeLayout` pair) and branches `expandedView` on `TwoCol` (PR-gated, `w >= 144`) to place PR metadata in a side rail beside a fixed-110 reading column, while the narrow/issue path keeps today's centered single column. Both gaps are pure-function-first (TDD).

**Tech Stack:** Go 1.26, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `github.com/charmbracelet/x/ansi` (test-side ANSI stripping). Design spec: `docs/superpowers/specs/2026-07-19-ux-polish-gaps-design.md` (authoritative).

## Global Constraints

- **All width/height accounting uses `lipgloss.Width` / `lipgloss.Height`, never `len`** — chips and CJK titles are multi-byte / wide-cell.
- **No border or width bleed:** every rendered segment is measured; the rail is width-clamped to its inner width and height-clamped to `RailH`; nothing may exceed `m.width` or `m.height`.
- **Single geometry funnel:** all expanded-view geometry flows through `computeExpandedLayout` → `setExpandedContent` → `reflowExpanded`. Nothing re-derives expanded height/width elsewhere.
- **Exact-fill invariant (list rows):** every dense row is one line and fills exactly `o.Width` across the width sweep (widths `>= 40`), focused and unfocused.
- **Scope is exactly Gap A + Gap B + audit.** Do not reintroduce removed features (`f`/`F` view keys, filter presets) and do not add any code for requirement #3 (superseded — `enter` stays the context-aware action; `h`/`left` no-wrap already landed).
- **`renderChips(labels, maxW)` contract (unchanged, reused):** returns `""` when `len(labels)==0` or `maxW < 3`; otherwise packs rounded pills to `maxW` cells with a dim ` +N` overflow. `labelChip(name, color)` colors each pill and handles empty/invalid color via its luminance fallback.
- Tuning constants (chip threshold/cap; two-col cutoff/rail clamp) are **tuned against the tests**, not guessed.

---

## File Structure

- `internal/ui/section.go` — Gap A: `renderItemRow` signature (add `labels []gh.Label`) + chip budget/placement; both `RenderRow` call sites (`PRSection` @101, `IssueSection` @284) pass their label slice; new chip tuning consts.
- `internal/ui/section_test.go` — Gap A tests: labeled fixture + chip assertions; update the four existing direct `renderItemRow` callers.
- `internal/ui/layout.go` — Gap B: `ExpandedLayout` struct + `computeExpandedLayout(w, h int, isPR bool)` + tuning consts (pure geometry, no rendering).
- `internal/ui/layout_test.go` — Gap B: table-driven boundary + section-selection + section-aware height tests.
- `internal/ui/expanded.go` — Gap B: `expandedView` branches on `TwoCol`; new `renderExpandedRail`; `setExpandedContent` reads `ContentW`/`VPHeight` from the helper; `expandedBoxWidth` reduced to a logview-only reading-column cap; `expandedChromeRows` + `expandedBoxHeight` retired (height authority moves into the helper).
- `internal/ui/expanded_test.go` — Gap B: two-col within-bounds + resize (wide→narrow→wide) regression test.

**Grounding facts (verified against the tree):**
- `renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review string) string` at `section.go:327`; final composition `left + titleTxt + strings.Repeat(" ", gap) + right` at `section.go:384`; focused rows wrapped by `rowBgWrap` at `section.go:396`.
- `PRSection.RenderRow` at `section.go:101` (author `""`, passes `status`/`reviewDot`); `IssueSection.RenderRow` at `section.go:284` (passes author, `ci`/`review` empty). `gh.PR.Labels` and `gh.Issue.Labels` are both `[]gh.Label`.
- Four existing direct `renderItemRow` test callers: `section_test.go:30`, `:80`, `:101`, `:322`.
- `discussionMaxWidth = 104` (`expanded.go:45`). `expandedBoxWidth() = min(m.width, discussionMaxWidth+6)` (`expanded.go:177`) — **also used by `logview.go:211,392`** (keep it alive for the log viewer).
- `expandedView` at `expanded.go:423`; narrow PR meta line `expandedMeta` at `expanded.go:397` (single joined line); `ciSummary` at `expanded.go:375`; `expandedChromeRows` at `expanded.go:155` (2 + 1 for PR); `expandedBoxHeight` at `expanded.go:165` (min-3 floor); `setExpandedContent` at `expanded.go:183` (min-1 floors); `reflowExpanded` at `expanded.go:212`.
- `gh.PRDetail.ReviewRequests []ReviewRequest` (field `.Login`), `gh.PRDetail.Diffstat()` → `{Files, Additions, Deletions}` (`prview.go:47,52`).
- `min`/`max` builtins are in use already (`section.go:418`, `expanded.go:57`) — use them; clamp = `min(max(v, lo), hi)`.

---

## Task 1: Gap A — bounded label chips in list rows

**Files:**
- Modify: `internal/ui/section.go` (`renderItemRow` @327, `PRSection.RenderRow` @101, `IssueSection.RenderRow` @284; new consts)
- Test: `internal/ui/section_test.go` (new labeled fixture + assertions; update callers @30, @80, @101, @322)

**Interfaces:**
- Consumes: `renderChips(labels []gh.Label, maxW int) string`, `labelChip`, `rowBgWrap`, `truncate` (all existing in `section.go`); `gh.Label{Name, Color}`.
- Produces: `renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review string, labels []gh.Label) string` (labels appended as the final parameter). Consumed only within `section.go`.

- [ ] **Step 1: Write the failing chip tests (labeled fixture + focused + sweep + `+N`)**

Add to `internal/ui/section_test.go`. These call `renderItemRow`/`RenderRow` with the new labels arg, so the package will not compile until Step 3 — that is the intended red.

```go
// labeledPR carries several chips including one with an empty color (exercises
// labelChip's fallback) and enough labels to force a "+N" overflow at a bounded
// budget.
func labeledPR() gh.PR {
	p := gh.PR{Number: 42, Title: "wire up the responsive rail"}
	p.Author.Login = "al"
	p.Labels = []gh.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "ui", Color: ""}, // empty color → labelChip fallback path
		{Name: "backend", Color: "0e8a16"},
		{Name: "needs-review", Color: "fbca04"},
		{Name: "priority", Color: "5319e7"},
	}
	return p
}

func TestListRowChipsAppearOnWideRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	row := s.RenderRow(0, RowOpts{Width: 160, NumWidth: nw})
	if strings.Contains(row, "\n") {
		t.Fatalf("row must be one line: %q", row)
	}
	if lipgloss.Width(row) != 160 {
		t.Errorf("wide labeled row width = %d, want 160", lipgloss.Width(row))
	}
	plain := ansi.Strip(row)
	if !strings.Contains(plain, "bug") {
		t.Fatalf("expected a chip label on a wide row: %q", plain)
	}
}

func TestListRowChipsForceOverflowPlusN(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	// Wide enough to show chips, tight enough that the bounded budget cannot fit
	// all five labels → a dim "+N" overflow must appear.
	row := s.RenderRow(0, RowOpts{Width: 96, NumWidth: nw})
	if lipgloss.Width(row) != 96 {
		t.Errorf("labeled row width = %d, want 96", lipgloss.Width(row))
	}
	if plain := ansi.Strip(row); !strings.Contains(plain, "+") {
		t.Fatalf("expected a +N overflow marker: %q", plain)
	}
}

func TestListRowChipsAbsentOnNarrowRow(t *testing.T) {
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	row := s.RenderRow(0, RowOpts{Width: 60, NumWidth: nw})
	if lipgloss.Width(row) != 60 {
		t.Errorf("narrow row width = %d, want 60 (exact-fill must hold with no chips)", lipgloss.Width(row))
	}
	if plain := ansi.Strip(row); !strings.Contains(plain, "wire up") {
		t.Fatalf("title must survive intact when chips are dropped: %q", plain)
	}
}

func TestFocusedLabeledRowIsExactFillSingleLine(t *testing.T) {
	// Focused rows run through rowBgWrap, which re-injects the row background
	// after every SGR reset, while each chip carries its own labelChip Background.
	// This guards against a per-chip-bg vs row-bg refill bug that a width-only
	// check on an unfocused row would miss.
	s := NewPRSection("is:open")
	s.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(s)
	for _, w := range []int{96, 120, 160, 200} {
		row := s.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: true})
		if strings.Contains(row, "\n") {
			t.Fatalf("w=%d focused labeled row must be one line: %q", w, row)
		}
		if got := lipgloss.Width(row); got != w {
			t.Errorf("w=%d focused labeled row width = %d, want %d", w, got, w)
		}
	}
}
```

Also add a **labeled variant to the existing width sweep**: in `layout_sweep_regression_test.go`, add a new test (do not mutate `sweepPRs`, which other tests rely on carrying no labels) that seeds `labeledPR()` and reuses the exact-fill sweep across `{40, 52, 64, 80, 100, 120, 160, 200}`, focused and unfocused:

```go
func TestDenseRowFillsWidthWithLabels(t *testing.T) {
	ps := NewPRSection("is:open")
	ps.SetPRs([]gh.PR{labeledPR()})
	nw := columnWidths(ps)
	for _, w := range []int{40, 52, 64, 80, 100, 120, 160, 200} {
		for _, focused := range []bool{false, true} {
			row := ps.RenderRow(0, RowOpts{Width: w, NumWidth: nw, Focused: focused})
			if strings.Contains(row, "\n") {
				t.Fatalf("w=%d focused=%v labeled row not single line: %q", w, focused, row)
			}
			if got := lipgloss.Width(row); got != w {
				t.Errorf("w=%d focused=%v labeled row width %d, want %d", w, focused, got, w)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail (red)**

Run: `go test ./internal/ui/ 2>&1 | head -30`
Expected: compile failure — `too many arguments in call to renderItemRow` / `s.RenderRow` uses undefined behavior — the package does not build. This is the intended red.

- [ ] **Step 3: Implement the chip budget + update every call site**

In `internal/ui/section.go`, add the tuning consts near the top of the file (values are a starting point; tune against the sweep in Step 4):

```go
const (
	// chipRowMinWidth is the row width below which list rows show no chips —
	// the title keeps the whole flexible middle on tight rows.
	chipRowMinWidth = 72
	// chipRowMaxW caps the chip budget so labels never starve the title.
	chipRowMaxW = 24
	// chipRowMinTitle is the title floor the chip budget must never squeeze below.
	chipRowMinTitle = 16
)
```

Change the signature and body of `renderItemRow` (append `labels []gh.Label`; reserve a bounded, lowest-priority chip budget; place chips just left of the right block; recompute the final `gap` from measured widths). Replace the region from the `titleRoom` computation (`section.go:357`) through the final `line`/return so it reads:

```go
func renderItemRow(o RowOpts, numStyle lipgloss.Style, num, title, author, age, ci, review string, labels []gh.Label) string {
	// ... unchanged up through: left, right, leftW, rightW ...

	// Reserve a bounded chip budget from the flexible middle. Chips are the
	// lowest-priority content, so they elide before the title on tight rows and
	// vanish entirely below chipRowMinWidth. Placed immediately left of the
	// right (age) block.
	chips := ""
	if w >= chipRowMinWidth {
		slack := w - leftW - rightW - chipRowMinTitle - 2 // -2: title/right separators
		budget := min(chipRowMaxW, slack)
		if budget >= 3 { // renderChips floor
			chips = renderChips(labels, budget)
		}
	}
	chipSeg := ""
	if chips != "" {
		chipSeg = chips + " " // one space between chips and the right block
	}
	chipW := lipgloss.Width(chipSeg)

	titleRoom := w - leftW - rightW - chipW - 2
	if titleRoom < 1 {
		titleRoom = 1
	}
	// ... unchanged titleSt selection + draftTag squeeze producing titleTxt ...

	gap := w - leftW - lipgloss.Width(titleTxt) - chipW - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + titleTxt + strings.Repeat(" ", gap) + chipSeg + right
	if o.Focused {
		line = rowBgWrap(line, theme.RowBg)
	}
	return line
}
```

Update the two production call sites:
- `PRSection.RenderRow` (`section.go:101`): append `p.Labels` as the final arg.
- `IssueSection.RenderRow` (`section.go:284`): append `is.Labels` as the final arg.

**Update the four existing direct `renderItemRow` test callers** so the package compiles (append `nil` — these tests assert non-chip behavior):
- `section_test.go:30` (`TestRenderItemRowIsSingleLine`) → `..., reviewDot("APPROVED"), nil)`
- `section_test.go:80` (`TestDraftRowIsStyledDistinctly`) → `..., reviewDot(""), nil)`
- `section_test.go:101` (`TestDraftRowShowsDraftTag`) → `..., reviewDot(""), nil)`
- `section_test.go:322` → `..., reviewDot(""), nil)`

- [ ] **Step 4: Run the tests to verify they pass (green); tune consts if the sweep fails**

Run: `go test ./internal/ui/ -run 'TestListRow|TestFocusedLabeledRow|TestDenseRowFillsWidth|TestRenderItemRow|TestDraftRow|TestPRSection' -v 2>&1 | tail -40`
Expected: PASS. If `TestDenseRowFillsWidthWithLabels` or `TestListRowChipsForceOverflowPlusN` fails on an off-by-one, adjust `chipRowMaxW` / `chipRowMinTitle` / the `-2` separator accounting (all width math via `lipgloss.Width`) until exact-fill holds at every swept width and the `+N` still appears at `w=96`. Do not weaken the assertions.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/section.go internal/ui/section_test.go internal/ui/layout_sweep_regression_test.go
git commit -m "feat(ui): colored label chips in list rows (Gap A)"
```

---

## Task 2: Gap B (part 1) — `computeExpandedLayout` pure geometry helper

**Files:**
- Modify: `internal/ui/layout.go` (add struct + func + consts)
- Test: `internal/ui/layout_test.go` (append table-driven tests)

**Interfaces:**
- Consumes: `discussionMaxWidth` (const in `expanded.go:45`, same package); `min`/`max` builtins.
- Produces:
  ```go
  type ExpandedLayout struct {
      TwoCol   bool // true only for a PR wide enough for a side rail
      RailW    int  // outer width of the left metadata rail (0 when !TwoCol)
      RailH    int  // rail body height (== content body height)
      ContentW int  // outer width of the right (tabbed) content pane
      VPHeight int  // viewport body height (content body minus tab strip + border rows)
  }
  func computeExpandedLayout(w, h int, isPR bool) ExpandedLayout
  ```
  Plus package consts `expandedRailMin=32`, `expandedRailMax=44`, `expandedColGap=2`, `expandedContentCap=discussionMaxWidth+6` (110), `expandedTwoColMin=expandedContentCap+expandedRailMin+expandedColGap` (144). Consumed by Task 3.

- [ ] **Step 1: Write the failing table test (boundary + section selection + section-aware height)**

Append to `internal/ui/layout_test.go`:

```go
func TestComputeExpandedLayoutSelection(t *testing.T) {
	const h = 40
	cases := []struct {
		name                   string
		w                      int
		isPR                   bool
		twoCol                 bool
		contentW, railW, vpH   int
	}{
		// PR: TwoCol false at 143, true at 144 (the expandedTwoColMin boundary).
		{"pr-just-below", 143, true, false, 110, 0, 35},
		{"pr-at-cutoff", 144, true, true, 110, 32, 36},
		{"pr-wide", 200, true, true, 110, 44, 36},
		{"pr-narrow", 90, true, false, 90, 0, 35},
		// Issue: never two-col, even wide → no dead rail.
		{"issue-wide", 160, false, false, 110, 0, 36},
		{"issue-narrow", 90, false, false, 90, 0, 36},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := computeExpandedLayout(c.w, h, c.isPR)
			if l.TwoCol != c.twoCol {
				t.Errorf("TwoCol = %v, want %v", l.TwoCol, c.twoCol)
			}
			if l.ContentW != c.contentW {
				t.Errorf("ContentW = %d, want %d", l.ContentW, c.contentW)
			}
			if l.RailW != c.railW {
				t.Errorf("RailW = %d, want %d", l.RailW, c.railW)
			}
			if l.VPHeight != c.vpH {
				t.Errorf("VPHeight = %d, want %d", l.VPHeight, c.vpH)
			}
			if c.twoCol && l.RailW+expandedColGap+l.ContentW > c.w {
				t.Errorf("two-col columns %d+%d+%d exceed w=%d", l.RailW, expandedColGap, l.ContentW, c.w)
			}
		})
	}
}

func TestComputeExpandedLayoutSectionAwareHeight(t *testing.T) {
	const w, h = 90, 40
	pr := computeExpandedLayout(w, h, true)   // narrow PR: carries a meta row
	iss := computeExpandedLayout(w, h, false) // narrow issue: no meta row
	if pr.VPHeight != iss.VPHeight-1 {
		t.Errorf("narrow PR VPHeight = %d, want one less than issue %d", pr.VPHeight, iss.VPHeight)
	}
	// A two-col PR must NOT lose a row to a phantom narrow-meta line.
	twoCol := computeExpandedLayout(160, h, true)
	if twoCol.VPHeight != iss.VPHeight {
		t.Errorf("two-col PR VPHeight = %d, want %d (no phantom meta row)", twoCol.VPHeight, iss.VPHeight)
	}
}
```

- [ ] **Step 2: Run to verify it fails (red)**

Run: `go test ./internal/ui/ -run TestComputeExpandedLayout 2>&1 | head -20`
Expected: compile failure — `undefined: computeExpandedLayout` / `undefined: expandedColGap`.

- [ ] **Step 3: Implement the helper + consts in `layout.go`**

Append to `internal/ui/layout.go`:

```go
const (
	expandedRailMin    = 32 // rail never narrower than this in two-col
	expandedRailMax    = 44 // …nor wider (a metadata rail past ~44 is wasted)
	expandedColGap     = 2  // cells between rail and content
	expandedContentCap = discussionMaxWidth + 6 // 110, the reading-column cap (was in expandedBoxWidth)
	// two-col only when a full rail AND a full-width content pane both fit.
	expandedTwoColMin = expandedContentCap + expandedRailMin + expandedColGap // 144
)

// ExpandedLayout is the computed geometry for the expanded detail frame. It is
// the single height/width authority for that view — callers never re-derive.
type ExpandedLayout struct {
	TwoCol   bool
	RailW    int
	RailH    int
	ContentW int
	VPHeight int
}

// computeExpandedLayout derives the expanded-view geometry from the terminal
// size and section kind. Two-col is PR-only (issues stay a centered single
// column at every width). The chrome/meta row count is section-aware: a PR
// carries a one-line meta row only in narrow mode (in two-col that content
// moves into the rail), so there is one height authority and no narrow-PR
// off-by-one. Floors mirror today's expandedBoxHeight (min-3) and
// setExpandedContent (min-1) so tiny terminals never hand vp a negative.
func computeExpandedLayout(w, h int, isPR bool) ExpandedLayout {
	twoCol := isPR && w >= expandedTwoColMin

	metaRows := 0
	if isPR && !twoCol {
		metaRows = 1
	}
	body := h - (2 + metaRows) // head + footer (+ narrow-PR meta)
	if body < 3 {
		body = 3
	}

	l := ExpandedLayout{TwoCol: twoCol}
	if twoCol {
		l.ContentW = expandedContentCap
		l.RailW = min(max(w-expandedColGap-l.ContentW, expandedRailMin), expandedRailMax)
		l.RailH = body
	} else {
		l.ContentW = min(w, expandedContentCap)
	}
	l.VPHeight = body - 2 // tabbedBox top tab/border line + bottom border row
	if l.VPHeight < 1 {
		l.VPHeight = 1
	}
	if l.ContentW < 1 {
		l.ContentW = 1
	}
	return l
}
```

- [ ] **Step 4: Run to verify it passes (green)**

Run: `go test ./internal/ui/ -run TestComputeExpandedLayout -v 2>&1 | tail -20`
Expected: PASS — both tests, all sub-cases.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/layout.go internal/ui/layout_test.go
git commit -m "feat(ui): add computeExpandedLayout geometry helper (Gap B)"
```

---

## Task 3: Gap B (part 2) — two-column expanded view wired through the helper

**Files:**
- Modify: `internal/ui/expanded.go` (`setExpandedContent`, `expandedView`, new `renderExpandedRail`; reduce `expandedBoxWidth`; remove `expandedChromeRows` + `expandedBoxHeight`)
- Test: `internal/ui/expanded_test.go` (two-col within-bounds + resize regression)

**Interfaces:**
- Consumes: `computeExpandedLayout`, `expandedContentCap`, `expandedColGap`, `ExpandedLayout` (Task 2); `renderChips`, `ciSummary`, `expandedMeta`, `tabbedBox`, `indentLines`, `truncate`, `authorStyle`, `dimStyle`, `titleStyle`; `gh.PRDetail{ReviewRequests, Diffstat()}`.
- Produces: `renderExpandedRail(pr gh.PR, d gh.PRDetail, l ExpandedLayout) string` (method on `Model`). Internal to `expanded.go`.

- [ ] **Step 1: Write the failing two-col bounds + resize test**

Append to `internal/ui/expanded_test.go`. Mirror `layout_sweep_regression_test.go` — ANSI-decode every line and assert width bounds; drive a real resize through `Update`.

```go
func TestExpandedTwoColStaysWithinBounds(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	m.detail[7] = gh.PRDetail{
		ReviewRequests: []gh.ReviewRequest{{Login: "octocat"}},
		Files:          []gh.DiffFile{{Path: "internal/ui/expanded.go", Additions: 40, Deletions: 5}},
	}
	m.loaded = true
	const w, h = 180, 45
	u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = u.(Model)
	m.enterExpanded()

	l := computeExpandedLayout(w, h, true)
	if !l.TwoCol {
		t.Fatalf("expected two-col at %dx%d", w, h)
	}
	for i, ln := range strings.Split(m.expandedView(), "\n") {
		if lw := lipgloss.Width(ln); lw > w {
			t.Errorf("two-col line %d width %d exceeds terminal width %d", i, lw, w)
		}
	}
	if fh := lipgloss.Height(m.expandedView()); fh > h {
		t.Errorf("two-col frame height %d exceeds terminal height %d", fh, h)
	}
}

func TestExpandedSurvivesResizeAcrossTwoColBoundary(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("owner/repo")
	m.setPRs(sweepPRs())
	m.detail[7] = gh.PRDetail{Files: []gh.DiffFile{{Path: "a.go", Additions: 1, Deletions: 1}}}
	m.loaded = true
	u, _ := m.Update(tea.WindowSizeMsg{Width: 180, Height: 45})
	m = u.(Model)
	m.enterExpanded()

	for _, sz := range [][2]int{{180, 45}, {100, 30}, {160, 24}, {90, 40}, {200, 50}} {
		w, hh := sz[0], sz[1]
		u, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: hh})
		m = u.(Model)
		for i, ln := range strings.Split(m.expandedView(), "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("%dx%d: line %d width %d exceeds %d", w, hh, i, lw, w)
			}
		}
		if fh := lipgloss.Height(m.expandedView()); fh > hh {
			t.Errorf("%dx%d: frame height %d exceeds %d", w, hh, fh, hh)
		}
	}
}
```

(If `NewModel`/`setPRs`/`enterExpanded`/`SetRepo` signatures differ from the sweep test's usage, mirror exactly what `TestBoardFitsWidthAndSurvivesResize` in `layout_sweep_regression_test.go` does — it is the reference for driving a resize through one model.)

- [ ] **Step 2: Run to verify it fails (red)**

Run: `go test ./internal/ui/ -run TestExpanded 2>&1 | head -30`
Expected: FAIL — today's `expandedView` builds only a centered single column, so at `180x45` either the two-col assertion is unmet (no rail rendered) or the frame does not use the width as expected. Confirm it is a real assertion failure, not a compile error.

- [ ] **Step 3: Implement — rail renderer, branch `expandedView`, funnel `setExpandedContent`, retire dead helpers**

In `internal/ui/expanded.go`:

(a) **Reduce `expandedBoxWidth` to the logview-only reading-column cap** (value unchanged; `logview.go:211,392` keep working):

```go
// expandedBoxWidth is the reading-column cap used by the full-screen log viewer.
// The PR/Issue expanded view derives its width from computeExpandedLayout.
func (m Model) expandedBoxWidth() int {
	return min(m.width, expandedContentCap)
}
```

(b) **Remove `expandedChromeRows` (`:155`) and `expandedBoxHeight` (`:165`)** — the height authority now lives in `computeExpandedLayout`. Verify no remaining callers: `grep -n "expandedChromeRows\|expandedBoxHeight" internal/ui/*.go` must return nothing after this edit.

(c) **Add the rail renderer** (width-clamped to inner `RailW-2`, height-clamped to `RailH`, all widths via `lipgloss.Width`/`truncate`):

```go
// renderExpandedRail builds the PR metadata side rail for the two-col expanded
// view: #num + title, author, branch→base, label chips (full renderChips at the
// rail's inner width, not w/3), requested reviewers, and a CI + diffstat
// one-liner. Width-clamped to RailW and height-clamped to RailH so a long label
// or reviewer set can never bleed past the column or push the frame past h.
func (m Model) renderExpandedRail(pr gh.PR, d gh.PRDetail, l ExpandedLayout) string {
	inner := l.RailW - 2
	if inner < 1 {
		inner = 1
	}
	var lines []string
	lines = append(lines, titleStyle.Bold(true).Render(truncate(fmt.Sprintf("#%d %s", pr.Number, pr.Title), inner)))
	if pr.Author.Login != "" {
		lines = append(lines, authorStyle(pr.Author.Login).Render(truncate("@"+pr.Author.Login, inner)))
	}
	if pr.HeadRefName != "" {
		lines = append(lines, dimStyle.Render(truncate(pr.HeadRefName+"→"+pr.BaseRefName, inner)))
	}
	if chips := renderChips(pr.Labels, inner); chips != "" {
		lines = append(lines, chips)
	}
	for _, r := range d.ReviewRequests {
		lines = append(lines, dimStyle.Render(truncate("• "+r.Login, inner)))
	}
	lines = append(lines, ciSummary(pr))
	if s := d.Diffstat(); s.Files > 0 {
		lines = append(lines, dimStyle.Render(truncate(fmt.Sprintf("%d files +%d -%d", s.Files, s.Additions, s.Deletions), inner)))
	}
	if len(lines) > l.RailH {
		lines = lines[:l.RailH]
	}
	return lipgloss.NewStyle().Width(l.RailW).Height(l.RailH).Render(strings.Join(lines, "\n"))
}
```

(d) **Funnel `setExpandedContent` through the helper** (replace `expanded.go:183-195`):

```go
func (m *Model) setExpandedContent() {
	_, isPR := m.section.(*PRSection)
	l := computeExpandedLayout(m.width, m.height, isPR)
	w := l.ContentW - 2
	if w < 1 {
		w = 1
	}
	m.vp.SetWidth(w)
	m.vp.SetHeight(l.VPHeight)
	m.vp.SetContent(m.expandedBody(w))
}
```

(e) **Branch `expandedView` on `TwoCol`** (replace `expanded.go:423-450`). Head + footer span the full block width in both modes; the `JoinHorizontal(rail, gap, contentBox)` sits between them; leftover width becomes an `indentLines` centering margin exactly as today's single column:

```go
func (m Model) expandedView() string {
	n := 0
	if v, ok := m.cursorVars(); ok {
		n = v.Number
	}
	ps, isPR := m.section.(*PRSection)
	l := computeExpandedLayout(m.width, m.height, isPR)

	blockW := l.ContentW
	if l.TwoCol {
		blockW = l.RailW + expandedColGap + l.ContentW
	}

	head := headerStyle.Render(fmt.Sprintf("  %s #%d", m.repo, n))
	if isPR {
		if title := ps.prAt(m.cursor).Title; title != "" {
			if avail := blockW - lipgloss.Width(head) - 4; avail > 12 { // truncate vs FULL block width, not ContentW
				head += dimStyle.Render("  " + truncate(title, avail))
			}
		}
	}
	head += m.statusBadge()
	foot := statusBarStyle.Render(m.expandedFooter())

	contentBox := tabbedBox(m.vp.View(), l.ContentW, l.VPHeight+2, expandedTabs, m.expandedTab)

	var mid string
	if l.TwoCol {
		rail := m.renderExpandedRail(ps.prAt(m.cursor), m.detail[n], l)
		gap := lipgloss.NewStyle().Width(expandedColGap).Render("")
		mid = lipgloss.JoinHorizontal(lipgloss.Top, rail, gap, contentBox)
	} else {
		parts := []string{}
		if isPR {
			parts = append(parts, m.expandedMeta(ps.prAt(m.cursor), l.ContentW-2)) // narrow PR keeps its one-line meta
		}
		parts = append(parts, contentBox)
		mid = strings.Join(parts, "\n")
	}

	out := strings.Join([]string{head, mid, foot}, "\n")
	if blockW < m.width { // center the block in a wide terminal
		out = indentLines(out, (m.width-blockW)/2)
	}
	return out
}
```

Note: `contentBox` outer height is `l.VPHeight + 2` (== `RailH`), so the rail and content pane are equal height for `JoinHorizontal`. `m.detail[n]` returns the zero `PRDetail` when uncached — the rail degrades gracefully (no reviewers/diffstat, `ciSummary` still renders).

- [ ] **Step 4: Run the Gap-B and full-package tests to verify green**

Run: `go test ./internal/ui/ -run TestExpanded -v 2>&1 | tail -30`
Expected: PASS. Then confirm nothing else regressed (resize/sweep/logview): `go test ./internal/ui/ 2>&1 | tail -20` → `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/expanded_test.go
git commit -m "feat(ui): width-responsive two-column expanded rail (Gap B)"
```

---

## Task 4: Deterministic gate (loop to green) + manual UX audit

**Files:** none (verification only).

- [ ] **Step 1: Run the fast deterministic gate; loop until green**

Run each; fix any failure and re-run the whole sequence until all pass:

```bash
go build ./...
go vet ./...
golangci-lint run ./internal/ui/   # only if the binary is available; skip cleanly if not
go test ./...
```

Expected: `go build`/`go vet` silent (exit 0); `golangci-lint` clean (or skipped when unavailable); `go test ./...` ends `ok`. Do not proceed to the audit until every command is green.

- [ ] **Step 2: Manual UX audit — build the binary and drive the TUI**

Build and run the real TUI (per the repo's normal run path). Eyeball, in **both light and dark themes**:
- **Gap A:** chip alignment and contrast in PR **and** Issue list rows; a long title on a medium-width row shows few/no chips (title priority) while a wide row shows chips; `+N` overflow reads clearly; rows with no labels look exactly as before (no stray separator); focused (hovered) labeled row keeps its row background across the chips (no bg gap).
- **Gap B:** two-column framing at wide widths (`>= 144`) — rail carries #num+title, author, branch→base, chips, reviewers, CI/diffstat; reading column is the same width as before; single-column fallback below 144; **Issue expanded view stays centered single-column at every width** (no dead rail).
- **Resize:** live-resize across the two-col boundary (and the `sideThreshold`=120 board boundary) — no bleed, no frame taller than the terminal, scroll position preserved.
- Footer hint accuracy; empty states (no labels, no reviewers, no checks).

- [ ] **Step 3: Fix any nits found; re-run the gate; commit**

These are polish edits (alignment/contrast/truncation), not new behavior. After each fix re-run Step 1 to green.

```bash
git add -A
git commit -m "fix(ui): UX-audit polish for chips + expanded rail"
```

---

## Self-Review (against the spec)

**1. Spec coverage**
- Gap A (chips in PR + Issue rows, bounded budget placed last, exact-fill) → Task 1.
- Issue rows get chips (one-line change at `IssueSection.RenderRow`) → Task 1 Step 3.
- Gap B struct/func/consts, PR-gated `TwoCol` at `w>=144`, `ContentW=110`, `RailW=clamp(w-2-110,32,44)`, section-aware chrome, `VPHeight` from the helper, floors → Task 2.
- `expandedView` branch, rail renderer, `setExpandedContent` funnel, resize survival, `expandedBoxWidth` retired as the expanded source (kept for logview), head truncation vs full block width → Task 3.
- Requirement #3 → **no task** (superseded / already landed), as instructed.
- Testing plan: labeled fixture incl. empty-color label + `+N` (Task 1 S1); focused+chips exact-fill / `rowBgWrap` (Task 1 S1); width-sweep exact-fill (Task 1 S1); `computeExpandedLayout` boundary+section+height table with exact values `TwoCol false@143/true@144`, `ContentW==110`, `RailW==32@144`, `RailW==44@200`, issue `TwoCol==false@>=144`, section-aware `VPHeight` (Task 2 S1); rail within-bounds + resize regression (Task 3 S1) → all covered.
- UX audit (build + drive + fix nits) → Task 4.
- Fast gate (build/vet/lint/test looped to green) → Task 4 Step 1.

**2. Placeholder scan** — no `TBD`/`add error handling`/`similar to`/`write tests for the above`; every code step shows the actual code and every run step names the command + expected output. Tuning consts have concrete starting values with an explicit tune-against-test step.

**3. Type consistency** — `renderItemRow(..., labels []gh.Label)` used identically at both prod call sites and all four test callers; `computeExpandedLayout(w, h int, isPR bool) ExpandedLayout` and its field names (`TwoCol/RailW/RailH/ContentW/VPHeight`) match between Task 2's definition and Task 3's use; `renderExpandedRail(pr gh.PR, d gh.PRDetail, l ExpandedLayout)` consumes verified `gh.PRDetail.ReviewRequests[].Login` and `Diffstat().{Files,Additions,Deletions}`; `expandedContentCap`/`expandedColGap` shared consts. `expandedBoxWidth` retained (logview); `expandedChromeRows`/`expandedBoxHeight` removed with a grep guard.

---

## Execution notes (charm/lipgloss)

- Every width decision uses `lipgloss.Width`; tests ANSI-strip with `ansi.Strip` before measuring where they check plain text.
- No segment may exceed its column: the rail is `lipgloss.NewStyle().Width(RailW).Height(RailH)`-boxed and its contents `truncate`d to inner width; the row `gap` is recomputed from measured segment widths so exact-fill holds with chips present.
- `JoinHorizontal(lipgloss.Top, rail, gap, contentBox)` requires equal heights — `RailH == VPHeight+2 ==` content-box outer height by construction.
- Resize correctness rests on the single funnel: `computeExpandedLayout` → `setExpandedContent` → `reflowExpanded` (which preserves + re-clamps the scroll offset). Do not compute expanded width/height anywhere else.
