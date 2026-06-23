# prdash TUI redesign — airy layout + dynamic triage preview (design)

**Date:** 2026-06-23
**Status:** Proposed (brainstormed with visual companion; approved in chat)
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

The 1-line vs 2-line (title + sub-row) presentation is a **build-time toggle**
evaluated live during Phase A; both are cheap to render. Default leans 2-line for
scannability.

### 2. Dynamic side preview (the triage card)
A right-hand pane shown **only when the terminal is wide enough** (threshold
configurable; default ~120 cols). On narrow terminals it is hidden and the user
expands instead. Content is **contextual**, driven by the focused PR's merge
state — it leads with the top blocker and its fix, then a compact status block,
then secondary signals. It is *not* the comment timeline (that moves to Expanded).
Lazily fetched per focused PR and cached (the Plan 4 lazy-fetch already exists).

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
signals — `mergeStateStatus`, `mergeable`, `isDraft`, `reviewDecision` — rather
than re-derived heuristics. Show the **highest-priority** state that applies:

| # | Signal | Headline | Suggested action | Deep-link tab |
|---|--------|----------|------------------|---------------|
| 1 | `isDraft` / `DRAFT` | Draft — not ready | `gh pr ready` | — |
| 2 | `DIRTY` / `CONFLICTING` | Conflicts with base | `↵` worktree to resolve | — |
| 3 | `BLOCKED` + failing checks | ✗ N checks failing (list them) | `r` rerun failed | Checks |
| 4 | `reviewDecision: CHANGES_REQUESTED` | ✎ Changes requested (the ask) | `↵` worktree to address | Reviews |
| 5 | `BEHIND` | Behind base by N commits | `u` update branch | — |
| 6 | `BLOCKED`, no failures | N unresolved conversations | — | Conversation |
| 7 | `REVIEW_REQUIRED` + reviewRequests | Waiting on @reviewer · 0 approvals | — | Reviews |
| 8 | `UNSTABLE` / pending | ● checks running… | (wait) | Checks |
| 9 | `CLEAN` | ✓ Ready to merge | `m` merge | — |
| 10 | fallback | latest comment / activity | — | Conversation |

**Secondary signals** (shown quietly below the headline, never as blockers):
stale approval (dismissed by new commits), size (`+adds −dels`, N files), age /
last-updated, "do-not-merge"/"blocked"/"WIP" labels, comment count, linked issue.

When `mergeStateStatus` is `UNKNOWN` (GitHub still computing), the card shows a
neutral "merge state pending…" rather than a wrong blocker.

## Data

Most fields are already fetched (`statusCheckRollup`, `reviewDecision`, `reviews`,
`comments`, `labels`, `updatedAt`). Cheap additions to the existing `gh` calls:

- List/detail: `mergeStateStatus`, `mergeable`, `isDraft`, `reviewRequests`.
- Detail: `files` (diffstat — per-file additions/deletions), optionally
  `latestReviews` (already requested in the args but not parsed).
- Extend the `gh.Check` struct with the check **name/workflow** so the Checks tab
  can label each row (today only state/conclusion are parsed).

Adding fields to the `--json` set bumps the cache `schemaVer` (a changed field set
becomes a clean miss, per the existing cache contract).

## Architecture / components

- **`internal/ui/theme.go`** — lipgloss style registry: accent/dim/state styles,
  glyphs, dividers, the status/key bar style. Single source of visual truth.
- **`internal/ui/layout.go`** — responsive layout: from the `tea.WindowSizeMsg`
  size, decide list-only vs list+side, compute pane widths/heights, and the
  list↔expanded mode. Replaces the ad-hoc `previewWidth` math.
- **List rendering** — the airy two-line rows + select gutter do **not** map onto
  `bubbles/table` (column-oriented, one row per item). Replace the table with a
  **custom viewport-based list renderer**: the `Section` interface gains a
  "render row(s) for item i" responsibility (so PR and issue rows can differ),
  and the model owns a `bubbles/viewport` for scroll. This is the largest
  structural change and the main risk; it is isolated to Phase A.
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
fetch (Plan 4) are reused; new actions `u` (update-branch) and `gh pr ready` join
the default set.

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
- Existing tests must stay green; the table→list swap will rewrite the
  list-rendering tests but preserve their assertions (row content, `#N` cell,
  selection marker).

## Out of scope

Full in-TUI syntax-highlighted diff pager (diffstat only; full diff → worktree/
browser); inline review-thread (file/line) comments (unchanged from base spec —
not in `gh --json`); theming UI; multi-repo. The 1-row/2-row choice is a live
toggle, not a spec decision.

## Open implementation decisions (resolved during build, not blockers)

- 1-line vs 2-line row default (try both live in Phase A).
- Exact wide-terminal threshold for showing the side pane.
- Whether `tab` and `→` both expand, or `tab` cycles preview tabs while `→`
  expands (settle in Phase C).
