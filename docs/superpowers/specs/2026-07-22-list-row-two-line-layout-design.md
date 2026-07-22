# Responsive two-line list rows + ⚠ flag alignment

Issue: [#48](https://github.com/noamsto/prdash/issues/48)
Date: 2026-07-22
Status: approved (design mockup reviewed)

## Problem

Two independent defects in the dense list row (`renderItemRow`, `internal/ui/section.go`):

1. **Cramped chips.** Label chips are carved out of the row's flexible middle with
   a bounded budget (`chipRowMaxW = 24`) and vanish entirely below
   `chipRowMinWidth = 72`. On the common narrow window they crowd the title, so
   both the title and the labels read poorly.
2. **Misaligned flag glyph.** The conflict/behind flag uses `"⚠"` (U+26A0,
   `flagGlyph` in `internal/ui/preview.go`). It defaults to emoji presentation —
   colored, ~2 terminal cells — but the layout budgets it as 1 cell (as with
   `✓ ✗ ●`), so it pushes the number column off the monospace grid.

## Approved design

### Row modes (binary, height-driven)

The inline mid-row chip budget is **removed**. A row renders in one of two modes,
selected by the list's available content height:

- **Two-line mode** — real estate available:
  - Line 1: `‹bar›‹mark› ‹ci› ‹rv› ‹auto› ‹⚠› ‹#num›  title…            age`
    — the title takes the whole flexible middle; no inline chips.
  - Line 2: indented under the title — label pills at **full row width** (far more
    fit before the `+N` overflow) followed by a dim secondary: the PR head branch
    (issues: author).
- **Single-line mode** — tight window: the current dense line **with chips
  removed**, so the title reclaims the whole middle. This is the "no room → no
  labels" fallback.

Within a mode, row height is uniform (every row is 2 lines in two-line mode),
which keeps scroll math simple.

**Trigger:** `twoLine = contentHeight >= twoLineMinRows`, with
`twoLineMinRows = 20` (tunable). Computed once in `computeLayout`, carried on
`Layout`, and passed to rows via a new `RowOpts.TwoLine` field. Width is not a
gate: line-2 chips get their own full-width line, so two-line mode helps most
exactly when the inline budget was tightest.

### Flag glyph alignment

Introduce a single const `warnGlyph = "⚠︎"` (VS15 forces text presentation →
1 cell; `lipgloss.Width` already reports 1). Replace the raw `"⚠"` at:

- both `flagGlyph` branches (`internal/ui/preview.go`),
- the `"⚠ no reviewers"` line (`internal/ui/preview.go`),
- the legend entry (`internal/ui/prlist.go`).

### Line accounting & scrolling

`renderList` (`internal/ui/prlist.go`) currently increments its display-line
counter once per row. In two-line mode each item spans 2 display lines:

- increment the line counter by the row's height,
- set `m.cursorLine` to the focused item's **first** line,
- teach `scrollToCursor` the row height so a focused row's label line stays on
  screen (not just its title line).

Group headers and blank separators are unchanged. `rowBgWrap` wraps **both** lines
of the focused item so the cursor background covers the whole item.

### No launch/split change

prdash starts exactly as before. A larger terminal simply crosses the height
threshold (two-line rows) and, past 120 cols, shows the preview pane.

## Components touched

| Unit | File | Change |
|------|------|--------|
| `RowOpts` | `section.go` | add `TwoLine bool` |
| `renderItemRow` | `section.go` | branch: two-line (title-only line 1 + indented chips/branch line 2) vs single-line (no chips); return possibly-2-line string |
| `PRSection.RenderRow` / `IssueSection.RenderRow` | `section.go` | pass secondary meta (PR head branch / issue author) for line 2 |
| `renderChips` | `section.go` | reused for the full-width line-2 budget; inline-budget callers removed |
| `computeLayout` / `Layout` | `layout.go` | compute + carry `TwoLine` (and `twoLineMinRows` const) |
| `renderList` / `scrollToCursor` | `prlist.go` | per-row height in line accounting + cursor scroll |
| `rowBgWrap` | `section.go` | wrap both focused lines |
| `warnGlyph` | `theme.go` (const) | new; used by `preview.go`, `prlist.go` |

`chipRowMinWidth` / `chipRowMaxW` / `chipRowMinTitle` are removed once inline chips
are gone (confirm no other referencing callers first).

## Testing (TDD)

- **Alignment:** `warnGlyph` is exactly 1 cell; a row with a flag and a row
  without one have identical left-column width up to the number.
- **Two-line:** with `TwoLine` on and labels present, the row is 2 lines; line 1
  has no chips; line 2 carries the chips + a dim branch; overflow shows `+N`.
- **Single-line (tight):** `TwoLine` off → 1 line, no chips, title uses the full
  middle.
- **Scrolling:** cursor on a two-line row scrolls so both of its lines are
  visible; group-header + two-line interleaving keeps `cursorLine` correct.
- **Regression:** existing `layout_sweep_regression_test.go`, `overflow_test.go`,
  `section_test.go`, `prlist_test.go` still pass (adjust expectations only where
  the inline-chip removal intentionally changes output).

## Non-goals

- No change to the preview pane, expanded view, or how prdash launches.
- No item-count-based heuristic — the mode depends on window height only.
- No new label styling beyond moving existing chips to their own line.
