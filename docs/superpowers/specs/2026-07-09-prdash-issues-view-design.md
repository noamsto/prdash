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

- **`i`** — flip `prs ⟷ issues`. Swaps `m.section` (`NewPRSection`/`NewIssueSection`),
  swaps `m.actions` (`DefaultPRActions`/`DefaultIssueActions`), resets cursor and
  selection, kicks a fetch.
- PR-only keys are inert in issue mode: `F` (author picker), `R` (reviewers),
  `D` (drafts), and the merge/rerun/update/ready actions (`m`/`r`/`u`/`M`).
- Shared in both modes: `s`, `f`, `/`, `space`, `V`, `enter`, `W`, `o`, `y`/`Y`,
  and all navigation.

## Mode-dependent state & presets

Today `prStates` and `defaultPresets` are globals. They become mode-keyed:

| mode   | states (`s` cycles)      | presets (`f` cycles)                |
|--------|--------------------------|-------------------------------------|
| PRs    | open · merged · closed   | mine · all *(unchanged)*            |
| Issues | open · closed            | mine (`assignee:@me`) · all         |

Issues have no "merged" state and no reviewer axis. `searchFor` / `splitState` /
`nextState` take the applicable state list as a parameter instead of reading a
global. "mine" for issues means `assignee:@me` (issues assigned to you), not
authored-by-you.

## Fetch, cache, hydrate

Mirror the PR fetch path with an issue twin:

- `issueFetchCmd` → `gh issue list` (existing `gh.IssueListArgs`) →
  `issuesFetchedMsg` → `IssueSection.SetIssues`.
- Cache under `issueKey(repo, filter)` using the existing `cache.Key` helper with
  a distinct prefix, so PR and issue caches never collide.
- `hydrate` paints issue rows from cache on toggle, just as preset switches do now.

## Light issue preview — built to grow

`previewPane()` gains an `*IssueSection` branch. **v1:** identity header + issue
**body** rendered through the existing `preview.Render` (glamour). Detail is stored
in a new `m.issueDetail map[int]gh.IssueDetail`, lazily fetched on cursor-settle
via the existing `debounceDetailCmd` machinery (`gh issue view {n} --json body`).

The grow path to a full comments timeline is baked in: `gh.IssueDetail` is defined
now with a `Body string` field and an empty `Timeline []TimelineItem` field. When
the timeline milestone lands (`--json comments` + a timeline renderer), the
preview and expanded view fill in with no structural change — same detail map,
same debounce, same expanded-tab slot.

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
- `i` toggle swaps section + actions and resets cursor/selection
- PR-only keys (`F`/`R`/`D`/`m`) are inert in issue mode
- issue `previewPane` renders the body

## Scope

**In:** toggle, issue list, mine/all + open/closed, issue actions
(worktree / bulk worktrees / open / copy), body preview, pretty header.

**Out (deferred to the timeline milestone):** comments/events timeline,
issue-specific expanded tabs, label/assignee editing, issue creation.
