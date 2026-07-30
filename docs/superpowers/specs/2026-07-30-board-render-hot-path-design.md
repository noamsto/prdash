# Board render hot path, phase 1 — Design

Remove the styled-string round-tripping in `gridHints` and the string-building in
`panelContentRows`. Arithmetic replaces measurement; nothing about the rendering
model changes.

## Measurements

Profiled 2026-07-30 on `BenchmarkParkedRender` (80 PRs, 180×45, cursor parked, no
detail cached), AMD Ryzen 7 PRO 8840HS, `-count=3`:

| Benchmark | Time/op | Allocs/op | Bytes/op |
|---|---|---|---|
| `BenchmarkScrollRender` | 4.29–5.12 ms | 27,082 | 1,287 KB |
| `BenchmarkParkedRender` | 3.43–3.96 ms | 25,895 | 1,142 KB |
| `BenchmarkPreviewPaneOnly` | 0.27–0.33 ms | 1,098 | 86 KB |
| `BenchmarkViewportViewOnly` | 0.24–0.28 ms | 3,997 | 66 KB |

Preview and viewport together account for ~0.56 ms of a ~3.8 ms parked frame.
The remaining ~3.25 ms is board composition — around six-sevenths of the frame,
and none of it preview or viewport.

CPU profile, cumulative share of samples:

| Function | Share |
|---|---|
| `Model.render` → `Model.board` | 84% |
| `Model.renderDocked` | 80% |
| `lipgloss.Style.Render` | 66% |
| `ansi.StringWidth` (grapheme-cluster iteration) | 42% |
| `boxBody` / `titledBoxTinted` | 30% |
| `keysActionsPanel` | 17% |
| `gridHints` | 17% |
| `panelContentRows` / `panelRowsFor` | 13% |

Allocation profile, share of objects: `gridHints.func1` **53%**, reached via
`ansi.Style.ForegroundColor` (30%), `ansi.foregroundColorString` (24%),
`strconv.FormatUint` (16%), `ansi.Style.Styled` (10%).

`gridHints` is a sixth of the CPU but **over half the allocations**.

## Root cause

Three avoidable constructs, all in `internal/ui/prlist.go`.

**1. The measuring loop builds styled strings and discards them** (`:2285`):

```go
render := func(h keyHint) string {
    key := accentStyle.Render(h.key)
    if pad := keyW - lipgloss.Width(h.key); pad > 0 {
        key += strings.Repeat(" ", pad)
    }
    return key + statusBarStyle.Render(" "+h.label)
}
for _, h := range hints {
    if w := lipgloss.Width(render(h)); w > cellW {
        cellW = w
    }
}
```

Each hint is fully styled — two `Style.Render` calls emitting SGR colour
sequences — purely so `lipgloss.Width` can strip that ANSI back off via
grapheme-cluster iteration and return a number. The string is then thrown away.

**2. Emission re-does both** (`:2296`):

```go
s := render(hints[j])
b.WriteString(s)
if j < i+cols-1 && j < len(hints)-1 {
    b.WriteString(strings.Repeat(" ", cellW-lipgloss.Width(s)))
}
```

`render` runs a second time per hint, and `lipgloss.Width` runs again on the
styled result.

**3. Height computation builds the entire grid to call `len` on it** (`:2348`):

```go
func panelContentRows(innerW int) int {
    lw, rw := panelSplit(innerW)
    return max(1+len(gridHints(navHintsFor("pr"), lw, false)),
               1+len(gridHints(defaultActionHints(), rw, true)))
}
```

Two more full grid builds per call, for a row count.

## The fix

ANSI escape sequences have zero display width, so `lipgloss.Width(render(h))` is
computable from the plain strings:

```
Width(render(h)) = Width(h.key) + max(0, keyW - Width(h.key)) + 1 + Width(h.label)
                 = max(keyW, Width(h.key)) + 1 + Width(h.label)
```

The `+1` is the leading space in `statusBarStyle.Render(" "+h.label)`.

Three changes:

1. **Extract the layout arithmetic** into a helper that both callers share:

   ```go
   // gridLayout computes the hint-grid geometry without building any strings.
   func gridLayout(hints []keyHint, width int, alignKeys bool) (cols, cellW, keyW int)
   ```

   It computes `keyW` as today (widest `h.key`, or 0 when `!alignKeys`), then
   `cellW` as the max of `max(keyW, Width(h.key)) + 1 + Width(h.label)` plus the
   existing `gutter = 3`, then `cols = max(1, (width+gutter)/cellW)` — identical
   formulas, no `Style.Render`, and `lipgloss.Width` applied only to unstyled
   `h.key` / `h.label`.

2. **`gridHints` styles each hint once.** It calls `gridLayout`, then builds a
   `cells []string` and a parallel `widths []int` in one pass, and pads with
   `cellW - widths[j]` rather than re-measuring. Its signature and return value
   are unchanged: `func gridHints(hints []keyHint, width int, alignKeys bool) []string`.

3. **`panelContentRows` stops building strings.** It calls `gridLayout` for each
   column and derives the row count arithmetically:

   ```go
   func gridRows(n, cols int) int  // ceil division; 0 when n == 0
   ```

   preserving the `1 +` header line and the `max` of the two columns.

`gridRows` must return 0 for `n == 0`, matching today's `gridHints` early return
of `nil` (so `1 + len(nil)` == 1).

## Non-goals

- **Memoizing `gridHints` output.** The hint lists are static per mode, so a
  cache keyed on (mode, width, `alignKeys`, theme) would take a parked frame's
  hint cost to zero. Deliberately deferred: `applyTheme` rebuilds every style var
  and there is no theme generation counter to invalidate against, so the cache
  needs an invalidation design this change does not need. Revisit after
  re-measuring.
- **Composing the board into a `lipgloss.Canvas`.** The remaining ~50% of frame
  CPU is `lipgloss.Style.Render` spread across `titledBoxTinted`, `boxBody`, and
  the row path, and that cost is inherent to composing styled strings with
  padding. A cell-buffer model addresses it, but the decision should be made
  against post-phase-1 numbers, not today's. Phase 2, gated on re-measurement.
- **Changing any rendered output.** This is a pure refactor; every frame must be
  byte-identical.
- **Touching `BenchmarkScrollRender`'s `renderList` gap** (~0.6–0.8 ms per
  keystroke). Separate concern.

## Correctness

The risk is that a width formula diverges from what `render` actually produces,
misaligning panel columns or changing reserved height — which would surface as
board misalignment rather than a crash.

Existing regression coverage already gates exactly this:

- `TestKeysActionsPanelListsKeysAndActions` — panel content
- `TestLayoutReservesPanelByHeight` — height reservation via `panelContentRows`
- `TestActionPaneHeightIsConstantWhileFiltering` — height stability
- `TestPanelBatchModeShowsOnlyBatchActions` — hint-set switching
- `TestBoardFitsWidthAndSurvivesResize` — full-board width across resize
- `TestFooterToggleNeverOverflowsAcrossResizeSweep` — width sweep, no overflow

Two tests to add, because nothing currently asserts the equivalence this change
relies on:

1. **Width-formula equivalence.** For a table of `keyHint` values covering an
   empty key, a multi-cell nerd-font glyph, a wide/CJK label, and an emoji label,
   assert `gridLayout`'s per-cell width equals `lipgloss.Width` of the styled
   string the old `render` closure produced. This is the load-bearing assumption
   (ANSI is zero-width) stated as a test.
2. **Byte-identical output.** For each of `navHintsFor("pr")`,
   `navHintsFor("issue")`, and `defaultActionHints()`, across a width sweep,
   assert the new `gridHints` output equals a frozen golden captured from the
   current implementation, and that `panelContentRows` returns the same value as
   `max(1+len(gridHints(...)), 1+len(gridHints(...)))` computed the old way.

## Success criteria

- `BenchmarkParkedRender` allocations drop from 25,895/op by at least 40%
  (`gridHints.func1` is 53% of objects; styling once instead of twice, and
  dropping two grid builds in `panelContentRows`, should remove most of it).
- `BenchmarkParkedRender` time/op improves measurably — expect roughly 20–30%
  from the combined `gridHints` (17%) and `panelContentRows`/`panelRowsFor` (13%)
  shares, not all of which is recoverable.
- `lipgloss.Width` no longer appears in any profile path under `gridHints` with a
  *styled* string as its argument.
- Every existing `internal/ui` test passes unchanged.
- Rendered output is byte-identical, proven by the golden test above.
- Re-profile afterwards and record the new split, so the phase-2 Canvas decision
  has current numbers.

## Risks

- **Width formula drift if `render`'s shape changes later.** The formula encodes
  `key + pad + " " + label`. Mitigated by test 1, which fails if the two diverge.
- **The `+1` assumes exactly one leading space.** It comes from
  `statusBarStyle.Render(" "+h.label)`; if that literal changes the formula must
  change with it. Called out in a code comment.
- **Benchmark noise.** Observed spread on `ParkedRender` is 3.43–3.96 ms (~15%),
  so before/after must use `-count` ≥ 6 and `benchstat`, not single runs.
- **Measurements are one geometry.** All numbers come from 80 PRs at 180×45; the
  CPU split may differ at other sizes, so the phase-2 decision should re-profile
  at more than one width.

## Related

- `docs/superpowers/specs/2026-07-30-shared-theme-state-design.md` — unrelated
  fleet work from the same session.
- A Go→Rust rewrite onto Rust's cell-buffer terminal-UI stack was evaluated and
  declined; the perf motivation behind it is addressed here instead. The
  cell-buffer model it would have provided is available in-ecosystem via
  `lipgloss.Canvas`, which is already used for overlay panels at
  `internal/ui/box.go:212` and is phase 2's subject.
- Issue #72 concerns `gridHints` flattening colour; it touches the same function
  but is a separate, behavioural change.
