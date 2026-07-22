# Unified tabbed detail pane + inline review comments

**Issue:** [#49](https://github.com/noamsto/prdash/issues/49)
**Date:** 2026-07-22
**Status:** Design approved, pending spec review

## Problem

prdash shows conversation comments and review summaries, but never the **inline
review comments** — the file+line threads that tell a reviewer what to actually
change. `gh pr view --json` does not expose them at all.

Adding them exposes a deeper structural issue: the PR detail is rendered by two
divergent code paths.

- `previewPane()` (`internal/ui/preview.go`) — a single *scrolling triage
  summary* (description snippet, blocker card, checks, latest activity) shown in
  the right-hand side pane when the terminal is wide (`computeLayout.ShowSide`).
- `expandedBody()` (`internal/ui/expanded.go`) — a *tabbed* full-screen view
  (Description │ Conversation │ Reviews │ Checks │ Diff), entered with `Enter`.

The same PR has two unrelated renderers, and reaching the tabbed content always
costs an `Enter`.

## Goals

- Show inline review comments, surfaced by **relevance** (unresolved-first), not
  as a browsing dump.
- Reach every detail tab **without `Enter`** when a side pane is present.
- Keep the triage-first identity: the common question — "what do I need to
  change?" — answerable with zero navigation.
- Collapse the two render paths into one component (a simplification, not just
  an addition).

## Non-goals (v1)

- Rendering the full unified diff / code hunks in the TUI. The Diff tab shows a
  file list plus comment threads; a 2–3 line hunk snippet above each thread is a
  later enhancement.
- Replying to or resolving threads from within prdash. Read-only in v1.
- Removing the `gh` CLI dependency wholesale (that is issue #47); this change
  uses `gh api graphql`, which fits that direction but does not complete it.

## Direction: one tabbed component, two containers

`previewPane()` and `expandedBody()` merge into a single tabbed renderer whose
container is chosen by `computeLayout.ShowSide`:

- **Wide** (`ShowSide == true`) → renders inside the side pane, tabs and all.
  No `Enter` required.
- **Narrow** (`ShowSide == false`) → the same tabs render full-screen. This is
  today's "dive-in", now purely the narrow-terminal renderer of the same
  component.

The full-screen `m.expanded` mode stops being a distinct content model; it
becomes the narrow (and `Enter`-maximized) presentation of the shared tabs.

### Tabs

```
 ▸Overview◂  Diff  Conversation  Reviews  Checks  Description
      1        2         3           4        5         6
```

| # | Tab          | Content |
|---|--------------|---------|
| 1 | Overview     | **Default.** Today's triage summary — identity, blocker card, checks line, description snippet, latest activity — **plus a top-unresolved-threads block** (zero-click). |
| 2 | Diff         | Changed files with `+/-`, each with its **inline threads grouped underneath**. Files with no comments collapse. |
| 3 | Conversation | Issue-level comments timeline (unchanged). |
| 4 | Reviews      | Review summaries: author + state + body (unchanged). |
| 5 | Checks       | CI checks (unchanged; keeps its check-cursor + rerun keys). |
| 6 | Description  | Full PR body markdown (unchanged). |

Overview keeps a description *snippet* with a "full text in Description tab"
hint; Description remains the full-body tab.

### Overview: top unresolved threads

A new `THREADS` section on the Overview tab, shown only when unresolved threads
exist:

```
 ─ THREADS ────────────────  3 unresolved
   preview.go:288  alice
     this allocates a new slice every frame
   prview.go:61    bob
     add the reviewThreads graphql field
   ▸ 1 more · 2 resolved hidden
```

- Lists the top N unresolved threads (N = 2, matching `previewN`), then a
  `▸ {rest} more · {resolved} resolved hidden` line.
- Each entry: `file:basename():line` (focus color) + author + a one-line dim
  body preview.

### Diff tab: threads grouped by file

```
   3 files   +47 -12       2 unresolved · 2 resolved

 ▾ internal/ui/preview.go            +12  -3
     L288  alice · 2h                  ● unresolved
       this allocates a new slice every frame
       └ you · 1h
         fixed in the latest push
     L301  bob · 3h                    ✓ resolved
       nit: rename bw → boxWidth

 ▾ internal/gh/prview.go             +5   -0
     L61   bob · 3h                    ● unresolved
       add the reviewThreads graphql field here

 ▸ internal/preview/timeline.go      +30  -9   (no comments)
```

- Extends today's `renderDiffstat`: each file row is followed by its threads.
- A thread renders its root comment then replies indented under a `└` connector.
- Unresolved threads lead; resolved threads collapse behind a
  `▸ N resolved` toggle per file (hidden by default — decision below).
- Files with no comments render as today (single stat line).

## Navigation

The list keeps focus throughout; switching tabs re-renders the active tab for
the current cursor row live.

| Key            | Action |
|----------------|--------|
| `h` / `l`      | Previous / next tab (wraps). Replaces today's `l`=dive-in; `h` was unbound. |
| `1`–`6`        | Jump to tab by index. (Digits are unbound in the main view today.) |
| `j` / `k`      | Move list cursor (unchanged); active tab re-renders for the new row. |
| `ctrl+j`/`ctrl+k` | Scroll the pane/tab content (unchanged). |
| `Enter`        | Maximize the current tab full-width (`Esc` to restore). On a narrow terminal (no side pane), opens the full-screen tabbed view. |
| `Esc`          | Restore from maximize / close the narrow full-screen view. |

- `z` (today's `previewMax` toggle) becomes redundant with `Enter`-maximize;
  keep as a hidden alias initially, remove once `Enter` is proven.
- `tab` stays the PR/Issue mode toggle — not repurposed.
- On the **Checks** tab, `r`/`R` (rerun hovered / all failed) still apply, as in
  the current expanded Checks tab.
- **Issue rows** keep their simpler pane (no PR-only tabs like Diff/Reviews);
  the tabbed component is PR-scoped, matching today's "expanded is PR-only".

## Data source

Inline threads come from GitHub GraphQL `reviewThreads`, fetched via
`gh api graphql`. One call returns path, line, `isResolved`, and the ordered
comments (author, body, timestamp) per thread — everything the display needs.
REST (`/pulls/{n}/comments`) would need a second call and carries no resolve
state.

### New gh layer types (`internal/gh`)

```go
type ReviewThread struct {
    Path       string
    Line       int        // resolved line; falls back to originalLine
    IsResolved bool
    Comments   []ThreadComment
}

type ThreadComment struct {
    Author    string
    Body      string
    CreatedAt time.Time
}
```

- Add a `ReviewThreadsArgs(owner, repo string, number int) []string` builder for
  the `gh api graphql -f query=… -F …` invocation, and a parser
  `ParseReviewThreads([]byte) ([]ReviewThread, error)` for the GraphQL response
  envelope (`data.repository.pullRequest.reviewThreads.nodes`).
- Owner/repo: reuse the existing repo-detection already used elsewhere (the
  detail cache is keyed by repo — see `detailKey`).

### Fetch & cache

- New `tea.Cmd` (`fetchThreadsCmd`) parallel to `fetchDetailCmd`, storing into a
  `m.threads map[int][]gh.ReviewThread` keyed by PR number, mirroring
  `m.detail`.
- Cache to disk like `prDetailMsg.raw`, with its own schema version constant
  (`threadsSchemaVer`) and cache key (`threadsKey(repo, number)`), so a field-set
  change is a clean miss.
- Fetched lazily on selection (same debounce path as detail), so the Overview
  THREADS block and the Diff tab paint as soon as threads arrive; before then,
  the block is omitted and the Diff tab shows files only.

## Rendering ownership

- The tab set, active-tab state, and tab-bar rendering move into the shared
  component so both containers use them. `expandedTab` / `expandedTabs` generalize
  to the pane; the side pane gains a tab bar it currently lacks.
- Tab **content renderers** are width-parameterized (they already take a width),
  so the only difference between side-pane and full-screen is the width and the
  surrounding box — no separate content code.
- `renderDiffstat` grows a threads-aware variant; `previewPane()`'s current
  blended body becomes the Overview tab renderer.

## Testing

- **Layout/width:** extend the existing layout-sweep regression tests to assert
  the tabbed pane renders within `SideWidth` at representative widths, and that
  the narrow renderer fills the full width. No horizontal overflow (charm-tui
  border-bleed class of bug).
- **gh parsing:** golden-file test for `ParseReviewThreads` against a captured
  GraphQL response (resolved + unresolved + multi-reply threads, missing `line`
  falling back to `originalLine`).
- **Overview threads block:** unit test the top-N fold + "N more · N resolved
  hidden" summary from a synthetic thread set (0 threads → block omitted; all
  resolved → block omitted).
- **Diff tab grouping:** threads attach to the right file; files with no threads
  render the plain stat line; resolved-collapse toggle hides/shows.
- **Navigation:** `h`/`l` wrap; `1`–`6` jump; cursor move re-renders the active
  tab; `Enter` maximizes and `Esc` restores.

## Phases

1. **Restructure** — unify `previewPane()` + `expandedBody()` into the tabbed
   component with an Overview tab; wire `h`/`l`/`1`–`6`/`Enter`-maximize; port
   existing tabs. No new data. Behavior parity plus tabs-in-pane.
2. **Inline comments** — add the gh GraphQL layer + fetch/cache; render the
   Overview THREADS block and the Diff-tab threads; unresolved-first with the
   resolved-collapse toggle.

## Open decisions (resolved)

- **Resolved threads:** unresolved-first; resolved collapse behind a
  `▸ N resolved` toggle, hidden by default.
- **Data source:** GraphQL `reviewThreads` via `gh api graphql`.
- **Diff hunk context:** v1 shows `file:line + comment` only; code-hunk snippet
  is a later enhancement.
