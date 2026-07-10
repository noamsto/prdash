# prdash — Issues board

**Issue:** [#22](https://github.com/noamsto/prdash/issues/22)
**Date:** 2026-07-09

## Problem

prdash shows a PR board. The data, rendering, and action layers for issues already
exist (`gh.FetchIssues`/`gh.Issue`, `IssueSection`, `action.DefaultIssueActions`,
`issue.Branch`) but were never wired into the TUI. Wire them in as a first-class
board the user toggles into from the PR list.

## Concept — a board mode

The Model gains one axis: **mode** ∈ `{prs, issues}`. `state`, `preset`, `section`,
and `actions` all become functions of the mode. Pressing `i` flips the mode; the
board re-fetches (cached → instant) and repaints. No new screens or components —
the `Section` interface already abstracts PR-rows vs issue-rows, so the existing
list/preview/board chrome renders both.

## Toggle & key map

- **`i`** — flip `prs ⟷ issues`. Swaps `m.section` (`NewPRSection`/`NewIssueSection`)
  and `m.actions` (`DefaultPRActions`/`DefaultIssueActions`), then runs the
  mode-aware switch (see *Fetch, cache, hydrate*).

  **View state on toggle** — the flip must reset all per-item and preview state so
  nothing stale from the other board leaks through:
  - **reset:** `cursor`, selection (`sel`), `previewExpanded`, `previewMax`,
    `previewOffset`, `hideDrafts` (a PR-only filter — clear it and re-apply so
    `PRSection.SetHideDrafts` never lingers into issue mode), and bump `detailSeq`
    (so an in-flight PR detail fetch can't paint into the issue preview); clear
    `err`.
  - **preserve:** `repo`, dimensions, `themeMode`, and each mode's own
    `state`/`preset` (so flipping back lands where you left it).

- **`l` / `right` / expanded view is disabled in issue mode (v1).** The expanded
  view (`enterExpanded`, `internal/ui/expanded.go`) is hardwired to PR tabs
  (`Conversation/Reviews/Checks/Diff`) and reads the PR `m.detail` map — for an
  issue it would stick on "Loading…" forever. In issue mode `l`/`right` is a no-op;
  the body preview in the side pane is the whole story for v1. The timeline
  milestone later adds a body-only issue expanded reading `m.issueDetail`.

- **PR-only keys are inert in issue mode:** `F` (author picker), `R` (reviewers),
  `D` (drafts), and the merge/rerun/update/ready actions (`m`/`r`/`u`/`M`).

- **Shared nav/overlay keys work in both modes:** `s`, `f`, `/`, `space`, `V`,
  `enter`, `W`, `o`, and all cursor movement.

- **Copy actions must be *added* to `DefaultIssueActions`.** Today that map has
  only `enter`/`W`/`o` (`internal/action/defaults.go:35`), so `y`/`Y`/`b` are inert
  in issue mode as written. Add `copy-number` (`y`), `copy-url` (`Y`), and
  `copy-branch` (`b`) — the derived branch is already available via
  `IssueSection.VarsAt` (`internal/ui/section.go:245`). Make `copiedLabel`
  (`internal/ui/actions.go:49`, hardcoded "PR number"/"PR numbers") kind-aware so
  issue copies read "issue number".

## Mode-dependent state & presets

Today `prStates` and `defaultPresets` are globals. They become mode-keyed:

| mode   | states (`s` cycles)      | presets (`f` cycles)                |
|--------|--------------------------|-------------------------------------|
| PRs    | open · merged · closed   | mine · all *(unchanged)*            |
| Issues | open · closed            | mine (`assignee:@me`) · all         |

Issues have no "merged" state and no reviewer axis. `searchFor` / `splitState` /
`nextState` take the applicable state list as a parameter instead of reading a
global. "mine" for issues means `assignee:@me` (issues assigned to you), not
authored-by-you — a **single** search, *not* the PR "mine" categorized dual-fetch
(`mineFetchCmd`/`setMine`). `mineFetchCmd` is not wired for issues.

Making presets mode-keyed touches every current consumer of the `defaultPresets`
global: `presetIndexFor`/`nextPreset` (`filter_presets.go:59-70`), `isMineView`
(`prlist.go:1073`), and the header label (`prlist.go:1030`). Each must read the
active mode's preset list.

## Fetch, cache, hydrate

Mirror the PR fetch path with an issue twin:

- `issueFetchCmd` → `gh issue list` (existing `gh.IssueListArgs`; `gh issue list`
  excludes PRs by default, so no post-filtering) → `issuesFetchedMsg` →
  `IssueSection.SetIssues`.
- Cache under `issueKey(repo, filter)` using the existing `cache.Key` helper with
  a distinct prefix, so PR and issue caches never collide.
- **Mode-aware hydrate + switch.** Today `hydrate()` (`prlist.go:275`) and
  `switchToFilter()` (`prlist.go:493`) are PR-only. Add an issue path: an
  `issueKey` cache lookup → `IssueSection.SetIssues` for the instant paint, plus
  the same `refreshing`/`loaded` bookkeeping the PR switch does, so the board shows
  "Loading…" before the first fetch and "No issues" (not a blank) once a fetch
  returns empty. The `i` toggle and `s`/`f` within issue mode both route through
  this switch.
- **`backgroundRefresh` stays PR-only.** It runs `gh pr list` (`prlist.go:481`) and
  is only reached from the checks poll (never fires for issues) and from actions
  with `Refresh:true`. No v1 issue action sets `Refresh`, so it stays inert — but
  any future issue action that needs a refresh must get an issue-aware refresh, not
  the PR one.

## Light issue preview — built to grow

`previewPane()` gains an `*IssueSection` branch. **v1:** identity header + issue
**body** rendered through the existing `preview.Render` (glamour). Detail is stored
in a new `m.issueDetail map[int]gh.IssueDetail`, lazily fetched on cursor-settle
via the existing `debounceDetailCmd` machinery (`gh issue view {n} --json body`).

The grow path to a full comments timeline is baked in: this work **defines a new**
`gh.IssueDetail` (mirroring `gh.PRDetail` in `internal/gh/prview.go`) with a
`Body string` field and an empty `Timeline []TimelineItem` field (`TimelineItem`
also new). `gh.IssueViewArgs`/`ParseIssueDetail` are added alongside. When the
timeline milestone lands (`--json comments` + a timeline renderer), the preview and
the future body-only issue expanded fill in with no structural change — same detail
map, same debounce.

## The pretty part — segmented mode indicator

The header prefix becomes a segmented control showing both boards, active one lit:

```
  noamsto/prdash    PRs │ Issues      mine · open · 12   ⠹ refreshing
                    ▔▔▔                active = accent+bold, inactive = dim
```

Active segment in `accentStyle` bold; inactive in `dimStyle`; thin `│` divider.
The legend (`?`) and docked key panel swap their hint rows to the active mode's
keys — issue mode drops the merge/reviewer hints, leaving a smaller, cleaner set.

## Testing

Table tests in the existing `prlist_test.go` / `section_test.go` style:

- mode-keyed `nextState` and preset cycling
- `issueKey` distinct from `prKey` for the same repo/filter
- `issuesFetchedMsg` populates issue rows
- `i` toggle swaps section + actions and resets the full view-state list (cursor,
  selection, `previewExpanded`, `previewMax`, `previewOffset`, `hideDrafts`)
- PR-only keys (`F`/`R`/`D`/`m`) and `l`/expanded are inert/no-op in issue mode
- copy actions (`y`/`Y`/`b`) work in issue mode; `copiedLabel` reads "issue …"
- issue `previewPane` renders the body; empty fetch shows "No issues", pre-fetch
  shows "Loading…"

## Scope

**In:** toggle, issue list, mine/all + open/closed, issue actions
(worktree / bulk worktrees / open / copy), body preview, pretty header.

**Out (deferred to the timeline milestone):** comments/events timeline,
issue-specific expanded tabs, label/assignee editing, issue creation.
