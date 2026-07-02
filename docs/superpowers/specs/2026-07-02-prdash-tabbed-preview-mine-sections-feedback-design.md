# prdash — tabbed preview, mine sections, action feedback

Design for consolidating the zoom/expand overlap, sectioning the mine view, and
giving inline actions visible progress. Built on branch `feat/8-perf-and-actions`
as three independent commits, implemented D → C → A/B.

## Problem

- **Zoom (`z`) and expand (`→`) overlap.** Both give one PR more room: zoom
  widens the Summary preview, expand is a full-screen tabbed reader. Confusing,
  and zoom has no exit hint since its panel was removed.
- **Expand's footer is the old plain style** — inconsistent with the bordered panel.
- **Inline actions give no feedback.** `r` (rerun failed) fires a `gh` subprocess
  with no in-flight or completion indication.
- **The mine view is one flat list.** Authored and review-requested are separate
  `f` presets; you can't see everything needing you at once.

## D · Action feedback

Inline actions (builtins + argv `gh` commands: rerun, merge, update, ready,
assign-reviewers) report progress on a transient line under the header:

- On dispatch: `⟳ <label>…` (reuses the existing refresh spinner tick).
- On completion: `✓ <label>` or `✗ <label>: <err>`, cleared after ~3s.

New `actionDoneMsg{label string, err error}` returned by the inline action cmds
(replacing their current `nil`/`fetchFailedMsg` returns for these paths).
`actionStatus{label, err, done bool}` on the model drives the line; a `tea.Tick`
clears it. Copy/exits-TUI actions are unaffected (instant / quit).

## C · Mine view = two sections

The `mine` preset fetches **both** `is:open author:@me` and
`is:open review-requested:@me`, dedupes by PR number, and tags each row with a
category (`mine` / `review`). `PRSection` grows a category-grouping mode
(alongside author-grouping) that renders `Mine` and `Review requested` headers.

Presets collapse to **`mine`** (the combined, sectioned view) and **`all`**; `f`
toggles between them. `warmFilters`/prewarm and the disk cache key on the two
underlying searches; the combined view is assembled from both.

## A/B · Tabbed preview + narrow expand

**Wide (side preview visible):** the preview gains a tab strip
`Summary · Conversation · Reviews · Checks · Diff`.

- `Summary` = current `previewPane` (identity, triage card, CI, reviewers,
  folded latest-2 timeline). The other four = current `expandedBody` renderers,
  moved to render into the preview width.
- Tabs cycle with `tab`/`shift+tab` and `→`/`l`·`←`/`h`; `1`–`5` jump;
  `ctrl+j/k` scrolls the active tab (existing `previewOffset`).
- The old `tab` = fold-timeline toggle is dropped.
- `z` widens the tabbed preview to full width; the tab strip and a `z exit` hint
  stay visible. No separate expand mode on wide.

**Narrow (<120 cols, no side pane):** `→`/`l` still opens the full-screen tabbed
reader, restyled to the bordered/panel footer style. `esc`/`h`-on-first-tab exits.

Shared tab-body renderers (Conversation/Reviews/Checks/Diff) are parameterized by
width so both the side preview and the narrow full-screen reader use them.

## Testing

- D: inline action returns `actionDoneMsg`; status line shows running then
  done/err; clears on tick.
- C: mine preset yields two categories; dedup; section headers render; `f`
  toggles mine↔all; both underlying searches prewarm/cache.
- A/B: preview tab cycling + jump; each tab body renders at preview width;
  zoom keeps the strip + exit hint; narrow still enters full-screen expand.

## Out of scope

Cross-repo lazytmux changes (separate PR #123); the merge of PR #9 itself.
