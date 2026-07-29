# prdash — selection rail: the bar column carries multi-select

**Date:** 2026-07-29
**Status:** approved

## Problem

On the dense board row, multi-select renders a pink `●` in a dedicated `mark`
cell that sits immediately left of the CI glyph, with no separator between them
(`internal/ui/section.go:375`):

```go
gutter := bar + mark + ci + " " + review + " " + auto + " " + flag + " "
```

The missing separator is deliberate — the comment on `section.go:373` notes that
`mark` is blank on every row but a selected one, so it already reads as spacing,
and dropping the separator pulls the whole grid one cell left. The cost lands on
exactly the rows that matter: on a selected row the pink `●` fuses with the CI
glyph into one unreadable cluster, and when CI is failing the adjacent red `✗`
makes it worse — two similar-weight marks in similar hues, touching.

Two facts make the `●` column redundant rather than merely cramped:

- **Focus is already double-encoded** — a cyan `▎` bar *and* a `RowBg` row
  background *and* a bold title. It does not need the bar to itself.
- **The legend already lists `●` twice** (`internal/ui/prlist.go:2145`) — once as
  "CI running", once as "selected". The glyph was overloaded before it was
  crowded.

## Design

Delete the `mark` column. The leftmost bar cell becomes the **selection**
channel; focus keeps the row background and bold title it already owns.

| Focused | Selected | bar cell | bar color | row bg | title |
|---|---|---|---|---|---|
| — | — | ` ` | — | none | normal |
| ✓ | — | `▎` U+258E | Focus (cyan) | `RowBg` | bold |
| — | ✓ | `▐` U+2590 | Select (pink) | none | normal |
| ✓ | ✓ | `▐` U+2590 | Select (pink) | `RowBg` | bold |

Selection wins the bar because it is the rarer and more consequential state — it
is what an action fires against. Focus loses no information: it was never
bar-only.

`▐` (right half block) rather than a same-glyph color swap so selection is
encoded by **weight as well as hue** — legible in a screenshot, under a shifted
terminal palette, and to a colorblind user. It sits flush against the column's
right edge, so a run of selected rows forms a solid pink rail.

The gutter narrows from 7 cells to 6 and the whole grid shifts one cell left —
which is what `section.go:373` wanted in the first place.

## Changes

### `internal/ui/theme.go`

Add the two glyph consts beside the existing `warnGlyph` / `mergedGlyph` /
`closedGlyph` — theme.go is the documented single source of glyphs and styles:

```go
focusBarGlyph = "▎" // U+258E, cursor row
selBarGlyph   = "▐" // U+2590, multi-selected row — heavier than focus on purpose
```

Fix the stale comment on the `Select` palette field (line 21), which still
describes the marker as `●`.

`selMarkStyle` is **not** renamed. It is the Select-color style, shared by the
board bar, the picker mark, and the header's `N selected` count.

### `internal/ui/section.go` (~355-375)

Replace the two independent `if`s with one switch and drop `mark` from the
gutter:

```go
bar := " "
switch {
case o.Selected:
	bar = selMarkStyle.Render(selBarGlyph)
case o.Focused:
	bar = focusBarStyle.Render(focusBarGlyph)
}
...
gutter := bar + ci + " " + review + " " + auto + " " + flag + " "
```

Replace the now-obsolete `section.go:373` comment with one explaining that the
bar cell encodes selection and focus falls back to the row background.

Two-line mode needs no change: `indent := lipgloss.Width(gutter)` narrows with
the gutter, so line-2 chips stay aligned under the `#number`.

### `internal/ui/prlist.go:2145`

Legend becomes `{"▎", "focus"}, {"▐", "selected"}`, using the new consts. This
resolves the duplicate-`●` ambiguity noted above.

### `internal/ui/expanded.go:98`, `internal/ui/logview.go:270`

Swap their raw `▎` literals for `focusBarGlyph`. Same glyph, same meaning —
folding them in is the point of introducing the const.

## Tests

Two existing tests assert the old marker and must change:

- `section_test.go:36` — `TestRenderItemRowIsSingleLine` asserts both `▎` and `●`
  on a focused+selected row; under the new rule neither appears. → `▐`.
- `section_test.go:73` — `"selected row should carry the ● marker"`. → `▐`.

Three new tests:

1. **Selection wins the bar** — a focused+selected row contains `▐` and *not*
   `▎`; a focused-only row contains `▎` and not `▐`.
2. **Focus stays legible under selection** — with the bar identical in both, a
   focused+selected row must not render byte-identical to a selected-only row.
   Assert the two rendered strings differ *and* that the focused one still
   carries the `RowBg` background escape. Guards the row-background and bold
   channels from being silently dropped, which is what makes rule 1 safe.
3. **Selection does not shift the column grid** — `#number` lands at the same
   cell with `Selected` true and false. Same shape as the existing
   `TestGutterSurvivesZeroWidthMarker`.

`layout_sweep_regression_test.go` asserts every row renders exactly `w` cells
across a width sweep. The freed cell is absorbed by the title/gap math, so it
should pass unchanged — it is the gate that proves the narrowing is safe, not a
test to edit.

## Verification

Beyond `go test ./...`, run the binary against a repo with a mix of open PRs and
confirm by eye: `space` on a row paints a heavy pink bar, moving the cursor onto
and off a selected row keeps the pink bar while the grey row background and bold
title track the cursor, a run of selected rows reads as a continuous rail, and
the `#`/age columns still line up vertically.

## Non-goals

- **`picker.go:97` keeps its `● `.** The filter picker has no status-glyph
  cluster to collide with and no focus bar to overload — the crowding is
  board-specific.
- No change to the header's `N selected` counter.
- No change to `rowKey` row caching (`prlist.go:443`) — it already keys on both
  `focused` and `selected`, so both states invalidate correctly.
