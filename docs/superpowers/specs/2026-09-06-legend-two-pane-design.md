# Legend: two panes, keyed to the row's gutter grammar

Issue: #120

## Problem

A red `⚠` appeared in a stacked view and the operator could not say what it
meant. The legend does document it — `⚠ conflict / behind base`, `prlist.go:2809`
— which makes this a design failure rather than a missing entry. The entry is
there and it does not teach.

The row encodes state in **two dimensions the legend has no way to express**:

```
[bar][CI] [review] [auto] [flag] [tree][#num]  title ⧉+N  TICKET  author  +diff  age
  0    1      2       3      4
```

**Column position.** `✗` in column 1 is a failed check; `✗` in column 2 is
changes requested. Same glyph, same `failStyle` red. Only the column separates
them, and the legend lists them six entries apart without naming a column.

**Hue.** `flagGlyph` (`preview.go:574`) paints `DIRTY`/`CONFLICTING` red and
`BEHIND` yellow. The legend collapses both into one red entry joined by a slash.

A flat fourteen-entry bag cannot carry either. That is the whole problem; the
rest follows from it.

## Defects

1. `✗` red twice — `prlist.go:2803` (checks failed) and `:2812` (changes
   requested), both `&failStyle`.
2. `·` documented once ("no CI", `:2805`) but emitted by `ciGlyph`
   (`theme.go:324`), `reviewDot` (`section.go:1187`), and `renderItemRow`'s
   empty-cell fill (`section.go:758-763`) with three different meanings.
3. `reviewApprovedGlyph` (`theme.go:278`) has **no entry**. It is the commonest
   column-2 state.
4. One red `⚠` for two states, as above.
5. `⧉+N` (`section.go:479`) undocumented.
6. Stack tree glyphs `⧉` / `├─` / `╰─` (`section.go:478-485`) undocumented.
   `⧉` also does double duty: stack root in the tree slot, "N hidden" in `⧉+N`.
7. Every group lays out at `termW-4` (`:2864`); twelve PR-mode actions sprawl
   the full terminal width.
8. Glyph groups are not mode-aware. The comment at `:2797` promises issue mode
   drops PR-only rows; the glyph group at `:2801` is not filtered, and issue
   rows render an empty gutter (`section.go:640-641` passes `""` for
   ci/review/auto).
9. `TestLegendGlyphsAreUnambiguous` passes despite defect 1. Its predicate is
   `len(seen) <= 1`, which fires only when *all* duplicates render identically;
   the dim closed `✗` (`:2807`) inflates `seen` to 2 and the two red ones slip
   through. Verified by running it.

## The teaching device: a real example row

The legend must teach column position. Three devices were considered and two
rejected.

**Numbered columns** (`1 ci`, `2 review`, …) invent an index that appears
nowhere in the UI — there is no ruler above the board — and collide with
`1-6 jump tab` two groups down the same modal, where digits already mean
something.

**Reusing the column header glyphs** is impossible: there is one
`statusHeadGlyph` over the CI cell and the rest of the gutter stays blank
(`section.go:905`). There is nothing per-column to reuse.

**Spatially aligning legend entries to the row's offsets** cannot work either:
gutter slots are one cell wide at a two-cell pitch (`section.go:768`), and a
wrapped label run cannot sit on that pitch.

So: **render one real example row, full width, directly under the title**, then
list the column blocks beneath it in left-to-right row order. The reader maps
block to column by reading order against a live specimen.

The example is produced by `renderItemRow` itself, not hand-assembled — the
example then cannot drift from the row grammar, because it *is* the row grammar:

```go
renderItemRow(
    RowOpts{Width: inner, Focused: true, Tree: "├─", StackMissing: "⧉+2", …},
    accentStyle, "#123", "example row", "ENG-1", "you", "2h", diffstat(120, 8),
    ciGlyph("fail"), reviewDot("REVIEW_REQUIRED"), autoMergeGlyph(true),
)
```

`Focused: true` is load-bearing, not decorative: the bar cell is blank unless
the row is focused or selected (`section.go:751-757`), so without it `▎` is
never demonstrated. Selection cannot also be shown — `▌` wins the same cell —
so the ROW block says so in words.

`Landed` is deliberately **not** set on the example. It shares the title/tag
budget with `⧉+2` and crowding both proves nothing; it gets a ROW entry instead.

## Layout: two panes

Left is *what the row is telling you*, right is *what you can press*.

Glyphs in the sketch below are ASCII stand-ins. Use the existing constants —
`warnGlyph` (``) for the flag, **not** the `⚠` drawn here: `theme.go:366`
rejects U+26A0 precisely because many terminals draw it two cells wide while
lipgloss measures one, shifting the number column. `blockerGlyph` is U+26A0 and
is for the preview only.

```
┌─ Legend ────────────────────────────────────────────────────────────┐
│ ▎✗  ●  ↻  ⚠  ├─#123  example row  ⧉+2   ENG-1   you   +120 -8   2h  │
│                                                                      │
│ STATUS                          │ NAVIGATION                         │
│  ✓ passing   ✗ failed           │  ↑↓/jk move    →/l expand          │
│  ◔ running   · no checks        │  ⇥ PRs/Issues  space select        │
│  ◌ draft  󰘭 merged  ✗ closed    │  V cluster                         │
│                                 │                                    │
│ REVIEW                          │ FILTERS                            │
│  ⛊ approved  ● required         │  / filter (@user, is:, text)       │
│  ✗ changes requested            │  s state   R reviewers   D drafts   │
│  ◐ I commented  · no decision   │                                    │
│                                 │ VIEW                               │
│ AUTO                            │  p all comments   h/l switch tab   │
│  ↻ auto-merge armed             │  1-6 jump tab  z max  alt+j/k scroll│
│                                 │                                    │
│ FLAG                            │ ACTIONS                            │
│  ⚠ conflict → resolve locally   │  ↵ worktree  m merge   r rerun     │
│  ⚠ behind base → u update       │  u update    M ready   L approve   │
│                                 │  W bulk  y #  Y url  b branch      │
│ STACK                           │  o open      X cleanup branch      │
│  ⧉ stack root                   │                                    │
│  ├─ ╰─ stacked on the row above │  a actions      ctrl+r refresh     │
│  ⧉+N members hidden by filter   │  ?/F1 legend    q quit             │
│                                 │                                    │
│ ROW                             │                                    │
│  ▎ focus   ▌ selected (hides ▎) │                                    │
│  faint row = draft              │                                    │
│  "landed" = merged this session │                                    │
│  age = last update; merged and  │                                    │
│  closed rows age from landing   │                                    │
└──────────────────────────────────────────────────────────────────────┘
```

Pane assignment is **static**, not height-balanced. Balancing would migrate
groups between panes as content changes — PR vs issue mode, `ShowSide` toggling
the `h/l` and `1-6` hints (`prlist.go:2836-2839`) — and the one thing the split
buys is that the reader learns where to look once. A column whose contents move
is worse than a column with dead space under it.

### Flag entries carry the remedy

`⚠ conflict → resolve locally` and `⚠ behind base → u update`. Naming the state
was never the gap; the operator saw a red triangle and asked what was
*happening*. This is the one place the two panes cross-reference each other, and
it turns a warning into an instruction.

### Column names, not `ci`

The first block is `status`, not `ci`, because merged, closed and draft are
cell-1 **overrides** — they replace the CI glyph rather than sitting elsewhere
(`section.go:207-219`). Filing them under a general "row markers" heading, as
the current legend does, misstates where they appear.

## Fallbacks

**Filtering drops to a single column.** A filtered legend is one to five hints;
two panes of two hints is absurd, and every empty-pane and height-mismatch
branch would exist solely for this path. The modal already changes shape while
typing — the title becomes `Legend: m` (`:2902`), the height shrinks with the
match count, and `overlayTop` re-centres each render — so this costs nothing in
stability. The example row is dropped too.

**Narrow terminals drop to a single column, on a content-derived threshold.**
Not a constant: an 80-column terminal gives inner 72 → panes of 34/35, and a
fixed 76 would push every 80-column user to one column for no reason. Instead,
fall back when either pane's widest un-wrappable cell exceeds its `panelSplit`
width — available as `gridLayout(hints, paneW, true).cellW - hintGutter`. Today
the widest is `/ filter (@user, is:, text)` at 28, putting the real crossover
near inner 62. A content-derived rule cannot go stale when someone adds a longer
hint.

This is a quality gate, not a correctness one: below the threshold
`panelColumn`'s `Width(w).Render` soft-wraps rather than overflows.

**Width caps at `listInnerMax`** (110, `layout.go:118`) rather than a new
constant. The cap costs no density: `ACTIONS`' widest cell is `X cleanup branch`
at 16, so `cellW` is 19 and a third grid column needs a pane ≥ 57, i.e. inner
≥ 117. Nothing between the fallback threshold and 117 gains a column. Past ~110
the modal is a sparse band on a wide terminal, which is the same reason
`listInnerMax` exists.

**Issue mode drops to a single column.** Issue rows render `· ·` and blanks
(`section.go:640-641`), so `status`/`review`/`auto`/`flag`/`stack` describe
nothing that appears. That leaves ROW — focus, selected, the age footnote — as
the entire left pane: three hints against twenty on the right, which is not a
two-column layout worth drawing. Issue mode therefore takes the same
single-column path as filtering, with the example row rendered as an issue row.

## Code shape

Additive. `renderLegendGroups` keeps its signature, so `expandedLegendGroups`
(`expanded.go:491`) and `logLegendGroups` (`logview.go:409`) — both single
untitled groups of five to seven hints, which should never be two-pane — are
untouched.

```
legendView()
 ├─ legendQuery != ""   → renderLegendGroups(…)      (existing path)
 └─ else                → renderLegendPanes(…)       (new)

legendBlock(groups, w) []string     ← extracted from renderLegendGroups:2866-2875
 ├─ renderLegendGroups  = titledBox(legendBlock(all, inner))
 └─ renderLegendPanes   = titledBox(exampleRow + JoinHorizontal(
                            legendBlock(left, lw), sep, legendBlock(right, rw)))
```

Extract `legendBlock` **before** adding `renderLegendPanes`; without it the
header/blank-line/grid composition exists twice.

No new layout primitive. An earlier draft proposed a `gutterBlock` for the
`1 ci  ✓ pass  ✗ fail …` shape — a label gutter with a wrapped hint run. It is
unnecessary: `legendGroup{title, hints}` rendered through header-then-`gridHints`
already is that shape. Each gutter column simply becomes its own `legendGroup`,
costing one header line per column instead of an inline label, and left-pane
height is free.

The separator is `panelBody`'s (`prlist.go:3048-3053`). Carry its comment
across — each separator line must be padded individually, because wrapping a
multi-line rule in `" "+…+" "` pads only the first and last rows and jags both
the divider and the right border.

## Tests

**Tighten the ambiguity guard.** The invariant the row actually relies on is:
*within one column group, each rendered key maps to exactly one label; across
groups, duplicates are allowed because the column disambiguates.* That admits
red `✗` in both `status` and `review` while rejecting the collision inside a
single group. Note the current test only inspects `g.title == "glyphs"`
(`:2091-2095`) — once the glyphs split into per-column groups it would silently
inspect nothing, so it must iterate every non-key group.

**Completeness, styled.** Enumerate every producer — `ciGlyph` × {pass, fail,
pending, none}, `reviewDot` × 4 decisions, `pendStyle.Render(reviewCommentedGlyph)`,
`autoMergeGlyph(true)`, `flagGlyph("DIRTY","")`, `flagGlyph("","BEHIND")`,
`draftMark`/`mergedMark`/`closedMark`, `focusBarGlyph`/`selBarGlyph` in their
styles, the three tree strings, `"⧉+"`, `landedTag` — and assert each appears as
some hint's `renderKey()`. Comparing styled output makes colour part of the
contract, which is what defect 4 needs. This is the test that would have caught
the missing `approved`.

**Width sweep** at termW ∈ {60, 80, 100, 120, 160, 220}: total width ≤ termW,
every line equal width, no soft-wrap inside a pane, and the example row's width
equals the box inner width.

**Threshold**: two panes at 100, one at 50, and the crossover moves when a
40-wide hint is appended to a group — proving it is derived, not hardcoded.

**Mode**: issue mode renders single-column (no `│` separator) and carries no
`status`/`review`/`auto`/`flag`/`stack` group at any terminal width.

## Out of scope

`card.go:14-15` uses the same red `✗` for conflict, checks-failing and
changes-requested inside the preview card — defect 1 on a different surface.
Separate issue.

The author hue (`authorStyle`) is a stable per-person colour that a new reader
may try to decode as state. Real, but it is not a glyph and the left pane is
already six blocks. Not addressed here.
