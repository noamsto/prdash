# prdash — cursor-near prefetch, group-scoped `V`, optimistic auto-merge/approve

**Date:** 2026-08-03
**Status:** approved
**Delivery:** three small PRs (below). Distance-tiered refresh intervals are
explicitly out of scope — a follow-up.

## Problem

Three independent board papercuts:

1. **Prefetch only walks downward.** `prefetchNumbers` in
   `internal/ui/preview.go` starts at the cursor and walks `i++`, so rows
   *above* the cursor stay cold even when they are closer than rows below.
   The cursor row is already fetched alone first; the rest of the window is
   one batch — order inside that batch is what this changes.

2. **`V` selects the whole board.** `case "V"` in `prlist.go` toggles every
   shown index. On a grouped board (author headers, or Mine / Review
   requested / Others) the user usually wants the **current group**, then
   optionally the whole board, then clear — not an immediate board-wide
   select.

3. **Auto-merge and Approve lag visually.** `A` (auto-merge) has no
   `Refresh: true`, so the armed sync glyph can wait until some unrelated
   refetch. `L` (approve) does refresh, but the review glyph still waits on
   the network round-trip. Both should paint as soon as the mutation
   succeeds — same spirit as `mergedSticky` and `ciRerun`.

---

## Feature 1 — Bidirectional distance-ordered prefetch

### Behavior

`prefetchNumbers(ps, cursor, fresh, window)` returns up to `window` PR numbers
whose detail is not session-fresh, ordered by distance from the cursor:

1. Prefer smaller `|i - cursor|`.
2. On a tie (same distance above and below), prefer the row **below** the
   cursor (preserves today's downward bias for the first neighbor).
3. Skip `fresh[num]`; stop at `window` numbers.

`warmDetailCmd` is unchanged: cursor row still goes out alone via
`detailCmdForCursor`; the remaining window still goes out as one
`batchDetailCmd`. Only the membership/order of that window changes.

`prefetchWindow` stays `5`. No new concurrency, no staggered ticks.

### Out of scope (follow-up)

Tiered revalidation intervals / TTLs by distance ("closer = faster"). The
existing hot/cold CI poll and `detailFreshTTL` (own vs others) stay as they
are.

### Tests

- Cursor in the middle of a 9-row board, window 5, none fresh → numbers at
  distances 0,1,1,2,2 (cursor, below, above, below+2, above+2) in that order.
- Fresh rows are skipped and do not consume the window budget wrongly —
  still fill up to `window` from farther neighbors.
- Cursor on the last row → only upward neighbors (plus cursor).

---

## Feature 2 — `V` cycles Group → All → None

### Behavior

No stored mode. Each `V` derives the next step from the current selection:

1. **Group** — if any row in the cursor's group is unselected, select every
   row in that group (additive; other groups untouched).
2. **All** — else if any shown row is unselected, select every shown row.
3. **None** — else clear the selection.

"Cursor's group" = contiguous run of shown indexes sharing
`section.groupLabel(i)` with the cursor when `grouped` is true.

### Flat boards

When `grouped == false` (single author, fuzzy-flattened find, etc.), the
whole shown set is one group. Step 1 therefore selects everything, and the
next `V` sees a fully-selected board → **None**. The All step is skipped
naturally; no special case required.

### Issue board

Same logic against whatever grouping the issue section exposes; if it is
always flat, behavior collapses to All → None like a flat PR board.

### Legend / help

Footer hint `V` `all` becomes `V` `group` (short enough for the hint strip;
the cycle is discoverable by pressing it).

### Tests

- Grouped board, nothing selected, cursor in group B → `V` selects only B.
- Group B fully selected, other groups not → `V` selects all.
- Everything selected → `V` clears.
- Partial group (one of three selected) → `V` fills the group (does not
  clear the partial and does not jump to All).
- Flat board → first `V` selects all, second clears.
- `space` mid-cycle does not break the derivation (no stuck state machine).

---

## Feature 3 — Optimistic auto-merge + approve glyphs

### Behavior

On `actionDoneMsg` with `err == nil`, before (or alongside) any refresh:

**Auto-merge (`auto-merge-squash`)** — for each number in
`actionStatus.nums`, set the in-memory PR's `AutoMergeRequest` to
`{MergeMethod: "SQUASH"}` (the only method this action arms). Bump `rowGen`
and repaint so `autoMergeGlyph(true)` shows immediately. Also set
`Refresh: true` on the default `A` action (or always refresh from the
handler) so the list/search eventually reconciles; the optimistic field
wins until then.

**Approve (`approve`)** — for each successful number, set
`ReviewDecision = "APPROVED"` on the in-memory PR (and, if `m.detail[n]`
exists, upsert the viewer's entry in `LatestReviews` to `APPROVED` so the
side card / commented-by-me logic stays consistent). Repaint immediately.
`L` already has `Refresh: true`; keep it.

Paint **only on success** ("if request went") — not on keypress. Failure
leaves the row unchanged; no revert path needed.

### Bulk

A bulk selection may partially fail today depending on how `runBulkNative`
aggregates errors. Optimistic patches apply only to numbers the done-handler
treats as succeeded. If today's handler is all-or-nothing (`err != nil`
skips the whole refresh), keep that: patch the full `actionStatus.nums` set
iff `err == nil`, else patch nothing. Do not invent per-PR success lists in
this PR unless the bulk path already exposes them.

### Precedence with refresh

When `backgroundRefresh` lands, the live list payload overwrites the
optimistic fields. That is correct — the server is source of truth. Do not
build a sticky map unless refresh is observed to clobber a still-true
auto-merge (unlikely; GitHub returns `autoMergeRequest` on the list query).

### Tests

- Successful auto-merge action → row contains `autoMergeGlyph(true)` on the
  next paint **without** waiting for a list fetch message.
- Failed auto-merge → glyph stays absent.
- Successful approve → review column shows the approved glyph immediately;
  failed approve does not.
- Bulk success patches every `actionStatus.nums` entry.

---

## Delivery

| PR | Scope |
|---|---|
| 1 | Feature 1 — `prefetchNumbers` distance order + tests |
| 2 | Feature 2 — `V` cycle + legend + tests |
| 3 | Feature 3 — optimistic patches on `actionDoneMsg` + `A` refresh + tests |

No shared types across the three; they can land in any order. Recommended
order is the table order (prefetch is pure + tiny; selection is UX-facing;
optimistic mutations touch the action done path).

## Non-goals

- Distance-tiered poll / detail TTL ("closer = faster").
- Disable / toggle-off auto-merge (still enable-only via `A`).
- Optimistic paint on keypress before the network returns.
- Changing `prefetchWindow` or CI poll hot/cold thresholds.
