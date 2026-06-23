# prdash TUI redesign — airy layout + dynamic triage preview (design)

**Date:** 2026-06-23
**Status:** LOCKED (brainstormed with visual companion; one adversarial spec-critic pass, all findings folded in)
**Author:** Noam
**Builds on:** `2026-06-22-prdash-design.md` and the merged Plans 1–4.

## Purpose

The shipped TUI works but looks like a prototype: no contained panes, the
preview overflows its area, fixed column widths don't respond to terminal size,
and there's no real header/footer. This redesign makes prdash look professional
and — more importantly — turns the preview from a passive comment dump into a
**dynamic triage surface** that leads with *what's blocking the merge* and *the
one key that fixes it*.

This is net-new product surface, not a restyle. It is delivered as one spec and a
phased plan (A → B → C below).

Non-goal: re-implementing a full diff viewer or a GitHub web client in the
terminal. prdash stays worktree-first — deep work happens in the worktree/editor.

## Aesthetic

Minimal & airy. Full-width content, generous spacing, a single accent color,
muted secondary text, and colored **state glyphs** (`✓` pass / `✗` fail /
`●` pending / `·` none). No heavy boxes — subtle dividers and a quiet status/key
bar. Inherits the lazytmux Catppuccin overlay for actual color values; the design
references roles (accent, dim, pass, fail, pending, warn), not hex.

## The three surfaces

### 1. List (browse + act)
Full-width, airy rows. Each PR/issue renders as a **title line** plus a **dim meta
line** (`author · age · labels · review · CI glyph`). Cursor row highlighted; a
`●` gutter marks multi-selected rows. The list is the cockpit — all actions fire
from here: `m` merge · `↵` open worktree · `r` rerun failed · `u` update-branch ·
`a` action overlay · `space`/`V` select · `/` filter · `q` quit.

**Decision:** the default row is the **2-line** form (title line + dim meta line)
— chosen for scannability. The 1-line form is a visual iteration we may try live
during Phase A (the row renderer is a single function), but it is *not* a shipped
config toggle. Phase A's "done" is the 2-line renderer.

The complete keymap is in §"Action set" below; the triage card's suggested-action
keys reference only bindings defined there.

### 2. Dynamic side preview (the triage card)
A right-hand pane shown **only when the terminal is wide enough** (threshold
default ~120 cols). On narrow terminals it is hidden and the user expands instead.
Content is **contextual**, driven by the focused PR's merge state — it leads with
the top blocker and its fix, then a compact status block, then secondary signals.
It is *not* the comment timeline (that moves to Expanded).

**Data source — the detail fetch, not the list.** `mergeStateStatus`/`mergeable`
are computed server-side and come back `UNKNOWN` far more often in a bulk
`gh pr list` than in a single `gh pr view`. So the triage card is built from the
**per-focused-PR detail fetch** (the Plan 4 lazy-fetch, extended — see §Data),
where GitHub has computed the state for that one PR. The *list* row needs none of
this: it shows the CI rollup glyph (already fetched) plus `reviewDecision`/
`isDraft` (cheap, reliably populated). The card handles `UNKNOWN` with a neutral
"merge state pending…" and a follow-up refetch, never a wrong blocker.

**Verify before Phase B (carry the base spec's rigor):** confirm `gh pr view
--json mergeStateStatus,mergeable` returns populated values for a real open PR,
and that a freshly-pushed PR transitions out of `UNKNOWN` on refetch. If even the
single-PR view is unreliable, the card degrades to CI + review only (still useful)
and the merge-conflict/behind/draft rows are dropped.

### 3. Expanded view
`→` expands into the focused PR (whether `tab` also expands or instead cycles the
preview tabs is an open decision below): a header (title · meta · state) and a tab
strip **Conversation · Reviews · Checks · Diff(stat)**, scrollable. `j/k` move
to the next/prev PR without collapsing; `esc` returns to the list; `↵` still opens
the worktree. **Deep-link:** expanding from a blocker card lands on the relevant
tab (failing-checks → Checks; changes-requested → Reviews) rather than always
Conversation.

## The triage ladder

The side card and the expand deep-link are driven by GitHub's own merge-state
signals — `mergeStateStatus`, `mergeable`, `isDraft`, `reviewDecision` (all from
the detail fetch, per §2) — rather than re-derived heuristics. Show the
**highest-priority** state that applies (suggested-action keys are defined in
§"Action set"):

| # | Signal | Headline | Suggested action | Deep-link tab |
|---|--------|----------|------------------|---------------|
| 1 | `isDraft` / `DRAFT` | Draft — not ready | `a` → Mark ready | — |
| 2 | `DIRTY` / `CONFLICTING` | Conflicts with base | `↵` worktree to resolve | — |
| 3 | `BLOCKED` + failing checks | ✗ N checks failing (list them) | `r` rerun failed¹ | Checks |
| 4 | `reviewDecision: CHANGES_REQUESTED` | ✎ Changes requested (the ask) | `↵` worktree to address | Reviews |
| 5 | `BEHIND` | Behind base by N commits | `u` update branch | — |
| 6 | `BLOCKED`, no failures | N unresolved conversations | — | Conversation |
| 7 | `REVIEW_REQUIRED` + reviewRequests | Waiting on @reviewer · 0 approvals | — | Reviews |
| 8 | `UNSTABLE` / pending | ● checks running… | (wait) | Checks |
| 9 | `CLEAN` | ✓ Ready to merge | `m` merge | — |
| 10 | fallback | latest comment / activity | — | Conversation |

¹ `r` reruns the latest **GitHub Actions** run for the head branch (the existing
`rerun-failed` builtin). Failing *external statuses* (StatusContext checks) and
non-Actions required checks can't be rerun this way — for those the card lists the
failures but omits the `r` hint. Card copy must not over-promise.

**Secondary signals** (shown quietly below the headline, never as blockers):
stale approval (dismissed by new commits), size (`+adds −dels`, N files), age /
last-updated, "do-not-merge"/"blocked"/"WIP" labels, comment count, linked issue.

When `mergeStateStatus` is `UNKNOWN` (GitHub still computing), the card shows a
neutral "merge state pending…" and schedules a single refetch, rather than a wrong
blocker.

## Action set (post-redesign)

The complete default keymap. Top-level keys are the common verbs; rarer actions
live in the `a` overlay only. Every "suggested action" in the triage ladder
references a key here.

| Key | Action | Scope | Notes |
|-----|--------|-------|-------|
| `↵` | Open worktree | single | exits-TUI (Plan 2) |
| `W` | Bulk worktrees | per-selected | exits-TUI (Plan 3) |
| `m` | Merge (squash) | single | confirm, default-No (Plan 2) |
| `r` | Rerun failed | single | latest Actions run (see ladder ¹) |
| `u` | Update branch | single | **new** — `gh pr update-branch` (ladder #5) |
| `o` | Open in browser | single | existing |
| `y` | Copy branch | single | existing (OSC52) |
| `a` | Action overlay | — | fuzzy menu incl. rare verbs |
| `space` / `V` | Select / select-all | — | Plan 3 |
| `/` | Filter | — | Plan 1 |
| `→` | Expand focused PR | — | **new** (Phase C) |
| `j`/`k`, `↑`/`↓` | Move cursor | — | (in expanded: `j`/`k` step PRs) |
| `q` / `ctrl+c` | Quit | — | |

**Overlay-only** (no top-level key): **Mark ready** (`gh pr ready` — ladder #1),
and future rare verbs (close/reopen). The base spec's `d` (diff-in-nvim) is
**dropped** — the diff now lives in the expanded **Diff(stat)** tab; deep work
happens in the worktree. `o` and `y` are **retained**.

## Data

**List fetch (`prFields`)** — already has `statusCheckRollup`, `reviewDecision`,
`labels`, `author`, `updatedAt`. Add only `isDraft` (static, reliably populated).
Bumping the list `--json` set bumps the cache `schemaVer` (a changed field set is a
clean miss, per the existing cache contract). Do **not** add `mergeStateStatus`/
`mergeable` to the list — they're unreliable in bulk (see §2).

**Detail fetch (`PRViewArgs` / `PRDetail`) — extended, not pure reuse.** Plan 4's
`PRDetail{Comments, Reviews}` and `PRViewArgs` grow:
- `mergeStateStatus`, `mergeable` — the triage card's merge-state source.
- `reviewRequests` — pending reviewers (ladder #7).
- `files` — diffstat (per-file additions/deletions; for the Diff tab + the "size"
  secondary signal).
- parse `latestReviews` (already requested in the args, currently discarded) for
  the Reviews tab / stale-approval signal.
The detail cache is the in-memory `m.detail map[int]gh.PRDetail` (cleared per
launch), so it needs **no** `schemaVer` — but the struct + parser changes are real
work, not reuse.

**Per-check names — `statusCheckRollup` is a heterogeneous union.** Two element
shapes: **CheckRun** (`name`, `workflowName`, `status`, `conclusion`) and
**StatusContext** (`context`, `state` — no `name`/`conclusion`). Today `gh.Check`
parses only `{State, Conclusion}`. Extend it to also capture `name`, `workflowName`,
`context`, and resolve the display label as **`name` → `workflowName` → `context`**
and the state as **`conclusion` → `state`** (the existing `CIState()` already does
the state fallback). Without this, the Checks tab shows blank rows for any repo
using classic commit statuses.

## Architecture / components

- **`internal/ui/theme.go`** — lipgloss style registry: accent/dim/state styles,
  glyphs, dividers, the status/key bar style. Single source of visual truth.
- **`internal/ui/layout.go`** — responsive layout: from the `tea.WindowSizeMsg`
  size, decide list-only vs list+side, compute pane widths/heights, and the
  list↔expanded mode. Replaces the ad-hoc `previewWidth` math.
- **List rendering — the highest-risk change.** The airy two-line rows + select
  gutter do **not** map onto `bubbles/table` (column-oriented, one row per item),
  so the table is replaced with a **custom viewport-based list**. `bubbles/viewport`
  scrolls a pre-rendered string and provides *no* row/cursor/selection model — so
  the renderer owns **everything** the table used to: row layout, the cursor
  highlight, and the `● ` selection marker (which today is string-injected into
  `rows[i][0]` in `applyFilter`). This ripples through machinery three prior plans
  built on `m.table.Cursor()`:
    - **Cursor ownership moves to the model** (`m.cursor int`, indexing the *shown*
      set), replacing `m.table.Cursor()`. `cursorVars()` (actions), `detailCmdFor
      Cursor()` (preview), `sel.toggle(cursor)` and the bulk fallback all switch to
      `m.cursor`. Scroll-follows-cursor becomes the model's job (clamp + set
      viewport offset).
    - **`Section` interface changes:** `Rows() []table.Row` / `Columns()` are
      replaced by `RenderRow(i, focused, selected, width) string` (PR and issue
      rows differ). `Len`/`SetShown`/`VarsAt`/`Haystacks` keep the same `shown[]`
      index contract — the cursor still indexes the shown set, so multi-select and
      filter semantics (incl. the Plan-3 "clear selection when shown changes" fix)
      carry over unchanged.
  This is isolated to Phase A but its blast radius is the whole `ui` package, not
  one file — the plan must sequence it as: model-owned cursor → custom renderer →
  rewire actions/preview/selection → delete the table.
- **`internal/triage/triage.go`** (pure, table-tested) — given a PR + its detail +
  merge state, return the ranked `Card{Headline, Detail, ActionKey, JumpTab}`.
  No UI/IO; deterministic; unit-tested against the ladder.
- **`internal/ui/expanded.go`** — the expanded mode: tab strip, per-tab scrollable
  content (Conversation / Reviews / Checks / Diffstat), `j/k` PR stepping, deep
  -link entry point (open at a given tab).
- **Renderers** — per-check list (names + state + duration + `↵ logs`), diffstat
  (file list + `+/−` + totals). Reuse the Plan 4 glamour renderer for bodies.
- **Status/key bar** — context-aware bottom line: repo · open count · active
  filter · selection count · mode-specific keys.

The action model (Plan 2), multi-select (Plan 3), cache (Plan 1), and lazy detail
fetch (Plan 4) are reused. New verbs per §"Action set": `u` (update-branch,
top-level) and Mark-ready (`gh pr ready`, overlay-only).

## Phasing

- **Phase A — Visual foundation.** theme + responsive layout + airy custom list
  renderer + contained side pane (still shows a conversation peek) + status/key
  bar. *Outcome: looks professional, nothing overflows.* (Biggest structural work:
  the table → custom-list replacement.)
- **Phase B — Dynamic triage card.** new `gh` fields + `internal/triage` + render
  the card with suggested actions; wire `u`/`ready` actions.
- **Phase C — Expanded mode.** tabbed detail (Conversation/Reviews/Checks/
  Diffstat) + deep-link from the triage card.

Each phase is independently shippable and leaves the TUI in a working state.

## Testing

- **`internal/triage`** — pure unit tests over the ladder: each `mergeStateStatus`
  / review / checks combination → expected card (headline, action, jump tab),
  including `UNKNOWN` and the priority ordering.
- **Layout** — table tests: given terminal (w,h) → expected pane split / list-only
  vs list+side / expanded geometry. Pure function, no rendering.
- **List/expanded renderers** — assert structural facts (row count, selected
  gutter marker present, active tab, deep-link lands on the right tab) as the
  existing UI tests do; visual polish verified live in tmux.
- The table→list swap rewrites the list-rendering tests: there is no longer a
  `table.Row`/cell type, so assertions move from `m.table.Rows()[i][0] == "#7"` to
  the rendered row string (`RenderRow` contains `#7`) and to model state
  (`m.cursor`, selection set, the `● ` marker present in a selected row's render).
  The *intent* of each existing test is preserved; the surface changes. All other
  packages' tests stay green.

## Out of scope

Full in-TUI syntax-highlighted diff pager (diffstat only; full diff → worktree/
browser); inline review-thread (file/line) comments (unchanged from base spec —
not in `gh --json`); theming UI; multi-repo; a shipped 1-row/2-row config toggle
(2-line is the decided default).

## Open implementation decisions (resolved during build, not blockers)

- Exact wide-terminal threshold for showing the side pane (default ~120 cols).
- Whether `tab` also expands or instead cycles the preview tabs while `→` expands
  (settle in Phase C).
- Trying the 1-line row form as a live visual iteration in Phase A (default ships
  2-line regardless).
