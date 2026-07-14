# Context-aware Enter + in-app check log viewer

Issue: [#30](https://github.com/noamsto/prdash/issues/30)

## Problem

Enter unconditionally runs the `"enter"` action (`wt switch pr:{N}` / `wt switch -c {branch}`)
in both the list view and the expanded view, regardless of what is focused. It
should instead drill into whatever is focused — and the Checks tab, where a
specific check is highlighted, has a more useful target than a worktree: the
check's logs.

## Guiding rule

**Enter drills into the focused entity.**

- PR-focused (list view; Conversation / Reviews / Diff tabs) → open worktree. The
  focused entity is the whole PR, and a worktree is the deepest useful drill-in.
- Check-focused (Checks tab) → open the highlighted check's logs.

## Action retargeting on the Checks tab

When the Checks tab is active and a check is highlighted, the PR-level actions
retarget to that check:

| Key | PR-focused (unchanged) | Checks tab (check highlighted) |
|-----|------------------------|--------------------------------|
| `Enter` | open worktree | open the in-app log viewer for the check |
| `o` | `gh pr view --web` | open the check's `DetailsUrl` in the browser |
| `Y` | copy PR URL | copy the check's `DetailsUrl` |
| `r` | rerun failed checks | rerun the highlighted check (existing `rerunHoveredCheck`) |

`R` (rerun all failed) is unchanged. `J`/`K` (prev/next PR), `h`/`l` (tabs),
`j`/`k` (check cursor) are unchanged.

## In-app log viewer

A sub-view layered over the Checks tab. `esc` returns to the Checks tab.

### Fetch & cache

- Async `tea.Cmd` running `gh run view --job <JobID> --log-failed`, with the
  spinner (`startSpinner()`) while loading.
- Parsed result cached per `JobID` in the model. No auto-refresh; re-entering a
  cached job renders instantly.

### Content

- Default: failed steps only (`--log-failed`). Passing checks have no failed
  steps, so they render the full log directly.
- `a` toggles to the full job log (`--log`) and back, re-fetching as needed
  (each variant cached separately per job).

### Parsing

`gh run view --log[-failed]` emits tab-separated lines of the form
`job⇥step⇥timestamp content`. Group consecutive lines by step name into a step
block (header + lines). Each rendered line retains its owning step, so copy can
target the step without a separate cursor.

### Coloring (theme-matched)

- Timestamps: grey/dim.
- Step headers: dim for passing steps, red (and marked) for the failing step.
- Content lines: red for `error`/`FAIL` markers, green for pass markers,
  default otherwise.
- The failing step is visually highlighted.

### Navigation & copy (single line-cursor model)

One cursor moves line-by-line (`j`/`k`, plus up/down). Every line knows its
step, so all three copy targets derive from that one cursor — no mode toggle:

- `y` → copy the focused line
- `s` → copy the step the focused line belongs to
- `Y` → copy the whole visible log (all currently loaded steps)
- `a` → toggle failed-only ↔ full log
- `esc` → back to the Checks tab
- `q` / `ctrl+c` → quit

## Edge cases

- **External checks** (StatusContext, no `JobID`): no `gh` logs available. Enter
  and `o` fall back to opening `DetailsUrl` in the browser with a brief notice;
  `Y` copies `DetailsUrl`.
- **Pending / running checks:** render whatever `gh` returns; empty output shows
  a "no logs yet" notice rather than an empty pane.
- **Worktree from the Checks tab:** Enter no longer opens a worktree there.
  Worktree stays reachable from the list view, the other three tabs, and `W`
  (bulk). Footer hints on the Checks tab reflect the retargeted keys.

## Model state (new)

Mirrors the existing `expanded` / `checkCursor` pattern:

- `logView bool` — whether the log sub-view is active
- `logSteps []logStep` — parsed, per-step log content for the current job
- `logCursor int` — line cursor within the flattened log
- `logShowAll bool` — full log vs failed-only
- loading / error fields for the async fetch

## Opening a raw URL

The existing `o` uses `gh pr view --web`. A check carries a raw `DetailsUrl`, so
`o`/Enter on an external check needs an "open this URL in the browser" path
(OS opener or a `gh browse`-style invocation). This is new plumbing but small.

## Out of scope

- Collapsible/foldable step navigation (chosen against in favor of the flat
  line-cursor model).
- Diff-tab file drill-down.
- Auto-refresh of logs while a check is still running.
