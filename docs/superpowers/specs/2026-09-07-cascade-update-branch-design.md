# Spec — cascade `update-branch` up a stacked PR (#118)

Revision 3. Revision 1 drew ten blocking findings, revision 2 seven more; both
rounds are folded in. Two of revision 2's findings collapsed design, rather than
adding to it: the wait is now one condition instead of two phases, and the
`prs`-vs-`shown` question turned out not to exist.

## Problem

`u` (`action.DefaultPRActions()["u"]`, `Scope: "per-selected"`, `Confirm: false`
— `internal/action/defaults.go:31`) fires exactly one
`MutationSource.UpdateBranch(p.ID)` per selected row
(`internal/ui/actions.go:301`, inside `nativeMutationFn`, driven by
`runBulkNative`). On a GitHub stack of N, updating link *k* moves link *k*'s
head, which is link *k+1*'s base — so every PR above *k* is behind again and the
user must press `u` on each, in order, waiting for GitHub to recompute
mergeability between presses.

The stack is already on the model (issue #89, landed in #104):
`gh.PR.Stack *PRStack` and `gh.PR.StackPosition` at `internal/gh/prs.go:115`,
rendered by `PRSection.setShownStacks`. The action ignores it.

## Goal

`u` on a PR that belongs to a stack updates that PR and every PR above it in
the stack, sequentially, each step waiting for GitHub to notice the moved base
and finish recomputing the next link before firing, stopping that chain on its
first failure and reporting per-PR what updated and what did not — behind a
confirmation prompt stating the count.

## Model

The run is a list of **chains**. A chain is a contiguous ordered run of PRs
where each link's base is the previous link's head; a standalone (non-stacked)
PR is a chain of length 1. Sequencing and the between-link wait apply **within**
a chain; a chain's failure stops **that chain only**.

One construct settles several findings: non-stacked bulk `u` stays as it is
today (N independent length-1 chains, all attempted, failures don't stop
siblings), the cascade trigger becomes a property of the work set rather than of
"did expansion grow it", and the report gets a natural grouping.

### C1 — when a plan is built at all

A cascade plan is built **only** when both hold:

- `a.Command.Native == "update-branch"`, and
- `m.section` is a `*PRSection`.

Otherwise `m.pendingCascade` is nil and every existing path behaves exactly as
today. `startBulk` (`internal/ui/actions.go:465`) is the single entry for *every*
`Scope: "per-selected"` action — `o`, `W`, `L` (approve, which already carries
`Confirm: true`), `A`, `M` — so an unscoped plan would make `L` on a stacked PR
render an "update branch" prompt and then run the cascade instead of the
approve. `W` and `o` also reach `startBulk` on the issue board, where the
`*PRSection` assertion fails.

### C2 — building a chain

Seeds are `m.selectedOrCursor()` mapped through `PRSection.prAt` — snapshotted
as `gh.PR` **values**, never indices, so a `backgroundRefresh` landing mid-run
cannot re-target the plan.

For a seed with `Stack != nil`, its chain is built by **walking
`StackPosition` upward from the seed one position at a time, stopping at the
first position that is absent from the board or not `OPEN`.** Contiguity is
required, not assumed: the chain's defining property is that each link's base is
the previous link's head, and a gap breaks that edge. Held positions
`{1,2,4,5}` are chain `1→2` plus an unreachable tail, not chain `1→2→4→5` —
updating #4 across the gap would merge an un-updated #3.

Gaps are ordinary, not exotic: `PRSection.prs` is scoped by the list fetch
(`defaultLimit` 20 / `openListLimit` 100 plus whatever omni query is active),
`stackParentNumber` already notes "a merged predecessor is absent from an open
board" (`internal/ui/section.go:136`), and `setShownStacks` computes
`missing := root.Stack.Size - len(members)` (`section.go:472`).

The truncated tail is reported in the **not attempted** group and is excluded
from the prompt's count — the prompt promises exactly what the run will try.

**The `OPEN` filter applies to walked links only, never to a seed.**
`nativeMutationFn`'s `update-branch` case is the one native marker with no state
pre-check (contrast `actions.go:244`, `268`, `276`, `284`, `292`, `304`), so
today `u` on a merged PR surfaces GitHub's rejection; filtering a seed out would
hit `runBulkNative`'s `if len(calls) == 0 { return nil }` (`actions.go:578`) and
make it a silent no-op — and would break the whole merged/closed board.

**Dedupe by `PR.Number`**, not `PR.ID`: the node id is `""` on a stale
gh-CLI-era cache entry (the case `actions.go:233` already guards), and keying on
`""` would collapse distinct PRs into one bucket. `Number` is unique per repo and
always populated.

**Chains merge by the walk, not by stack identity.** A higher seed joins a lower
seed's chain only when that chain's walk actually reaches it; otherwise it starts
its own chain at its own position. "Same `Stack.Number` ⇒ same chain" would
contradict the truncation rule: with held positions `{1,2,4,5}` and seeds at 1
and 4, seed 4 belongs to a chain that stopped below it, so it would be demoted to
"not attempted" — silently dropping an explicitly selected PR to zero
`UpdateBranch` calls, which narrows AC 4 and violates C2's own rule that
eligibility filtering never applies to a seed. Under the walk rule that case is
two chains: `1→2` and `4→5`. Every PR still appears in exactly one chain, since
a walk that reaches a seed absorbs it.

Seed indices are bounds-checked before `prAt`, mirroring `runBulkNative`
(`actions.go:552`): `prAt` indexes `s.shown` unguarded (`section.go:134`).

**Ordering is total, on one axis:** sort chains by
`(Stack != nil, Stack.Number)` — non-stacked seeds first in seed order, then
stack chains by ascending `Stack.Number` — and links within a chain by ascending
`StackPosition`.

### C3 — scope: the shown set

Expansion reads the **shown** set, via `prAt`. Revision 2 argued for the full
held set on the grounds that a filter could hide a link mid-chain; that case does
not exist. `setShownOrdered` runs `expandStackMembers` on every shown-set
computation — "a stack is a display unit, so matching any link shows every link
the section holds" (`internal/ui/section.go:297`, `323`) — `applyFilter` is its
only producer (`prlist.go:833`), and `hideDrafts` explicitly exempts stack
members (`section.go:300`). A seed is by definition at a shown index, so its
stack matched, so every held link of that stack is already shown.

So `prs` and `shown` coincide here, there is no convention to override
(`prlist.go:2089` — "actions work on the filtered set" — stands), and the prompt
needs no PR enumeration or hidden-link marker.

Links `Stack.Size` says exist but the board does not hold are out of scope: the
board already paints them as a dim `⧉+N` marker, and C2's walk stops at the
first one.

### C4 — when it prompts

`u` carries `Confirm: false`, so `startBulk`'s existing gate never trips for it.
It gains one disjunct:

```
if a.Confirm || overThreshold || m.needsOthersConfirm(a) || m.pendingCascade.cascades() { … }
```

`startBulk` builds the plan once (under C1's guard) and stores it as
`m.pendingCascade *cascadePlan`. `cascades()` is true when any chain has length
> 1, and false on a nil receiver.

- `confirmQuestion` gains a leading branch: when `m.pendingCascade` cascades, it
  renders `Update branch for N PRs in stack?` instead of `"%s for %d PRs?"`.
  One gate, one prompt — no double prompt even if a user sets `Confirm: true` on
  `u`.
- `confirmAnswer(true)` executes the **stored** plan, never a re-expansion.
- `confirmAnswer(false)`, and the two other `m.pending = &a` sites
  (`prlist.go:2194`, `prlist.go:2357`, both the `Scope != "per-selected"` path),
  set `m.pendingCascade = nil`. That is the field's whole lifetime: written by
  `startBulk`, consumed or cleared by `confirmAnswer`, never read while
  `m.pending == nil`. Every confirm-prompt exit routes through `confirmAnswer`
  (`prlist.go:2076`), so there is no escaping path.

When no chain has length > 1 the plan is discarded and the press routes to
`runBulkNative` exactly as today. That covers a single non-stacked PR, a bulk
selection of non-stacked PRs, and a seed that is the top link of its stack.

**Stated limitation:** the cascade is reachable only through `startBulk`
(`Scope: "per-selected"`, the packaged default). A user who reconfigures `u` to
`Scope: "single"` routes through `runAction`/`singleNativeCmd` and gets today's
single update with no prompt — a deliberate opt-out, since wiring the same gate
into four dispatch sites buys nothing for the packaged keymap.

### C5 — the between-link wait

Revision 1 polled "until the next link's `Mergeable` resolves", which is a
no-op: a link's pre-update state is normally already resolved
(`mergeStateResolved` is `v != "" && v != "UNKNOWN"`, `preview.go:550`), so the
first probe returns "resolved" on stale data. Revision 2's two-phase version
still could not tell "GitHub hasn't started recomputing" from "there is nothing
to recompute", and resolved the ambiguity by proceeding — a swallowed timeout.

The predicate that removes the ambiguity is **link *i*'s own pre-update
`MergeStateStatus`**, which both `gh.PR` (`prs.go:91`) and `gh.PRDetail`
(`prview.go:40`) carry:

- **link *i*'s `mss` is resolved and not `BEHIND`** → updating it is a no-op,
  its head does not move, link *i+1* is never invalidated → **skip the wait
  entirely**. Nothing to wait for, and no timeout to swallow.
- **link *i*'s `mss` is `BEHIND`, or is unresolved** (`""` / `"UNKNOWN"`) → its
  head may well move, so link *i+1* may well be invalidated. **Wait.**
  Unresolved must not take the skip branch: `mergeState` returns
  `p.MergeStateStatus` verbatim when no resolved detail is cached
  (`preview.go:560`), and `"UNKNOWN" != "BEHIND"` — so a naive `== "BEHIND"`
  test would skip every wait in the chain in exactly the case the feature exists
  for (pressing `u` moments after the base moved, before GitHub has computed
  anything), turning the whole cascade into the fire-and-forget the task
  forbids.

Every link's pre-update `(mergeable, mss)` is snapshotted **at plan time** — the
walk in C2 already reads every link, so this costs nothing — using
`mergeState(p, m.detail[p.Number], hasDetail)`, the same resolution the board and
`mergePreCheck` use. Later links additionally carry forward whatever their wait
observed.

That collapses the wait to **one condition**. After `UpdateBranch(link[i])`
succeeds, if a `link[i+1]` remains and link *i* was `BEHIND`-or-unresolved,
probe `DetailSource.FetchDetails([link[i+1].Number])` on a beat until:

- `mergeConflicted(mergeable, mss)` → link *i+1* fails with a conflict error and
  the chain stops. Checked first, since a conflict is a resolved terminal answer
  and no mutation GitHub would reject is sent.
- `mss == "BEHIND" && !mergeUnresolved(mergeable)` → GitHub has noticed the
  moved base *and* finished recomputing. Proceed.
- probes exhausted → link *i+1* fails with a timeout error, chain stops.

**At least one probe is always issued before proceeding.** The condition is
level-triggered, and link *i+1* can already be `(BEHIND, MERGEABLE)` before the
run starts — a user who pressed `u` on link *i* alone yesterday leaves exactly
that state — so without a mandatory first beat the very first probe could pass on
stale data and fire *i+1* with no evidence GitHub processed *i*'s head move.

An edge-triggered alternative (require an observed `mergeUnresolved` probe
first) was rejected: GitHub's recompute window is often shorter than the beat,
so a healthy cascade would routinely miss it and time out. Failing runs that
would have succeeded is worse than a bounded settle.

The mandatory beat is safe to be *only* a beat because correctness of the result
never depends on the wait: `UpdateBranch(link[i])` returning means *i*'s head ref
has already advanced server-side, so *i+1*'s mutation is well-defined against the
new base either way. The wait exists so GitHub does not **reject** the mutation
mid-recompute — which is why exhaustion is a failure and a conflict is a failure,
but a settled `(BEHIND, resolved)` reading after a real beat is enough to go on.

**Bounds are probe counts, not wall-clock deadlines**, so a test drives them by
feeding N messages with no sleeping and no injected clock:

| constant | value | at the beat |
| --- | --- | --- |
| `cascadeProbeEvery` | `1500 * time.Millisecond` | the beat |
| `cascadeWaitProbes` | 30 | ~45s before a link times out |

- **A probe that errors** consumes one probe of the budget and is otherwise
  ignored. It never sets `m.err` — the existing `batchDetailCmd` path turns a
  fetch error into `fetchFailedMsg`, which blanks the board when the list is
  empty (`prlist.go:2556`), plainly wrong mid-run. Budget exhausted by errors
  reports the same timeout failure.
- **A nil `DetailSource`** means there is no wait mechanism, so `cascades()` is
  false and `u` degrades to today's single update. An unwaited cascade is the
  fire-and-forget the task forbids, so it is not offered.

Each probe's detail is folded into `m.detail`/`m.fresh`/the on-disk cache the
same way `detailsBatchMsg` does (`prlist.go:1893`) — the probe already paid for
the fetch, so the board and preview get fresh mergeability instead of waiting on
the post-run refetch.

**On "reuse rather than a second polling mechanism":** the wait reuses
`mergeUnresolved`, `mergeConflicted`, `mergeState` and `DetailSource`, but its
beat is genuinely new. The existing checks poll polls *check rollups*, on a
tiered idle schedule, for every running row (`checksPollMsg`,
`prlist.go:1967`) — it neither fetches mergeability nor correlates with a run,
and driving a serial cascade off a shared idle-tiered board poll would be worse
than a purpose-built beat. Stating that plainly rather than claiming a reuse
that isn't performed.

### C6 — non-blocking, progress-legible, re-entrancy

Every stage is a message; nothing sleeps inside a `tea.Cmd`. A `tea.Cmd`
goroutine outlives `tea.Quit`, so a sleep-loop wait would keep mutating GitHub
after the user quit and would freeze the badge for the whole wait. Beats use
`tea.Tick`, mirroring the `checksPollTick` shape.

Each link's mutation closure is obtained from
`nativeMutationFn("update-branch", p)`, not by calling
`mutationSource.UpdateBranch` directly, so the `p.ID == ""` stale-cache guard
and its message are reused rather than re-implemented.

State lives in `m.cascade *cascadeRun`: the plan, the chain/link cursor,
per-link outcomes, the probe counter, and link *i*'s carried-forward
`MergeStateStatus`.

| message | carries | `Update` does |
| --- | --- | --- |
| `cascadeUpdatedMsg` | `err` | record the outcome; start the wait, skip it, advance, or end the chain |
| `cascadeProbeMsg` | — | the beat: fire one `FetchDetails` |
| `cascadeProbedMsg` | `detail`, `raw`, `err` | fold the detail in; evaluate C5's condition; advance, keep waiting, or fail the link |

The badge names the current step — `⣾ Updating stack 2/4 · #103…` while
mutating, `⣾ Waiting on #104 · 3/30…` while probing. The run starts the spinner
with `m.startSpinner()` in the same `tea.Batch` shape `runBulkNative` uses
(`actions.go:611`); the loop then sustains itself, since `actionRunning()` stays
true until settle (`prlist.go:1945`). No *new* spinner is introduced, but it must
be started explicitly — a minutes-long cascade with a frozen glyph would read as
a hang.

**Beats must no-op on a nil `m.cascade`.** A `tea.Tick` already in flight when a
chain fails arrives after the settle branch clears the run, so
`cascadeProbeMsg`/`cascadeProbedMsg` return early rather than dereferencing nil.

**Lifetime of `m.cascade`:** set when the run starts, **cleared in the
`actionDoneMsg` settle branch**. Without that the re-entrancy rule below would
brick every native mutation for the rest of the session.

**Re-entrancy:** while `m.cascade != nil`, a keypress that would start a native
mutation is a no-op. The check runs *before* any plan is built and before
`m.pending`/`m.pendingCascade` is written. Scoped to a live cascade, so no other
action's dispatch changes. Two interleaved cascades would break the sequencing
constraint outright. `pollBusy` (`prlist.go:1441`) needs no change: it already
includes `m.actionRunning()`, which is true for the whole run.

**Starting a cascade clears `m.sel`**, exactly as `runBulkNative` does ("the
batch op consumes the selection", `actions.go:589`). Otherwise the selection
survives as stale row indices into a `shown` set the post-run refresh is about
to reorder, and the next `u` targets whatever rows now sit at those indices.

**Quit mid-cascade:** the beats stop with the program and no further mutation is
issued. Whatever landed on GitHub stays landed; there is no local state to
unwind.

### C7 — settling, refresh, and partial failure

The run ends in a single `actionDoneMsg`, so everything already wired to it
keeps working. But all three post-action branches in that handler are gated on
`msg.err == nil` (`prlist.go:2022`, `2031`, `2043`), which is wrong for a partial
cascade: links that *did* get a merge commit pushed would get no `fresh`
invalidation, no `backgroundRefresh`, and no `ciRerun` stamp, so their rows would
show pre-update checks indefinitely — and `u` does set `rerunCI`
(`rerunsCI`, `actions.go:384`).

`actionStat` gains one field:

```go
partial []int // PRs that succeeded even though the run as a whole failed
```

and the handler gains two branches firing only when `msg.err != nil &&
len(partial) > 0`: the `refresh` invalidation + `backgroundRefresh`, and the
`rerunCI` stamps + `delayedRefreshCmd`. `partial` is nil for every other action,
so nothing else changes. `applyOptimisticAction` has no `update-branch` case, so
it needs none.

`actionStatus.nums` is set to the whole plan up front, matching the existing
idiom (`nums` assigned at `actions.go:584`, driving
`invalidateLaunchCache` at `587`); `partial` carries the succeeded set back at
settle time.

### C8 — reporting

The settled badge cannot carry a per-PR report: `statusBadge` renders
`truncate(s.fail, max(0, avail-6))` (`prlist.go:2727`) and `truncate` keeps the
**head** (`section.go:1072`), so any tail — precisely the updated and
not-attempted lists — is what gets cut, and `clearStatusCmd` wipes it after 3s.

Revision 2 put the report in `statusBar()`. That surface does not exist in the
default layout: `board()` reaches `statusBar` in only two of its five paths, and
the common wide+tall geometry (`ShowSide && ShowPanel`) returns `renderDocked`
(`prlist.go:2566`), which stacks bar/list/panel and never calls it
(`preview.go:605`). `previewMax` returns without a footer, `!ShowFooter` drops
it, and `ShowPanel` replaces it with `keysActionsPanel`.

So the report is split:

- **Badge** — width-independent summary, no per-PR list:
  `✗ Stack update failed · 2/4 updated`. On full success, the existing wording
  stands: `statForBulk` suffixes `a.Past`, giving
  `Branch updated — checks re-running ×4`.
- **Overlay** — a dismissible panel, shown only when a link failed or was not
  attempted. It goes in `render()`'s overlay switch beside `confirmPanel`,
  `pickerView`, `legendView` and `actionsPanel` (`prlist.go:2461`), built with
  the same `titledBox` + `overlayTop` machinery. An overlay is rendered over
  `board()` in **every** layout branch, which is exactly the property
  `statusBar` lacks; it is multi-line, so nothing is truncated; and it persists
  until the user dismisses it with a key, which is the "a half-cascaded stack
  must not be silent" guarantee.

  ```
  ╭─ Stack update ───────────────────╮
  │ updated       #101  #102         │
  │ failed        #103 — has conflicts│
  │ not attempted #104               │
  ╰──────────────────────────────────╯
  ```

  Groups with no members are omitted; multiple chains each get a segment. The
  panel joins `previewMouseBounds`' obscured-overlay list (`prlist.go:2407`) so
  a wheel gesture doesn't scroll the preview underneath it.

  **Precedence and dismissal:** the report sits **last** in the overlay switch —
  after `pending`, `showPicker`, `showLegend`, `showActions` — so a prompt or
  picker the user opened always wins. It is dismissed by any key, handled in the
  board's `tea.KeyMsg` case before the other bindings, the same shape as the
  confirm prompt's own exit (`prlist.go:2076`). `logView` and `expanded` return
  before the switch (`prlist.go:2441`, `2448`), so a run settling inside those
  views shows only the badge — which they do render (`expanded.go:527`,
  `logview.go:429`) — and the report is waiting when the user returns to the
  board. That is deliberate: the report outlives the view it was born in rather
  than being dropped by it.

The report body is a pure function of the run's outcomes, so it is unit-testable
independent of rendering. A single-link run (no cascade) is untouched: today's
verbatim-error badge, no overlay.

## Non-goals

- **Changing `MutationSource` or `DetailSource`.** C5 establishes that
  mergeability is the signal, and both interfaces already expose it.
- **Cascading down.** Only PRs above the seed, per the issue.
- **Links `Stack.Size` implies but the board does not hold** (C3).
- **Bridging a gap in held stack positions** (C2) — the tail is reported, not
  updated.
- **Any other action.** `merge`, `approve`, `auto-merge` keep their current
  per-selection semantics; C1 is what guarantees it.
- **A separate keybinding.** Settled in the task doc as default-cascade.
- **Cascade from a user-reconfigured `Scope: "single"` `u`** (C4).

## Acceptance criteria

1. `u` on a stacked PR calls `UpdateBranch` for that PR and every contiguous
   open link above it, in ascending `StackPosition` order, one at a time.
2. When link *i* was `BEHIND` **or unresolved**, no `UpdateBranch` fires for
   link *i+1* until a probe observes
   `mss == "BEHIND" && !mergeUnresolved(mergeable)`:
   - `(BEHIND, UNKNOWN) ×2` then `(BEHIND, MERGEABLE)` → exactly one mutation,
     after the third probe;
   - a source that never reaches the condition → timeout failure, **no**
     mutation;
   - link *i+1* already `(BEHIND, MERGEABLE)` → still exactly one probe before
     the mutation, never zero;
   - a seed snapshotted `(UNKNOWN, UNKNOWN)` → probes are issued, not skipped.

   Only a link *i* whose `mss` is **resolved and not `BEHIND`** issues no probe.
3. A failure mid-chain stops that chain; other chains and standalone seeds still
   run. The overlay names updated, failed, and not-attempted PRs.
4. A bulk selection spanning a stack calls `UpdateBranch` exactly once per PR.
5. `u` on a non-stacked PR — single, bulk, and on the merged/closed board —
   behaves exactly as today, shows no new prompt, and continues past a failure
   with today's `N of M failed` badge.
6. `L` (approve) on a stacked PR shows today's approve prompt and approves; it
   does not render an update-branch prompt or cascade. Same for `A`, `M`, `o`,
   `W`, and for `W`/`o` on the issue board.
7. Unit tests cover: chain ordering across multiple stacks, dedupe, a gapped
   stack truncating at the gap, **two seeds straddling a gap forming two
   chains**, the `OPEN` filter on walked links only, the wait (proceed,
   mandatory-first-probe, skip-when-resolved-and-not-behind, wait-when-
   unresolved, conflict, timeout, errored probe), a nil `DetailSource` degrading
   to a single update, stop-on-failure confined to one chain, the report body,
   the partial-failure refresh path, and `m.cascade` clearing at settle.
8. `go build ./...`, `go vet ./...`, `go test ./...`, `golangci-lint run`,
   `treefmt` all pass; their real output goes in the PR body.

## Escalated

Two deliberate deviations from the task doc, surfaced rather than left for
review to discover. Both are the requester's call to accept or reverse.

1. **The checks-poll half of the reuse constraint is declined.** The task asks
   to "reuse the existing `mergeUnresolved` / checks-polling machinery rather
   than inventing a second polling mechanism". The `mergeUnresolved` /
   `mergeState` / `mergeConflicted` / `DetailSource` half is reused. The
   checks-poll half is not: `checksPollMsg` (`prlist.go:1955`) fetches *check
   rollups* for every running row on an idle-tiered schedule, never fetches
   mergeability, and does not correlate with a run — driving a serial cascade
   off a shared idle-tiered board poll would be worse than a purpose-built
   beat. C5 states this in place rather than claiming a reuse it doesn't
   perform.

2. **A top-link seed prompts nothing.** The task says plain `u` on a PR "that is
   part of a stack" cascades behind a confirmation prompt. When the seed is the
   *highest* link, there is nothing above it to cascade to, so C4 routes it to
   today's single update with no prompt. This is a narrowing of the settled
   decision: strictly read, such a PR *is* part of a stack. The reading taken is
   that a one-PR "cascade" prompt asking to "update 1 PR in stack" is noise, and
   AC 5's "no new prompt for an unchanged single update" is the better
   invariant.
