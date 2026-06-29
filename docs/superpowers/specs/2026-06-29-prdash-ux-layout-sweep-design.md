# prdash UX layout sweep — dense board + focus-driven triage card (design)

**Date:** 2026-06-29
**Status:** Draft (brainstormed with visual mockups)
**Author:** Noam
**Issue:** noamsto/prdash#5
**Builds on:** `2026-06-23-prdash-tui-redesign-design.md` (the three-surface model, the triage ladder, the lazy per-PR detail fetch).

## Purpose

The shipped TUI reads like a reading list, not a status board. Each PR takes
three screen-lines (title + dim meta + blank), so a tall terminal shows ~12 PRs,
and the detail you act on (why it's blocked, which check failed, the latest
comment) is spread across the row, the side card, and the expanded view.

This sweep reshapes the **list into a high-level, actionable status board** and
makes the **side card the single place for "why + what to do."** It optimizes one
loop the author runs all day:

> glance at status → copy it / jump into its worktree / rerun failed checks /
> see if it conflicts or what the latest comments are.

Non-goal: a diff viewer or a web client in the terminal. prdash stays
worktree-first; deep work happens in the worktree/editor. This is a layout +
information-design change, not new product surface.

## The board (list)

### Row anatomy

One PR per screen-line. Fixed columns, scanned vertically. Left → right:

| Col | Width | Content | Color role |
|-----|-------|---------|------------|
| bar | 1 | `▎` on the cursor row, else space | focus (sky) |
| mark | 1 | `●` when multi-selected, else space | select (mauve) |
| CI | 1 | `✓` pass · `✗` fail · `●` running · `·` none | pass / fail / pending / dim |
| RV | 1 | `✓` approved · `✗` changes requested · `●` review pending · `·` none | pass / fail / pending / dim |
| ! | 1 | `⚠` conflict (red) · `⚠` behind base (yellow) · space otherwise | fail / pending |
| # | 4–5, right | PR/issue number | accent (blue) |
| title | flex, ellipsis | PR/issue title | text; dim if draft |
| who | content | author login | per-author stable color (`authorStyle`); bots dim |
| age | 3–4, right | relative age (`2d`, `5h`) | dim |

The cursor row additionally gets a subtle background highlight (surface0). A row
can be both focused and selected (`▎●`).

Density: ~1 line/PR vs today's 3 — roughly **3× more PRs visible**.

### Reliable vs detail-derived columns

This is the core correctness rule (carried from the base spec's §2):

- **Reliable from the bulk list fetch** — CI rollup glyph, `reviewDecision`,
  `isDraft`. These render for every row, always.
- **Detail-derived** — the `!` column reads `mergeStateStatus`/`mergeable`, which
  come back `UNKNOWN` in a bulk `gh pr list`. The board therefore **never derives
  `!` from the list fetch.** It is filled only from the per-PR detail cache:
  - a row whose detail has been fetched (because it was focused, or a background
    prefetch reached it) shows its real `⚠`/blank;
  - a row with no cached detail shows a blank `!` — **never a guessed blocker.**
- **Background prefetch** — on load and on scroll, prefetch detail for the
  visible rows with bounded concurrency (2–3 in flight) so the `!` column fills
  in within a second or two without spamming `gh`. Cached detail also warms the
  side card (see Snappiness).

### Sections, sorting, empty state

- Keep the existing `PRSection` / `IssueSection` grouping; render compact dim
  headers (`PRs · 12`, `Issues · 3`) with a thin rule, not boxes.
- Issue rows reuse the same grid; CI/RV/`!` columns render `·`/blank (issues
  have no checks/review/merge state). Issue number + title + author + age.
- Sorting is unchanged from current behavior.
- The existing empty-state and loading behavior is preserved.

## The side card

Shown when the terminal is wide (`sideThreshold = 120`, unchanged) and follows
the cursor. The list/card split stays at the current **45 / 55** (`computeLayout`
already computes `side = 55%`). On narrower terminals the card is hidden and the
user presses `l`/`→` to expand the focused PR.

Built from the **per-focused-PR detail fetch** (reliable `mergeStateStatus` etc.),
not the list. Content, top to bottom:

1. **Identity** — `#309  Add retry logic to the uploader` · author · branch ·
   age. The branch is shown because it anchors copy/worktree actions.
2. **Blocker first** — the single highest-priority merge blocker from the triage
   ladder, plus the key that fixes it, in a left-accented callout:
   `⚠ Behind base · 2 checks failing` / `u update-branch  r rerun failed`.
   This is the card's headline — not a wall of fields.
3. **Checks, broken out** — failing checks **by name** (`✗ test (ubuntu)`,
   `✗ lint`), then passing/running. Names tell you rerun-the-flake vs
   jump-into-the-worktree; the board's `✗` only said "something failed."
4. **Review** — decision + who is requested (`● pending — requested alice, carol`).
5. **Latest** — newest 2 timeline comments inline (`@author · age` + body),
   older folded behind `▸ N earlier`. The full thread lives in Expanded. Reuses
   the existing timeline-folding logic.

`UNKNOWN` merge state degrades to a neutral "merge state pending…" headline and a
follow-up refetch, never a wrong blocker (per the base spec).

## Context-aware action bar

The bottom bar surfaces only the keys that apply to the focused PR, so the
recommended fix is always one visible keystroke and the bar isn't cluttered with
inapplicable verbs. Driven by the same triage state as the card.

**Always present (core verbs):** `↵` worktree · `y` copy url · `o` open ·
`a` actions (full menu) · `space` select · `/` filter · `l` expand · `q` quit.

**State-specific (shown only when applicable):**

| Focused-PR state | Extra key shown |
|------------------|-----------------|
| Draft | `ready` mark-ready |
| Conflict (`DIRTY`/`CONFLICTING`) | — (resolve in worktree) |
| Checks failing | `r` rerun failed |
| Behind base | `u` update-branch |
| Changes requested | — (address in worktree) |
| Clean + approved + passing (Ready) | `m` merge |

The `a` actions overlay still lists the **complete** set regardless of state, so
nothing becomes undiscoverable — the bar is an accelerator, the overlay is the
index.

### `y` rebinding

Today `y` is "Copy branch". Change `y` → **Copy URL** (the share action), and add
`Y` → **Copy branch** to preserve the branch-copy. Worktree-jump (`↵`) already
covers the checkout case, so the URL is the more distinct value for `y`.

## Snappiness

Fast `j/k` must never block on the network or spam `gh`:

- The list renders instantly from the cached `gh pr list` result.
- On focus change, the card renders **immediately from cache** if the focused
  PR's detail is cached; otherwise it shows a skeleton (identity + CI rollup are
  already known) and fires a detail fetch.
- The detail fetch is **debounced** (~150 ms after focus settles), so holding
  `j/k` scrolls without firing a fetch per row.
- Background prefetch (bounded concurrency) warms detail for visible rows, which
  both fills the board's `!` column and pre-warms the card.
- Cache TTL and storage are unchanged (`internal/cache`).

## Expanded view

Behavior is unchanged: header + tabs (Conversation · Reviews · Checks · Diff),
`j/k` scroll, `J/K` step PR, `h/l`/`1-4` tabs, `esc` back, `↵` worktree. It
inherits the palette and the rebound `y`. Deep-link from the card's blocker is
retained (failing-checks → Checks tab, changes-requested → Reviews tab).

## Palette

Unchanged in mechanism: roles are 256-color indices that inherit the terminal's
theme (the lazytmux Catppuccin Mocha overlay) — `theme.go` is the source of
truth. This sweep adds no new roles; it reuses accent / dim / pass / fail /
pending / select / focus and the per-author palette. (The brainstorm mockups
approximated these in hex; the app keeps using indices.)

## Testing

Follow the existing table-driven style:

- **Row renderer** — column layout, glyph mapping per CI/review/draft state,
  title truncation/ellipsis, cursor-bar + select-mark gutter, the
  reliable-vs-detail-derived `!` rule (blank when detail uncached).
- **Side card** — content order per triage kind; blocker-headline selection
  (highest-priority state wins); checks broken out by name; comment folding;
  `UNKNOWN` → neutral headline.
- **Action bar** — the visible key set for each focused-PR state; core verbs
  always present; `a` overlay always complete.
- **Snappiness** — debounce gating (inject a clock/seam so a held-key burst fires
  one fetch); prefetch concurrency bound; cache-hit renders without a fetch.
- **Layout** — `computeLayout` is already covered; extend if the gutter changes
  list width math.

## Phasing

- **Phase A — the board.** Dense 1-line row renderer, gutter (focus bar + select
  mark), compact section headers, issue-row degradation. Keep the existing side
  card untouched. Ship density first.
- **Phase B — the card + detail plumbing.** Blocker-led card reorder, checks by
  name, folded comments; per-PR detail cache feeding the `!` column; bounded
  background prefetch.
- **Phase C — actions + polish.** Context-aware action bar, `y`/`Y` rebinding,
  debounced focus-fetch.

## Open decisions (resolved defaults)

- `y` = Copy URL, `Y` = Copy branch (above). Revisit if branch-copy is the more
  common reach.
- `!` column shows one glyph color-coded by severity (red conflict / yellow
  behind). If two states apply, conflict wins (higher in the ladder).
