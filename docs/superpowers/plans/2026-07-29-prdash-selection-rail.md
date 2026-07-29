# Selection Rail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make prdash's multi-select indicator the row's leftmost bar cell — a heavy pink `▌` — instead of a pink `●` crammed against the CI glyph, and delete the now-redundant `mark` column.

**Architecture:** The dense board row's gutter currently has two state cells: `bar` (cyan `▎` when the row is the cursor) and `mark` (pink `●` when multi-selected), with no separator between `mark` and the CI glyph. Focus is already triple-encoded — bar, row background, and bold title — so the bar cell can be handed to selection instead. Selection wins the cell; focus falls back to the row background and bold title it already had. The `mark` column disappears, narrowing the gutter from 10 display cells to 9.

**Tech Stack:** Go 1.26.3, `charm.land/lipgloss/v2`, `github.com/charmbracelet/x/ansi`. Tests are plain `go test`.

**Spec:** `docs/superpowers/specs/2026-07-29-prdash-selection-rail-design.md`

**Worktree:** all paths are relative to `~/Data/git/.worktrees/noamsto/prdash/feat-selection-rail` (branch `feat/selection-rail`).

## Global Constraints

- The bar cell must render as **exactly one display cell**. Any glyph substituted here must be single-width — `oneCell` guards the grid against zero-width glyphs but cannot shrink an over-wide one, and an over-wide glyph shifts the `#number` column (see the `warnGlyph` comment in `internal/ui/theme.go`).
- Glyph constants live in `internal/ui/theme.go`. It is the single source of glyphs and styles for the UI package.
- `selMarkStyle` is **not** renamed. It is the Select-color style, shared by the board bar, the picker mark (`internal/ui/picker.go:97`), and the header's `N selected` count (`internal/ui/prlist.go:2040`).
- Tests assert the **literal** glyphs `"▌"` and `"▎"`, never the constants. A test asserting `selBarGlyph` would still pass if the constant were changed to the wrong glyph.
- `internal/ui/picker.go:97` keeps its `● ` marker. Out of scope — the picker has no status-glyph cluster to collide with.
- Do **not** touch the row cache (`rowKey`, `internal/ui/prlist.go:443`) or the header's `N selected` counter (`internal/ui/prlist.go:2040`). `rowKey` already keys on both `focused` and `selected`, so both states invalidate cached rows correctly — no change is needed and any change here is out of scope.
- Run tests with `go test ./internal/ui/`. Full suite: `go test ./...`.
- **Pre-existing baseline — do not fix, it is out of scope.** `main` already carries these, verified on the main checkout before this branch started:
  - `gofmt -l internal/ui/` reports `internal/ui/preview.go` and `internal/ui/threads_render_test.go`.
  - `golangci-lint run ./internal/ui/` reports 2 staticcheck `QF1012` findings, at `internal/ui/expanded.go:117` and `internal/ui/expanded.go:133`.

  Scope every formatter and linter check to the files your task actually changed, and judge "clean" against this baseline. Reformatting or refactoring these files is scope creep — leave them alone. Note that `expanded.go` is a Task 3 file, so Task 3 will see those 2 findings; they are not yours.

---

### Task 1: The rail rule — glyph consts and the bar switch

**Files:**
- Modify: `internal/ui/theme.go` (add glyph consts after the `warnGlyph` const; fix the `Select` palette-field comment)
- Modify: `internal/ui/section.go` (the `renderItemRow` doc comment, the bar/mark block, the `gutter` line)
- Test: `internal/ui/section_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: two package-level string constants in `internal/ui/theme.go`, used by Tasks 2 and 3:
  - `focusBarGlyph = "▎"` (U+258E left one-quarter block)
  - `selBarGlyph = "▌"` (U+258C left half block)

---

- [ ] **Step 1: Update the two existing tests that assert the old `●` marker**

In `internal/ui/section_test.go`, find `TestRenderItemRowIsSingleLine`. Its `want` list asserts both `▎` and `●` on a row that is focused *and* selected. Under the new rule neither appears — a focused+selected row shows only `▌`.

Replace this line:

```go
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▎", "●", "⚠", autoMergeGlyph(true)} {
```

with:

```go
	for _, want := range []string{"#7", "hello world", "alice", "2d", "▌", "⚠", autoMergeGlyph(true)} {
```

Then find `TestPRSectionRenderRow` and replace this block:

```go
	sel := s.RenderRow(0, RowOpts{Width: 80, Selected: true})
	if !strings.Contains(sel, "●") {
		t.Fatalf("selected row should carry the ● marker: %q", sel)
	}
```

with:

```go
	sel := s.RenderRow(0, RowOpts{Width: 80, Selected: true})
	if !strings.Contains(sel, "▌") {
		t.Fatalf("selected row should carry the ▌ bar: %q", sel)
	}
```

- [ ] **Step 2: Add the selection-wins-the-bar test**

Append this to `internal/ui/section_test.go`:

```go
// TestSelectedBarWinsOverFocusBar: the row has one bar cell and two states that
// want it. Selection takes it — it is what an action fires against — and focus
// falls back to the row background and bold title (TestFocusedRowGetsBackground).
func TestSelectedBarWinsOverFocusBar(t *testing.T) {
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#1", "title", "", "2d",
			ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false), "", nil)
	}
	cases := []struct {
		name          string
		o             RowOpts
		want, notWant string
	}{
		{"selected", RowOpts{Width: 80, Selected: true}, "▌", "▎"},
		{"focused and selected", RowOpts{Width: 80, Focused: true, Selected: true}, "▌", "▎"},
		{"focused", RowOpts{Width: 80, Focused: true}, "▎", "▌"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := row(c.o)
			if !strings.Contains(got, c.want) {
				t.Errorf("want bar %q: %q", c.want, got)
			}
			if strings.Contains(got, c.notWant) {
				t.Errorf("must not carry bar %q: %q", c.notWant, got)
			}
		})
	}
	if got := row(RowOpts{Width: 80}); strings.Contains(got, "▌") || strings.Contains(got, "▎") {
		t.Errorf("a row that is neither focused nor selected must have no bar: %q", got)
	}
}
```

- [ ] **Step 3: Add the two regression guards**

These two pass on the *current* code as well — they pin behavior the refactor must not break, rather than driving it. Add them anyway; Step 4 explains what to expect.

First, extend the existing `TestFocusedRowGetsBackground` so it covers the selected combinations. Add these two blocks immediately before its closing `}`:

```go
	if got := row(RowOpts{Width: 80, Focused: true, Selected: true}); !strings.Contains(got, set) {
		t.Fatalf("focused+selected row should keep the cursor background: %q", got)
	}
	if got := row(RowOpts{Width: 80, Selected: true}); strings.Contains(got, set) {
		t.Fatalf("selected-but-unfocused row must not carry a background: %q", got)
	}
```

Second, append this new test to the file:

```go
// TestSelectionDoesNotShiftColumnGrid: the bar cell is always exactly one cell
// wide, so toggling focus or selection must never move the #number. The old ●
// lived in a second, dedicated column — this pins the grid now that it is gone.
func TestSelectionDoesNotShiftColumnGrid(t *testing.T) {
	numCol := func(row string) int {
		line := strings.Split(ansi.Strip(row), "\n")[0]
		b := strings.Index(line, "#7")
		if b < 0 {
			t.Fatalf("#7 not found in %q", line)
		}
		return lipgloss.Width(line[:b])
	}
	row := func(o RowOpts) string {
		return renderItemRow(o, accentStyle, "#7", "t", "", "2d",
			ciGlyph("pass"), reviewDot(""), autoMergeGlyph(false), "", nil)
	}
	want := numCol(row(RowOpts{Width: 80, NumWidth: 3}))
	for _, o := range []RowOpts{
		{Width: 80, NumWidth: 3, Selected: true},
		{Width: 80, NumWidth: 3, Focused: true},
		{Width: 80, NumWidth: 3, Focused: true, Selected: true},
	} {
		if got := numCol(row(o)); got != want {
			t.Errorf("focused=%v selected=%v: #number at col %d, want %d",
				o.Focused, o.Selected, got, want)
		}
	}
}
```

No new imports are needed — `strings`, `testing`, `lipgloss`, and `ansi` are already imported by `internal/ui/section_test.go`.

- [ ] **Step 4: Run the tests to verify the right ones fail**

Run:

```bash
go test ./internal/ui/ -run 'TestSelectedBarWinsOverFocusBar|TestRenderItemRowIsSingleLine|TestPRSectionRenderRow|TestFocusedRowGetsBackground|TestSelectionDoesNotShiftColumnGrid' -v
```

Expected — three tests FAIL:
- `TestSelectedBarWinsOverFocusBar/selected` — `want bar "▌"` (current code renders `●`).
- `TestSelectedBarWinsOverFocusBar/focused_and_selected` — both `want bar "▌"` and `must not carry bar "▎"` (current code renders `▎●`).
- `TestRenderItemRowIsSingleLine` — `row missing "▌"`.
- `TestPRSectionRenderRow` — `selected row should carry the ▌ bar`.

Expected — these PASS already, as regression guards:
- `TestSelectedBarWinsOverFocusBar/focused` (current code renders `▎`, no `▌`).
- `TestFocusedRowGetsBackground` (focus already paints the background regardless of selection).
- `TestSelectionDoesNotShiftColumnGrid` (the old `mark` column was also exactly one cell, so the grid does not move today either).

If `TestSelectionDoesNotShiftColumnGrid` or `TestFocusedRowGetsBackground` *fails* here, stop — something about the current row layout differs from what this plan assumes.

- [ ] **Step 5: Add the glyph constants to `internal/ui/theme.go`**

Find the `warnGlyph` constant and its comment block — the declaration uses a Unicode escape, `const warnGlyph = "\uF421"`, with a trailing `// nerd: nf-oct-alert` comment. Immediately **after** that line, insert:

```go
// focusBarGlyph and selBarGlyph share the row's single leftmost cell. The bar
// encodes multi-selection, and marks the cursor row only when that row is not
// selected — on the board row, focus also reads via the row background and a
// bold title, selection has nothing else. selBarGlyph is the heavier block on
// purpose: selection is what an action fires against, so it must read by
// weight and not by hue alone.
// Both must stay single-width; see warnGlyph above.
const (
	focusBarGlyph = "▎" // U+258E left one-quarter block
	selBarGlyph   = "▌" // U+258C left half block
)
```

- [ ] **Step 6: Fix the stale palette comment in `internal/ui/theme.go`**

The `Select` field comment still describes the marker as `●`. Replace:

```go
	Select  string // pink — multi-select ●
```

with:

```go
	Select  string // pink — multi-select bar ▌
```

- [ ] **Step 7: Replace the bar/mark block in `internal/ui/section.go`**

In `renderItemRow`, replace this block:

```go
	bar, mark := " ", " "
	if o.Focused {
		bar = focusBarStyle.Render("▎")
	}
	if o.Selected {
		mark = selMarkStyle.Render("●")
	}
```

with:

```go
	// One cell, two states that want it: selection wins, because it is what an
	// action fires against. Focus still reads via the row background and the bold
	// title further down.
	bar := " "
	switch {
	case o.Selected:
		bar = selMarkStyle.Render(selBarGlyph)
	case o.Focused:
		bar = focusBarStyle.Render(focusBarGlyph)
	}
```

- [ ] **Step 8: Drop `mark` from the gutter in `internal/ui/section.go`**

Replace this block (the comment describes a column that no longer exists):

```go
	// No separator after mark: it is blank on all but multi-selected rows, so it
	// already reads as spacing — and dropping it pulls every row one cell left.
	gutter := bar + mark + ci + " " + review + " " + auto + " " + flag + " "
```

with:

```go
	gutter := bar + ci + " " + review + " " + auto + " " + flag + " "
```

Do **not** touch `indent := lipgloss.Width(gutter)` further down — it measures the gutter, so the two-line form's second line follows the narrowed grid automatically.

- [ ] **Step 9: Update the `renderItemRow` doc comment in `internal/ui/section.go`**

The comment above `renderItemRow` still documents the `mark` column. Replace:

```go
// Single-line (default): ‹bar›‹mark› ‹ci› ‹rv› ‹auto› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
```

with:

```go
// Single-line (default): ‹bar› ‹ci› ‹rv› ‹auto› ‹!› ‹num› ‹title…›            ‹author›  ‹age›
```

- [ ] **Step 10: Run the same tests to verify they pass**

Run:

```bash
go test ./internal/ui/ -run 'TestSelectedBarWinsOverFocusBar|TestRenderItemRowIsSingleLine|TestPRSectionRenderRow|TestFocusedRowGetsBackground|TestSelectionDoesNotShiftColumnGrid' -v
```

Expected: PASS, all five (and all three `TestSelectedBarWinsOverFocusBar` subtests).

- [ ] **Step 11: Run the full package suite — this is the gate on the narrowed gutter**

Run:

```bash
go test ./internal/ui/
```

Expected: `ok  github.com/noamsto/prdash/internal/ui`.

`layout_sweep_regression_test.go` asserts that every row renders to exactly the requested width across a width sweep, and `section_test.go` has several two-line column-alignment tests. The gutter just lost a cell, so these are what prove the title/gap math absorbed it. **Do not edit these tests to make them pass** — if any fails, the gap arithmetic in `renderItemRow` is wrong, and that is the bug to fix.

- [ ] **Step 12: Run the whole suite and the formatter**

Run:

```bash
go test ./...
gofmt -l internal/ui/
golangci-lint run ./internal/ui/
```

Expected: tests `ok`, `gofmt -l` prints **nothing**, golangci-lint reports no issues.

- [ ] **Step 13: Commit**

```bash
git add internal/ui/theme.go internal/ui/section.go internal/ui/section_test.go
git commit -m "feat(ui): multi-select reads as a heavy bar, not a mark beside the CI glyph"
```

---

### Task 2: Legend — name the new bar, and stop listing `●` twice

**Files:**
- Modify: `internal/ui/prlist.go` (the `glyphs` group in `legendGroups`)
- Test: `internal/ui/prlist_test.go`

**Interfaces:**
- Consumes: `focusBarGlyph`, `selBarGlyph` from Task 1.
- Produces: nothing later tasks depend on.

The board legend lists `{"●", "CI running"}` and `{"●", "selected"}` — the same glyph explained two different ways. Task 1 removed the second `●` from the UI, so the legend is now simply wrong as well as ambiguous.

---

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/prlist_test.go`:

```go
// TestLegendGlyphsAreUnambiguous: the legend explains glyphs, so listing one glyph
// under two meanings makes it useless. It shipped with ● as both "CI running" and
// "selected"; the selection bar fixed the UI, this pins the legend.
func TestLegendGlyphsAreUnambiguous(t *testing.T) {
	for _, mode := range []string{"pr", "issue"} {
		// width/height are set because legendGroups reaches computeLayout for the
		// side-pane hint; a zero-size Model would exercise a degenerate layout.
		m := Model{mode: mode, width: 120, height: 40}
		var glyphs []keyHint
		for _, g := range m.legendGroups() {
			if g.title == "glyphs" {
				glyphs = g.hints
			}
		}
		if len(glyphs) == 0 {
			t.Fatalf("mode %q: legend has no glyphs group", mode)
		}
		seen := map[string]string{}
		for _, h := range glyphs {
			if prev, dup := seen[h.key]; dup {
				t.Errorf("mode %q: glyph %q explained twice — %q and %q", mode, h.key, prev, h.label)
			}
			seen[h.key] = h.label
		}
		if got := seen["▌"]; got != "selected" {
			t.Errorf("mode %q: want ▌ labelled \"selected\", got %q", mode, got)
		}
		if got := seen["▎"]; got != "focus" {
			t.Errorf("mode %q: want ▎ labelled \"focus\", got %q", mode, got)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/ui/ -run TestLegendGlyphsAreUnambiguous -v
```

Expected: FAIL, twice per mode — `glyph "●" explained twice — "CI running" and "selected"`, and `want ▌ labelled "selected", got ""`.

- [ ] **Step 3: Update the legend**

In `internal/ui/prlist.go`, inside `legendGroups`, replace:

```go
			{"▎", "focus"}, {"●", "selected"}, {"[draft]", "dimmed"},
```

with:

```go
			{focusBarGlyph, "focus"}, {selBarGlyph, "selected"}, {"[draft]", "dimmed"},
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
go test ./internal/ui/ -run TestLegendGlyphsAreUnambiguous -v
```

Expected: PASS.

- [ ] **Step 5: Run the package suite and formatter**

Run:

```bash
go test ./internal/ui/
gofmt -l internal/ui/prlist.go internal/ui/prlist_test.go
```

Expected: `ok`, and no output from `gofmt -l`. The check is scoped to this task's two files on purpose — `gofmt -l internal/ui/` would also list two files this branch never touched (see the pre-existing baseline in Global Constraints).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/prlist.go internal/ui/prlist_test.go
git commit -m "fix(ui): legend names the selection bar and stops explaining ● twice"
```

---

### Task 3: Adopt `focusBarGlyph` in the two other focus bars

**Files:**
- Modify: `internal/ui/expanded.go` (the checks-list cursor gutter)
- Modify: `internal/ui/logview.go` (the log-line cursor gutter)

**Interfaces:**
- Consumes: `focusBarGlyph` from Task 1.
- Produces: nothing.

Both files build a cursor gutter from a raw `"▎"` literal. Same glyph, same meaning as the board's focus bar — folding them into the constant is the reason for introducing it, and it means a future change to the focus bar cannot miss two of its three sites. Pure substitution: no behavior change, so no new test. The existing suite plus the compiler cover it.

---

- [ ] **Step 1: Substitute in `internal/ui/expanded.go`**

Inside the checks-list loop, replace:

```go
			gutter = focusBarStyle.Render("▎") + " "
```

with:

```go
			gutter = focusBarStyle.Render(focusBarGlyph) + " "
```

- [ ] **Step 2: Substitute in `internal/ui/logview.go`**

Inside the log-line loop, replace:

```go
			gutter = focusBarStyle.Render("▎") + " "
```

with:

```go
			gutter = focusBarStyle.Render(focusBarGlyph) + " "
```

- [ ] **Step 3: Verify no raw focus-bar literals remain outside the constant**

Run:

```bash
rg -n '"▎"' internal/
```

Expected: exactly one hit — the `focusBarGlyph` declaration in `internal/ui/theme.go`. Test files may also match; those are correct (per Global Constraints, tests assert literals deliberately).

- [ ] **Step 4: Run the whole suite and the formatter**

Run:

```bash
go test ./...
gofmt -l internal/ui/expanded.go internal/ui/logview.go
golangci-lint run ./internal/ui/
```

Expected: tests `ok`, no `gofmt -l` output, and golangci-lint reporting **exactly the 2 pre-existing staticcheck `QF1012` findings** at `internal/ui/expanded.go:117` and `internal/ui/expanded.go:133` — no more, no fewer. Those two predate this branch (see the pre-existing baseline in Global Constraints). Do **not** fix them: they sit in a file this task touches, but at unrelated lines, and fixing them is scope creep. If a *third* finding appears, or one appears in `logview.go`, that one is yours.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/expanded.go internal/ui/logview.go
git commit -m "refactor(ui): focus bars share the focusBarGlyph const"
```

---

## Manual Verification

Automated tests cover the glyph, the grid, and the legend, but not whether the rail actually *reads* well — that was the point of the change. Build and drive it:

```bash
go build -o prdash . && ./prdash
```

On a repo with several open PRs, confirm by eye:

1. `space` on a row paints a **heavy pink bar** at the left edge, clearly separated from the CI glyph — no fused `●✕` cluster.
2. Move the cursor onto and off that selected row: the pink bar **stays**, while the grey row background and the bold title track the cursor. The selected row is still identifiable as the cursor row when both apply.
3. Press `V` to select all, then confirm the cursor row is still findable: every bar is pink and no `▎` appears anywhere, so the cursor must read from the row background and bold title alone.
4. Select several adjacent rows — they form a continuous pink rail down the left edge.
5. The `#number` and age columns still line up vertically across selected, focused, and plain rows, in both one-line and two-line modes (`t` toggles two-line if bound; otherwise check a row with labels).
6. Open the legend and confirm it reads `▎ focus` and `▌ selected`, with `●` appearing only as "CI running".
7. `space` in the filter picker (`/`) still shows its own `● ` marker — unchanged, per the spec's non-goals.

Note: per a known prdash quirk, if you are running inside the `prefix+p` tmux popup, clipboard actions silently fail on tmux 3.6 — unrelated to this change, but do not chase it.
