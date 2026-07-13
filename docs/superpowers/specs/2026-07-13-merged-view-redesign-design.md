# Merged/Closed View Redesign — Design

**Date:** 2026-07-13
**Status:** Approved (design), pending implementation plan

## Problem

The PR list treats every state identically, which makes the terminal (merged / closed) states confusing:

1. **Left-column glyphs are historical noise for terminal PRs.** For a merged PR, cell 1 (CI rollup) and the actionability sort key describe the PR's state *before* it landed. A merged PR showing `✗` (CI failed) or `●` (running) is meaningless — it merged. The intended mauve merged glyph (`mergedMark`, `section.go:91`) is being obscured by this.
2. **No time-based ordering for merged/closed.** Sorting is `prRank` (actionability) then `UpdatedAt` desc (`sortPRs`, `section.go:146`). For merged PRs the rank is meaningless yet still buckets rows by frozen CI/review state, so they read as scrambled rather than chronological. `gh.PR` has no `MergedAt`/`ClosedAt` field, so we currently *cannot* sort by the event that matters.
3. **Redundant, misplaced status.** The current-view descriptor (`mine · merged · N`) sits on the global top line (`header()`, `prlist.go:1268-1271`) while the list-box title carries a second count (`PRs · N`, `listTitle()`, `prlist.go:1311`). The view state should live on the pane it describes; the top line should be reserved for global/persistent state.

## Goals

- Top line = global/persistent state only. List title = the current view.
- Terminal states (merged, closed) show a state-appropriate glyph, not pre-merge CI noise.
- Merged/closed rows sort newest-event-first (by merge / close time).
- No changes to Issues rows, keybindings, or the open-state behavior beyond the header split.

## Design

### 1. Header split (`internal/ui/prlist.go`)

- **`header()`** drops its third segment (the `label · state · count` block at `prlist.go:1268-1271`). The top line keeps: repo name, `modeSegments` board tabs (`PRs │ Issues`), refresh spinner, transient `statusBadge`, and the `N selected` counter. This is "where am I" state that persists across state/preset toggles.
- **`listTitle()`** (`prlist.go:1311`) becomes the view descriptor: `<state-glyph> <preset-label> · <state> · <count>`.
  - `state-glyph` is the same glyph used in the rows' cell 1 for that state (see §2): `󰥭`-style open glyph for open, `󰘭` merged, dim `✗` for closed. (Exact open glyph chosen during implementation to match existing `prGlyph`/`issueGlyph` conventions at `prlist.go:1229`.)
  - `preset-label` = preset name (`mine`/`all`) or the raw custom author body, reusing the logic currently in `header()`.
  - `count` = shown count (`m.section.Len()`), matching today's `listTitle` single-number behavior.
  - Examples: `󰘭 mine · merged · 20`, open `mine · open · 20`, `✗ mine · closed · 20`.
- The `shown/total` tally currently in `header()` via `count()` (`prlist.go:1287`) is dropped from the top line; the title's single shown count replaces it. (If the `/` live-filter case needs a shown/total hint, it can be added to the title later — out of scope here.)

### 2. State-aware row glyphs (`internal/ui/section.go`)

Cell layout is unchanged (`bar mark ci review flag`, `section.go:322`). Only the values differ by `PR.State`:

| State | Cell 1 (was CI) | Cell 2 (review) | Cell 3 (flag) |
|-------|-----------------|-----------------|---------------|
| **Open** (unchanged) | `ciGlyph(CIState())` | `reviewDot(ReviewDecision)` | `flagGlyph(...)` |
| **Merged** | mauve `󰘭` (`mergedGlyph`/`mergedStyle`) | `reviewDot` kept (`✓` approved / `·` none) | blank |
| **Closed** (not merged) | dim/grey `✗` (new `closedGlyph`, `dimStyle`) | `reviewDot` kept | blank |

- The existing `mergedMark()` override (`section.go:91-93`) generalizes into a state switch in `PRSection.RenderRow` (`section.go:90`) that picks cell 1 by `PR.State` and blanks cell 3 for terminal states.
- Closed glyph: a new `closedGlyph` const + reuse of `dimStyle` (`theme.go`) so it reads as "dead" rather than an active red failure. Distinct from the CI-fail `✗` (`failStyle` red) by color.
- The `?` legend (`prlist.go:1345-1350`) gains lines for the merged and closed glyphs.

### 3. State-aware, time-based sort (`internal/gh/prs.go` + `internal/ui/section.go`)

- **New fields** on `PR` (`gh/prs.go:53`): `MergedAt time.Time` and `ClosedAt time.Time`, added to `prFields` (`gh/prs.go:10`) as `mergedAt,closedAt` (valid `gh pr list --json` fields).
- **`sortPRs` becomes state-aware** (`section.go:146`). The section already knows the active state (threaded from `Model.state`; exact plumbing decided in the plan — either pass state into `SetPRs`/`SetCategorized` or store it on the section):
  - **Open:** unchanged — `prRank` then `UpdatedAt` desc.
  - **Merged:** `MergedAt` desc (newest merge on top). No `prRank`.
  - **Closed:** `ClosedAt` desc. No `prRank`.
- Grouping (`Mine` / `Review requested` via `SetCategorized`, or by author) is preserved in all states; the new sort orders rows *within* each group.

### 4. Time column (`internal/ui/section.go` `renderItemRow`)

The right-hand relative time reflects the event the view is about:
- **Open:** time-since-`UpdatedAt` (as today).
- **Merged:** time-since-`MergedAt`.
- **Closed:** time-since-`ClosedAt`.

## Out of scope

- Issue rows (open/closed only; no CI/review cells — untouched).
- Keybindings, filters, draft handling, author/reviewer pickers.
- Any `/` live-filter shown/total display in the new title.
- Fetch limit changes.

## Affected files

- `internal/gh/prs.go` — `PR` struct fields + `prFields`.
- `internal/ui/section.go` — `RenderRow` state switch, `sortPRs` state-awareness, time column.
- `internal/ui/prlist.go` — `header()` trim, `listTitle()` rebuild, legend, state plumbing into the section.
- `internal/ui/theme.go` — `closedGlyph` const (+ style reuse).
