# UX Polish (issue #3) — remaining-gaps design

**Date:** 2026-07-19
**Branch:** `feat/3-ux-polish-checks-rerun-label-chips-respo`
**Scope:** small, surgical — ~3 source files + tests, plus a manual audit pass.

## Problem statement

Issue #3 was a 6-item UX-polish pass on the prdash TUI. Most of it already
landed on `main` via later PRs (#5 / #29 / #30 / #33 / #34 / #35): the navigable
Checks tab with `r`/`R`/`o`/`Y`/`enter` (rerun/open/copy/logs), the real-color
label backend (`gh.Label{Name, Color}`, `labelChip`, `renderChips`), the
`h`/`left` no-wrap exit on tab 0, and the preview-perf work (triage prefill,
neighbor prefetch, disk-cached details). This spec covers only the **genuine
remaining gaps**, and explicitly does not reintroduce removed features (`f`/`F`
view keys, filter presets).

Two code deliverables remain, plus one verification step:

- **Gap A** — colored label chips render only in the expanded metadata line, not
  in the list rows.
- **Gap B** — the expanded detail view is a single centered reading column at
  every width; it does not use the horizontal space of a wide terminal.
- **Audit** — a manual TUI pass after A+B to catch alignment/contrast/truncation
  nits.

Requirement #3's second clause ("`enter` expands from the list") is **superseded
and out of scope** — see the note below.

---

## Gap A — label chips in list rows

### Current state

- Rows are rendered by `renderItemRow` (`internal/ui/section.go:327`), which
  composes `left + titleTxt + gap + right` and fills to exactly `o.Width` via a
  computed `gap` (`section.go:380-384`). This exact-fill invariant is locked by
  `TestRenderItemRowIsSingleLine` (`section_test.go`) and the width sweep in
  `layout_sweep_regression_test.go`.
- `PRSection.RenderRow` (`section.go:85`) calls it with an **empty** author
  (author is hoisted into the group header) and passes `status`/`reviewDot`
  into the `ci`/`review` columns; `right` is therefore just the age string.
- `IssueSection.RenderRow` (`section.go:282`) passes the author and leaves
  `ci`/`review` empty (they render as dim `·`).
- Chips already exist and are type-agnostic: `renderChips(labels []gh.Label,
  maxW int)` (`section.go:446`) packs rounded pills into `maxW` cells with a
  dim `+N` overflow and returns `""` when `maxW < 3`; `labelChip` (`theme.go:215`)
  colors each pill from the 6-hex GitHub color with luminance-based fg. Both
  `gh.PR.Labels` (`prs.go:80`) and `gh.Issue.Labels` (`issues.go:18`) are
  `[]gh.Label` (`prs.go:67`).

### Design decision

Pass the label slice into `renderItemRow` and let it carve a **bounded chip
budget** from the row's flexible middle, placing chips immediately to the left
of the right (age) block:

```
‹bar›‹mark› ‹ci› ‹rv› ‹!› ‹num› ‹title…›   ‹chips +N›   ‹author›  ‹age›
```

Chips are the **lowest-priority** content, so they are the first thing to
disappear when the row is tight — satisfying "placed last, elide first":

1. Add a `labels []gh.Label` parameter to `renderItemRow` (both call sites pass
   their PR/Issue labels; empty slice for none).
2. Reserve the chip budget only when the row is wide enough
   (`w >= chipRowMinWidth`, a new tuning const). The budget is
   `min(maxChipW, availableSlack)` where `maxChipW` caps chips so they never
   starve the title (e.g. `w/4`, capped at a small absolute like 24 cells), and
   `availableSlack` is what remains after `left`, the right block, a minimum
   title width, and the one-space separators. On narrow rows the budget is 0.
3. Truncate the title to the room left **after** reserving the chip budget, so
   the title still gets priority on tight rows (budget shrinks to 0 before the
   title is squeezed below its minimum).
4. `renderChips(labels, budget)` returns `""` when `budget < 3`, so no chips
   and no separator render — the row collapses cleanly to today's layout.
5. Recompute the final `gap` from measured `lipgloss.Width` of every segment
   (including the rendered chip string) so the exact-fill invariant holds. All
   width math uses `lipgloss.Width`, never `len`, because chips and CJK titles
   are multi-byte / wide-cell.

The chip budget/threshold constants are tuned against the width-sweep test
rather than guessed.

**Trade-off (documented, accepted):** giving the title priority means very long
titles on medium-width rows will show few or no chips. That is the correct
behavior for a dense 2-line-budget board — the title is the primary identifier;
chips are a secondary at-a-glance signal that the expanded rail (Gap B) always
shows in full.

### Issue rows — recommendation: **yes, add chips**

Issues should get chips too, for three reasons: (1) issue triage is
label-driven, so labels are arguably *more* load-bearing on an issue row than a
PR row; (2) issue rows have spare room — `ci`/`review` are empty; (3)
`renderChips`/`labelChip` already take `[]gh.Label` and `gh.Issue.Labels` is the
same type, so it is a one-line change at the `IssueSection.RenderRow` call site
with zero new code. Consistency across both section kinds is worth more than the
marginal saving of skipping it.

### Files touched

- `internal/ui/section.go` — `renderItemRow` signature + chip budget/placement;
  both `RenderRow` call sites (`PRSection` @85, `IssueSection` @282) pass
  `labels`.
- `internal/ui/section_test.go` — the four existing direct `renderItemRow`
  callers (≈ lines 30, 80, 101, 322) must be updated for the new `labels`
  parameter to keep compiling.

---

## Gap B — width-responsive two-column expanded rail

### Current state

- `expandedView` (`internal/ui/expanded.go:423`) always builds a single centered
  column: `head`, a one-line `expandedMeta` (`expanded.go:397`, which itself
  packs author/branch/chips/CI via `renderChips(pr.Labels, w/3)`), then a
  `tabbedBox` (`box.go:91`) around `m.vp.View()`, then the footer. The whole
  stack is capped to `expandedBoxWidth = min(m.width, discussionMaxWidth+6)`
  (`expanded.go:177`; `discussionMaxWidth = 104`, `expanded.go:45`) and
  centered with `indentLines` when the terminal is wider (`expanded.go:446-448`).
- The viewport width is derived once, from `expandedBoxWidth - 2`, in
  `setExpandedContent` (`expanded.go:184,192`); height from `expandedBoxHeight`
  (`expanded.go:165`) minus `expandedChromeRows` (`expanded.go:155`: header +
  footer, +1 meta line for a PR). `expandedBody(w)` (`expanded.go:124`) renders
  the active tab at that width, and the prose tabs re-center via
  `renderDiscussionColumn` (`expanded.go:49`).
- `reflowExpanded` (`expanded.go:212`) re-runs `setExpandedContent` on resize,
  preserving scroll offset — so any geometry change must flow through the same
  helper to survive resize.

The single-column layout wastes the left/right thirds of a wide terminal, where
a metadata rail could live without stealing reading width.

### Design decision

Introduce a pure geometry helper and branch the layout on width. **Naming:**
mirror the existing file convention — a `Layout` struct produced by a
`computeLayout` func (`layout.go:15,29`). So the struct is `ExpandedLayout` and
the constructor is `computeExpandedLayout(w, h int, isPR bool) ExpandedLayout`
(a type-and-func with the *same* identifier would not compile — Go shares the
package-level namespace for types and funcs).

**Why the `isPR` parameter — the frame is section-dependent, so `(w,h)` alone
is insufficient:**

- **The rail content is entirely PR-specific** — `branch→base`,
  `PRDetail.ReviewRequests`, `ciSummary`, `PRDetail.Diffstat()`, and today's
  `expandedMeta` is rendered *only* for `PRSection` (`expanded.go:440`). Issues
  reach the expanded view too (`enterExpanded` is section-agnostic,
  `expanded.go:106`). So **two-col is PR-only**: `TwoCol = isPR && w >=
  expandedTwoColMin`. An Issue at any width stays a centered single column,
  exactly as today — no dead rail, no lost centering.
- **The chrome/meta row count differs by section**, matching the existing
  `expandedChromeRows()` (`expanded.go:155-161`): head + footer are always 2
  rows; a PR additionally carries a one-line meta row *in narrow mode only* (in
  two-col that meta content moves into the rail). So:
  `metaRows = 1 if (isPR && !TwoCol) else 0`; `chromeRows = 2 + metaRows`.
  `expandedChromeRows()` is updated to return `2 + metaRows` (i.e. it becomes
  two-col-aware) and the helper uses the same rule, so there remains a single
  height authority — no narrow-PR off-by-one overflow, no wasted row for narrow
  Issues or two-col frames.

```go
// ExpandedLayout is the computed geometry for the expanded detail frame.
type ExpandedLayout struct {
    TwoCol   bool // true only for a PR wide enough for a side rail
    RailW    int  // outer width of the left metadata rail (0 when !TwoCol)
    RailH    int  // rail body height (== content body height)
    ContentW int  // outer width of the right (tabbed) content pane
    VPHeight int  // viewport body height (content body minus tab strip + border rows)
}

func computeExpandedLayout(w, h int, isPR bool) ExpandedLayout
```

Height: `body = h - chromeRows` (chromeRows per the section-aware rule above);
`RailH = body` (two-col); `VPHeight = body - 2` (the current `tabbedBox`
deduction: the top tab/border line + the bottom border row — matching today's
`-2`; the no-overflow resize test is the check this row-count holds in every
mode). `VPHeight`/`RailH` are the only vertical outputs; the caller does not
re-derive height elsewhere. **Carry today's floors into the helper** so a
pathologically small terminal never hands `vp.SetHeight`/`SetWidth` a negative:
clamp `VPHeight`/`RailH` to `>= 1` and `ContentW`/`RailW` to `>= 1` (the current
`expandedBoxHeight` min-3 / `setExpandedContent` min-1 behavior).

**Wide/narrow cutoff — do NOT reuse `sideThreshold = 120`.** The reading column
is hard-capped at `discussionMaxWidth + 6 = 110` (`expanded.go:45,177`). If
two-col engaged at 120 the rail would get only `120 - gap - contentW` cells —
either a uselessly thin rail or a content pane squeezed below today's 110,
*regressing* the reading width exactly as two-col turns on. Instead define the
cutoff so the content pane keeps its full 110 alongside a usable rail:

```
const (
    expandedRailMin = 32 // rail never narrower than this in two-col
    expandedRailMax = 44 // …nor wider (a metadata rail past ~44 is wasted)
    expandedColGap  = 2  // cells between rail and content
    expandedContentCap = discussionMaxWidth + 6 // 110, unchanged from today
    // two-col only when a full rail AND a full-width content pane both fit:
    expandedTwoColMin = expandedContentCap + expandedRailMin + expandedColGap // 144
)
```

**Split algorithm (fully determined, no free parameters):**

- **Wide (`isPR && w >= expandedTwoColMin`) → `TwoCol`:**
  - `ContentW = expandedContentCap` (110) — **identical to today's centered
    reading column, so the reading width never regresses.**
  - `RailW = clamp(w - expandedColGap - ContentW, expandedRailMin, expandedRailMax)`.
    At `w = 144`, `RailW = 32` (exactly the min); it grows to `44` by `w = 156`
    and stays there.
  - Any leftover (`w - RailW - expandedColGap - ContentW`, non-negative and only
    positive once `RailW` saturates at 44) becomes an outer centering margin —
    the whole `head / [rail│content] / footer` block is `indentLines`-centered
    just as the single column is today (`expanded.go:446-448`).
  - The rail carries the metadata currently crammed into `expandedMeta` — PR
    `#num` + title, author, `branch→base`, label chips (full `renderChips` at
    `RailW`-derived inner width, not `w/3`), requested reviewers
    (`PRDetail.ReviewRequests`, `prview.go:47`), and a diffstat / CI one-liner
    (`ciSummary`, `expanded.go:375`; `PRDetail.Diffstat()`, `prview.go:52`).
- **Narrow (`!isPR`, or `w < expandedTwoColMin`) → single column:** keep today's
  layout — for a PR, the existing **one-line** `expandedMeta` above the tab
  strip (it returns a single joined line, `expanded.go:409`; this stays one row,
  so `metaRows = 1` and the chrome count is unchanged from today); for an Issue,
  no meta row. Viewport at `ContentW = min(w, expandedContentCap)`. This is the
  current behavior, refactored to read its width from `computeExpandedLayout`
  instead of the standalone `expandedBoxWidth`. `TwoCol=false`, `RailW=0`.

**Frame composition (pins head/footer scope):** `head` (repo + title) and the
`footer` hint line span the **full block width above and below** the columns —
they are *not* beside the rail. In two-col the head truncates against the **full
two-col block width** (`RailW + gap + ContentW`), not against `ContentW`, so a
wide title is not clipped to 110. The `JoinHorizontal(rail, gap, contentBox)`
sits between head and footer. Height budget uses the section-aware `chromeRows`
rule from the Design-decision block above — one height source of truth.

**Critical invariant:** the viewport width is set from `ExpandedLayout.ContentW`
in **both** modes (via `setExpandedContent`, which stays the single funnel for
geometry), so markdown/timeline/diffstat render at the correct width and
`reflowExpanded` keeps working on resize. Nothing bypasses the helper.

The rail and content pane are joined with `lipgloss.JoinHorizontal`; every
segment is measured with `lipgloss.Width`, and the rail is height-clamped to
`RailH` and width-clamped to its inner width so a long label set or reviewer
list cannot push the frame past `h` or bleed past `RailW`.

### Files touched

- `internal/ui/layout.go` — add `ExpandedLayout` struct +
  `computeExpandedLayout(w, h int, isPR bool)` and the tuning consts (pure
  geometry; no rendering).
- `internal/ui/expanded.go` — `expandedView` branches on `TwoCol`; add a rail
  renderer; `setExpandedContent` reads `ContentW`/`VPHeight` from the helper;
  `expandedBoxWidth` is retired as the sole width source (its `discussionMaxWidth+6`
  cap moves into `expandedContentCap`).

---

## Requirement #3 second clause — superseded / out of scope

Issue #3's acceptance text says "`enter` expands from the list." **This is
dropped as a code deliverable.** #30/#35 deliberately repurposed list-focused
`enter` into a context-aware action that opens the PR's worktree
(`m.actions["enter"]` → "Open worktree", `internal/action/defaults.go:5,41`;
list `enter` falls through to the action dispatcher at `prlist.go:1460-1470`).
That newer design supersedes #3's criterion. Expanding from the list stays on
`l`/`right` (`prlist.go:1453-1459` → `enterExpanded`), which is already
implemented, and the expanded view keeps its own context-aware `enter` (Checks
tab → logs, else worktree; `expanded.go:300-307`) unchanged.

The other half of requirement #3 — `h`/`left` exits expand on tab 0 without
wrapping — is already done (`expanded.go:236-241`). **Requirement #3 therefore
needs no code changes.**

---

## Edge cases

- **No labels:** `renderChips` returns `""` (`section.go:447`); rows and rail
  render exactly as today, no stray separators.
- **Very long / many labels:** `renderChips` packs to the budget and appends
  dim `+N`; in rows the budget is capped so labels never starve the title, in
  the rail the chip block wraps within `RailW` and the rail is height-clamped.
- **Narrow terminals (`w < expandedTwoColMin`):** row chip budget goes to 0
  (chips vanish first); expanded view stays single-column with a compact header.
- **Issue detail at wide width:** stays a centered single column (two-col is
  PR-gated), identical to today — no empty rail, no off-center content.
- **Resize:** all expanded geometry flows through `computeExpandedLayout` →
  `setExpandedContent` → `reflowExpanded`, which preserves and re-clamps the
  scroll offset (`expanded.go:212-221`); a wide→narrow (or narrow→wide) resize
  swaps layout modes without bleed, and a narrow-PR frame is never a row too
  tall (section-aware `chromeRows`).
- **External / job-less checks:** unchanged — this change set does not touch the
  Checks tab or `renderChecks`.
- **Wide-cell (CJK) titles and chip strings:** all width accounting uses
  `lipgloss.Width`, matching the sweep test's worst-case row.
- **Pathologically short/narrow terminal:** the helper clamps `VPHeight`/`RailH`
  and `ContentW`/`RailW` to `>= 1` (carrying today's floors), so `vp.SetHeight`/
  `SetWidth` never receive a negative on tiny terminals.

## Testing plan

Pure functions get unit tests, mirroring existing `*_test.go` patterns:

- **Chip packing in rows** (`section_test.go`): the width-sweep fixture
  `sweepPRs()` (`layout_sweep_regression_test.go:19`) sets **no** `Labels`, so
  reusing it as-is exercises none of the chip path — add a **labeled fixture**
  (a PR with several labels, incl. one with an empty/invalid color to hit the
  fallback chip, and enough labels to force `+N`). Assert (a) the row is still
  one line and fills exactly `o.Width` across the width sweep (reuse the
  ANSI-decode + `lipgloss.Width` check so a chip-budget off-by-one is caught),
  (b) chips appear on a wide row, (c) chips are absent (no separator, title
  intact) on a narrow row, (d) a `+N` overflow renders when labels exceed the
  budget. **Critically include `focused=true` WITH chips:** the focused row runs
  through `rowBgWrap` (`section.go:396`), which re-injects the row background
  after every SGR reset, while each chip carries its own `Background` from
  `labelChip` — a width-only assertion won't catch a per-chip-bg vs row-bg
  refill bug, so assert the focused+labeled row is still exact-fill and single
  line.
- **`computeExpandedLayout` width + section selection** (`layout_test.go`):
  table-driven across the `expandedTwoColMin = 144` boundary, **with `isPR`**:
  for `isPR=true`, `TwoCol` is false at 143 and true at 144; at `w = 144` assert
  the **exact** split `ContentW == 110`, `RailW == 32`,
  `RailW + expandedColGap + ContentW <= w`; at a large width (e.g. 200) assert
  `RailW == expandedRailMax (44)`, `ContentW == 110`, leftover is centering
  margin; when narrow assert `ContentW == min(w, 110)`, `RailW == 0`. **For
  `isPR=false` (issue): assert `TwoCol == false` even at `w >= 144`**, so a wide
  issue view never gets a dead rail. Also assert the section-aware height:
  `VPHeight` for a narrow PR is one row less than for a narrow issue at the same
  `(w,h)` (the meta row), and a two-col PR does not lose that row to a phantom
  narrow-meta line. Exact-value assertions (not just inequalities).
- **Rail renders within bounds** (`expanded_test.go`): render the two-col
  expanded view at a wide size and assert no line exceeds `m.width` and the rail
  column inner width is `<= RailW` (ANSI-decoded width check), plus a resize
  wide→narrow→wide leaves no overflow and preserves scroll — charm-TUI
  layout-regression coverage in the spirit of `layout_sweep_regression_test.go`.

No test is added for requirement #3 (no code change).

## Verification / UX audit (not a code deliverable)

After A+B land and the gate is green (`go build ./... && go vet ./... &&
go test ./...`), build the binary and drive the running TUI to eyeball:
chip alignment and contrast in both list rows and the rail (light + dark
themes), title truncation with chips present, two-column framing at wide widths
and the single-column fallback at narrow widths, a live resize across the
`sideThreshold` boundary, footer hint accuracy, and empty states (no labels, no
reviewers, no checks). Fix any nits found; these are polish edits, not new
behavior.
