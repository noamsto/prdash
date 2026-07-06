# prdash: merged/closed PRs + live summary refresh

Date: 2026-07-06

## Goal

Two related gaps in the TUI:

1. **Merged/closed PRs are unreachable.** Every filter preset hardcodes `is:open`, so
   there is no way to browse PRs that have already merged (or closed) with the same
   filters (mine / all / by-author) that apply to open PRs.
2. **Actions don't refresh the view.** After a mutating action — `Merge`, `Update
   branch`, `Mark ready`, `Rerun checks`, or a reviewer change — only the status badge
   settles. The PR list and the cached PR detail (what the side preview / summary
   renders) stay stale until the next manual refresh.

## Background: current architecture

- **Filter presets are gh search strings.** `defaultPresets` holds `{"mine",
  "is:open author:@me"}` and `{"all", "is:open"}`. The `mine` view is special: it
  fetches two searches (`mineFilter` = `is:open author:@me`, `reviewFilter` =
  `is:open review-requested:@me`) and renders them as two sections. A custom author
  filter is built as `"is:open " + author:X …`. The state (`is:open`) is baked into
  every search string.
- **`m.filter`** is the fully-resolved search string. It is the cache key
  (`prKey(repo, filter)`), the `gh pr list` argument, and (via `presetIndexFor`) the
  preset identity. `isMineView` keys on the preset *name*, not the filter string.
- **Detail freshness.** The side preview / summary renders from `m.detail[number]`
  (a `gh.PRDetail`). `m.fresh[number]` gates revalidation: `detailCmdForCursor` and
  `prefetchCmd` skip any number already marked fresh this session. A `prsFetchedMsg`
  (list arrival) fans out `detailCmdForCursor` + `prefetchCmd`, which refetch details
  only for numbers *not* marked fresh.
- **Actions.** `runAction` (single) and `runBulk` (per-selected) run the gh command
  and settle via `actionDoneMsg`, which today only updates the status badge. The lone
  exception is the reviewer picker (`assignReviewersCmd`), which refetches the list
  but not the detail.

## Part 1 — State as an orthogonal dimension

Split the open/merged/closed state out of the preset search so it composes with every
existing filter.

### Model

Add two fields to `Model`:

- `state string` — one of `"open"`, `"merged"`, `"closed"`; defaults to `"open"`.
- `body string` — the state-agnostic qualifier for the active view (e.g.
  `"author:@me"`, `""` for `all`, or `"author:alice author:bob"` for a custom pick).

`m.filter` remains the resolved string and is always recomputed as
`searchFor(m.state, m.body)`. Because the resolved string differs per state, merged
and open results cache under distinct keys automatically.

### filter_presets.go

- Presets store a **body**, not a full search:

  ```go
  const (
      mineBody   = "author:@me"
      reviewBody = "review-requested:@me"
  )

  var defaultPresets = []filterPreset{
      {"mine", mineBody},
      {"all", ""},
  }
  ```

- Add composer / splitter / state helpers:

  ```go
  var prStates = []string{"open", "merged", "closed"}

  // searchFor composes a gh search from a state and an optional body qualifier.
  func searchFor(state, body string) string {
      s := "is:" + state
      if body == "" {
          return s
      }
      return s + " " + body
  }

  // splitState strips a leading is:<state> token, returning the state (default
  // "open") and the remaining body. Used by NewModel to seed from the initial filter.
  func splitState(search string) (state, body string) { … }

  func nextState(s string) string { … } // cycles prStates, wrapping
  ```

- `presetIndexFor` now matches on **body** (compares `p.search`/body to the passed
  body), so `splitState` + `presetIndexFor` recover the preset from any resolved
  filter regardless of state.

### Key binding

New `s` key in the list key handler (alongside `f`):

```go
case "s":
    m.state = nextState(m.state)
    m.filter = searchFor(m.state, m.body)
    return m, m.switchToFilter()
```

The other filter mutation sites set `m.body` and recompute `m.filter` the same way:

- `f` (preset cycle): `m.body = defaultPresets[m.presetIdx].search; m.filter = searchFor(m.state, m.body)`.
- author picker `confirmPicker`: `m.body = strings.Join(terms, " "); m.filter = searchFor(m.state, m.body)`.
- `NewModel`: `m.state, m.body = splitState(filter)`, then `m.presetIdx = presetIndexFor(m.body)`.

### State-aware mine view

The mine view fetches two searches; make both state-aware:

- `mineFetchCmd` lists `searchFor(m.state, mineBody)` and `searchFor(m.state, reviewBody)`.
  Its `fetchFailedMsg` error tags must use `searchFor(m.state, mineBody)` too — today
  they are hardcoded to `mineFilter`, so under a non-open state the `fetchFailedMsg`
  guard (`msg.filter != m.filter`) would misclassify the failure as a background
  prewarm, swallow the error, and leave the spinner stuck (`m.refreshing` never clears).
- `mineFetchedMsg` carries the `state` it was fetched under; the handler caches each
  half under `searchFor(msg.state, …)` and only repaints when
  `m.isMineView() && msg.state == m.state` (otherwise it is a background prewarm →
  cache only). This replaces the current `m.filter != mineFilter` guard.
- `hydrate`'s mine branch reads the cache under `searchFor(m.state, mineBody/reviewBody)`.

### Header + help

- Header shows the state: `repo   mine · merged · 12` (drop the hardcoded "open" in
  the count suffix — the count is now state-relative). For a custom author filter
  (`presetIdx == -1`), the label must switch from `m.filter` to `m.body`, else the
  state renders twice (once in the resolved filter string, once in the new state
  segment).
- Add `s  cycle state (open/merged/closed)` to the `?` legend and, if space allows,
  the status bar.

### Prewarm

`Init` prewarms the open state only (`mineFetchCmd` + `fetchCmd("is:open")`), which is
the default view. The first toggle to merged/closed fetches cold; subsequent toggles
back are warm from cache. No change needed.

## Part 2 — Refresh after mutating actions

### Mark which actions mutate

Add `Refresh bool` to `action.Action`. Set `true` in `DefaultPRActions` for:

- `m` Merge (squash)
- `u` Update branch
- `M` Mark ready
- `r` Rerun checks

Copy / open-in-browser / worktree actions leave it `false`.

**Expanded Checks-tab rerun paths.** `rerunSelectedCheck` and `rerunAllFailedChecks`
(`internal/ui/expanded.go`) build `actionStat{…}` literals directly and emit
`actionDoneMsg`, bypassing `runAction`/`runBulk`. They must set `refresh: true` and
`nums: [cursor PR number]` on their `actionStat` too, or rerunning from the Checks tab
won't refresh while `r` from the list would. Route both through a shared helper that
stamps `refresh`/`nums`, or set the fields inline in each.

### Carry affected PRs through the action lifecycle

`actionStat` gains `refresh bool` and `nums []int` (the PR numbers the action touched):

- `runAction`: `nums = [v.Number]`, `refresh = a.Refresh`.
- `runBulk`: `nums` = every PR number in the selection, `refresh = a.Refresh`.

### Refetch on success

Add a cursor-preserving refetch (unlike `switchToFilter`, which resets cursor +
selection):

```go
// refreshCurrent refetches the active view in place, keeping the cursor.
func (m *Model) refreshCurrent() tea.Cmd {
    m.refreshing = true
    fetch := m.fetchCmd(m.filter)
    if m.isMineView() {
        fetch = m.mineFetchCmd()
    }
    return tea.Batch(fetch, m.startSpinner())
}
```

Note: `refreshCurrent` preserves the **cursor** (`setPRs` only clamps it), but the
selection is dropped regardless — both `prsFetchedMsg` and `mineFetchedMsg`
unconditionally `m.sel.clear()` on arrival. That is fine here: bulk actions already
clear the selection in `runBulk`, and after a mutating action a stale selection is
undesirable anyway.

In the `actionDoneMsg` handler, after settling the badge, when `err == nil &&
m.actionStatus.refresh`:

```go
for _, n := range m.actionStatus.nums {
    delete(m.fresh, n) // force the detail/summary to revalidate
}
cmds = append(cmds, m.refreshCurrent())
```

The resulting `prsFetchedMsg` already fans out `detailCmdForCursor` + `prefetchCmd`,
which re-pull the now-unfresh details → the summary reflects the change. A merged /
newly-closed PR drops out of an `is:open` list on the refetch.

### Fold in reviewer assignment

`assignReviewersCmd` already refetches the list but leaves the detail marked fresh, so
reviewer changes don't show in the summary. Clear `m.fresh[number]` before its refetch
so the summary updates too.

## Non-goals

- Mutating actions on already-merged/closed PRs. `gh` will error and the failure badge
  surfaces it; we don't pre-disable actions per state.
- Continuous polling of in-flight CI after a rerun — one refetch on completion, not a
  live tail.

## Assumption to smoke-test

`PRListArgs` (`internal/gh/prs.go`) never passes `--state`; state lives entirely in the
`--search` string, so swapping the token to `is:merged`/`is:closed` is consistent with
the existing contract. This can't be unit-tested here (private repo, no auth), so verify
manually that `gh pr list --search "is:merged author:@me"` returns merged PRs before
relying on it — if `gh` ever injects a default `is:open`, merged views would come back
empty.

## Testing

- `filter_presets_test.go`: `searchFor`, `splitState`, `nextState`, `presetIndexFor`
  (body-keyed) round-trips across all three states.
- `prlist_test.go` / `perf_actions_test.go`: `s` cycles state and re-fetches;
  `mineFetchedMsg` caches under the right per-state keys and only repaints the matching
  view+state.
- Action refresh: a mutating action success clears `m.fresh` for the affected numbers
  and issues a refetch; a non-mutating action (copy) does not; a failed mutating action
  does not refetch.
- Rewrite the existing mine-prewarm guard test (`internal/ui/perf_actions_test.go`,
  the `m.filter != mineFilter` case): the guard becomes `isMineView() && msg.state ==
  m.state`, and `mineFetchedMsg` gains a `state` field. Other filter-string assertions
  (e.g. `picker_test.go`, open-state `perf_actions_test.go` cases) survive unchanged
  because open-state resolved strings are identical.
