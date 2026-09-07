# Plan — cascade `update-branch` up a stacked PR (#118)

Spec: `docs/superpowers/specs/2026-09-07-cascade-update-branch-design.md`
(sections C1–C8). Issue: #118.

All new code lands in one new file, `internal/ui/cascade.go`, plus its test file
`internal/ui/cascade_test.go`; the wiring steps touch four existing files.
Nothing in `internal/gh` or `internal/action` changes — no interface, no query,
no keybinding.

## Design shape

The feature splits into a **pure core** and a **thin tea shell**, so almost
everything is unit-testable without a `tea.Program`:

- **pure** — chain building (C2), the step-decision function (C5/C6), the report
  body (C8). No `tea.Cmd`, no `Model`, no network.
- **shell** — `Update` branches that turn a decision into a `tea.Cmd`, plus the
  overlay render.

The decision function is the crux: `cascadeRun` advances by consuming one
outcome (a mutation result or a probe result) and returning what to do next.
That makes the sequential-with-wait behaviour assertable by feeding a list of
outcomes, with no clock and no sleeping.

---

- [ ] **Step 1: chain building (pure)**

  New `internal/ui/cascade.go`. Types:

  ```go
  // cascadeLink is one PR in a chain plus the merge state it was planned under.
  type cascadeLink struct {
      pr        gh.PR
      mergeable string // resolved at plan time via mergeState
      mss       string
  }

  type cascadeChain []cascadeLink

  type cascadePlan struct {
      chains  []cascadeChain
      dropped []int // held links a walk could not reach: reported "not attempted"
  }

  func (p *cascadePlan) cascades() bool // nil-safe; true iff some chain has len > 1
  func (p *cascadePlan) count() int     // total links in all chains, for the prompt
  ```

  Pure builder, taking the shown PRs in shown order and the seed PRs, plus a
  merge-state resolver so the caller injects `mergeState(p, m.detail[n], ok)`:

  ```go
  func buildCascadePlan(shown, seeds []gh.PR, state func(gh.PR) (mergeable, mss string)) *cascadePlan
  ```

  Rules, all from C2:
  - seeds are used as given (bounds-checked by the caller); a seed is **never**
    filtered on `State`.
  - for a seed with `Stack != nil`, walk `StackPosition+1, +2, …` looking up each
    position among `shown` with the same `Stack.Number`; stop at the first
    position that is absent **or** not `OPEN`.
  - **`dropped` gets every remaining held link of that `Stack.Number` above the
    stop position**, not just the one that stopped the walk. For held positions
    `{1,2,4,5}` seeded at 1 the walk stops at the absent 3, and #4 and #5 are
    held-but-unreachable — they must be reported, or a `u` on a gapped stack
    updates 1→2 and says nothing about 4 and 5 (the silent partial cascade AC 3
    forbids). Links another chain absorbs are excluded from `dropped`, so a
    number never appears both updated and not-attempted.
  - a walk that reaches another seed absorbs it (chains merge by the walk, not by
    `Stack.Number`); a seed no walk reaches starts its own chain.
  - dedupe by `pr.Number` across the whole plan.
  - order chains by `(Stack != nil, Stack.Number)` and links within a chain by
    ascending `StackPosition`, using a **stable** sort so "non-stacked seeds
    first in seed order" (C2) is actually preserved.
  - snapshot `(mergeable, mss)` per link via the injected resolver.

  Tests in `internal/ui/cascade_test.go` (table-driven, matching the repo's
  style): single stack of 4 from the bottom seed; seed mid-stack (only links
  above); top-link seed → one chain of 1, `cascades()` false; gapped `{1,2,4,5}`
  seeded at 1 → chain `1→2` and `dropped == []int{4,5}`; `{1,2,3(MERGED),4,5}`
  seeded at 1 → chain `1→2`, `dropped` containing 4 and 5; the same `{1,2,4,5}`
  holding **seeds at 1 and 4** → two chains `1→2`, `4→5`, `dropped` empty; a
  MERGED **seed** surviving (no filter on seeds); bulk selection spanning a stack
  deduping to one entry per number; two stacks ordering by `Stack.Number` with
  non-stacked seeds first in seed order; `nil` plan `cascades()` false.

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 2: the sequential driver (pure decision function)** (implement: escalated)

  Still in `cascade.go`. Run state:

  ```go
  type cascadeOutcome struct {
      number int
      err    error // nil = updated
  }

  type cascadeRun struct {
      plan        *cascadePlan
      chain, link int // cursor into plan.chains
      probes      int // probes spent on the current wait
      // observed is the (mergeable, mss) a wait saw for the link that is about
      // to mutate, or the zero value when no wait preceded it.
      observedMergeable, observedMSS string
      hasObserved                    bool
      done                           []cascadeOutcome
      skipped                        []int // chain tails after a failure, plus plan.dropped
      // step is the last decision returned, so the test pump can tell a mutate
      // cmd (which it must invoke) from a probe beat (which it must not).
      step cascadeStep
      // stat is the run's own badge state. The run owns it rather than reading
      // m.actionStatus, because seven sites in expanded.go and logview.go
      // overwrite m.actionStatus without passing through runAction/startBulk.
      stat *actionStat
  }
  ```

  Decisions are a small closed set, so the shell holds no policy:

  ```go
  type cascadeStep int
  const (
      cascadeMutate cascadeStep = iota // fire UpdateBranch for the current link
      cascadeProbe                     // schedule one probe beat
      cascadeSettle                    // run is over
  )
  ```

  Three pure methods:

  - `func (r *cascadeRun) start() cascadeStep` — always `cascadeMutate`.

  - `func (r *cascadeRun) onUpdated(err error) cascadeStep` — record the outcome
    for the link that just mutated; on error, move its chain's remaining links
    into `skipped` and advance to the next chain; on success, if a next link
    exists in this chain, decide between `cascadeProbe` and `cascadeMutate` by
    C5's rule against the **effective** pre-update state of the link that just
    mutated:

    ```
    effective = observed (when hasObserved) else link's plan snapshot
    skip the wait iff mergeStateResolved(effective.mss) && effective.mss != "BEHIND"
    ```

    Clear `hasObserved` and reset `probes` **unconditionally at the method's
    exit**, on every branch — not inside the on-success clause. `observed` is
    only ever set on `onProbed`'s proceed branch, where `mss == "BEHIND"` always,
    so a value leaking across a chain boundary always reads "wait": chain 2's
    first link being `CLEAN` (a no-op update whose head never moves) would then
    wait for chain 2's second link to become `BEHIND`, which never happens — 30
    probes and a spurious timeout on a perfectly healthy chain. Clearing at exit
    makes the leak impossible.

    The error and chain-exhaustion branches return `cascadeMutate` for the next
    chain's first link, or `cascadeSettle` when no chain remains.

    **The carry-forward is load-bearing, not bookkeeping.** In a stack of 3+,
    only the bottom link is normally `BEHIND` at plan time — links 2..N are
    `CLEAN`/`BLOCKED` against their own (not-yet-moved) bases. Deciding link 3's
    wait from link 2's *plan-time* snapshot would read `CLEAN`, skip the wait,
    and fire link 3 immediately after link 2's mutation returned: the
    fire-and-forget the task forbids, breaking AC 1 and AC 2 in the ordinary
    case. The wait that preceded link 2's mutation observed it `BEHIND`, and that
    is the value link 3's decision must use.

  - `func (r *cascadeRun) onProbed(mergeable, mss string, err error) cascadeStep`
    — `probes++` **first**, so an errored probe consumes budget. Then:
    - `err != nil`, **or the number was absent from the returned map** → skip the
      conflict/proceed tests entirely, but still apply the budget test below (so
      a run of errored or empty probes ends in a timeout, per C5).
    - `mergeConflicted(mergeable, mss)` → fail the current link with a conflict
      error, move the chain's remainder to `skipped`, advance to the next chain.
    - `mss == "BEHIND" && !mergeUnresolved(mergeable)` → record
      `observed{mergeable, mss}` for the link about to mutate, return
      `cascadeMutate`.
    - `probes >= cascadeWaitProbes` → fail the current link with a timeout error,
      move the chain's remainder to `skipped`, advance.
    - otherwise → `cascadeProbe`.

  The mandatory-first-probe rule (C5) falls out of `onUpdated` returning
  `cascadeProbe` rather than testing the condition itself — the condition is only
  ever evaluated in `onProbed`, i.e. after at least one beat.

  Constants:
  ```go
  const cascadeProbeEvery = 1500 * time.Millisecond
  const cascadeWaitProbes = 30
  ```

  Tests: `(BEHIND, UNKNOWN) ×2` then `(BEHIND, MERGEABLE)` → `probe, probe,
  mutate`, mutation only after the third probe; **a chain of 3 whose links 2 and
  3 are snapshotted `CLEAN` still issues probes before link 3** (the
  carry-forward regression); never-resolving source → timeout after exactly
  `cascadeWaitProbes` probes and no further mutate; already-`(BEHIND,
  MERGEABLE)` next link → still one probe before the mutate; effective `mss`
  resolved and not `BEHIND` → `cascadeMutate` with zero probes; effective `mss`
  `""`/`"UNKNOWN"` → probes issued; conflict → failure, no mutate; errored probes
  consume budget and end in a timeout; failure in chain 1 leaves chain 2 running
  and marks only chain 1's tail skipped.

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 3: the report (pure)**

  In `cascade.go`:

  ```go
  func (r *cascadeRun) errored() bool    // some link actually returned an error
  func (r *cascadeRun) failed() bool     // errored(), or anything skipped
  func (r *cascadeRun) badge() string    // "Stack update failed · 2/4 updated"
  func (r *cascadeRun) report() []string // overlay lines, per C8
  func (r *cascadeRun) updated() []int   // succeeded numbers → actionStat.partial
  ```

  `errored()` and `failed()` are deliberately different. The settle's
  `actionDoneMsg.err` is non-nil **iff `errored()`** — a gapped stack that
  updated everything it attempted and only has `dropped` links settles
  `err == nil`, so it does not show "✗ Stack update failed" for a run that
  failed at nothing. `failed()` is the broader test, and it is what gates the
  overlay: a not-attempted-only run still owes the user a report.

  `badge()`'s denominator is **attempted + dropped**, not `count()` — `count()`
  covers only the links in chains (C2 excludes the dropped tail), so in the
  gapped case `2/2` would hide the two links nobody touched.

  `report()` emits only non-empty groups, in the order updated / failed / not
  attempted, one line per group with `#N` numbers; the failed line carries the
  error text. Tests assert each grouping, group omission when empty, a
  not-attempted-only run (the gapped-stack case, which has no failure but must
  still report), and that a fully-successful run has `failed() == false` and no
  lines.

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 4: model state and settle integration**

  `internal/ui/prlist.go` — `Model` gains both fields **here**, before anything
  references them — declaring them any later leaves this step unbuildable:

  ```go
  cascade        *cascadeRun   // live cascade run; nil when none
  pendingCascade *cascadePlan  // plan awaiting the y/n prompt
  cascadeReport  []string      // settled cascade's per-PR report; nil when none
  ```

  `internal/ui/actions.go` — `actionStat` gains:
  ```go
  partial []int // PRs that succeeded even though the run as a whole failed
  ```

  `internal/ui/messages.go` — `actionDoneMsg` gains `cascade bool`, so the
  cascade's own settle is distinguishable from any other action's. **The three
  run messages are declared here too**, in this step, because Step 5's
  `runCascade` returns a cmd that produces `cascadeUpdatedMsg` and would not
  otherwise compile at its own verify gate:

  ```go
  type cascadeUpdatedMsg struct{ err error }
  type cascadeProbeMsg struct{}
  type cascadeProbedMsg struct {
      number int
      detail gh.PRDetail
      raw    []byte
      err    error
      ok     bool // the number was present in the returned map
  }
  ```

  `cascadeProbedMsg` carries the PR number so the fold can never write a key it
  did not receive.

  `internal/ui/prlist.go`, the `actionDoneMsg` branch (2008):
  - clear `m.cascade = nil` **only when `msg.cascade`**. An unconditional clear
    is a live hazard: `rerun-failed` (`actions.go:163`), `cleanup-branch`
    (`actions.go:158`) and a native-tool clipboard copy (`actions.go:145`) all
    emit `actionDoneMsg` and stay reachable, so a stray `r` during a 45s-per-link
    wait would clear the run and every later beat would hit the nil-guard — the
    stack stops mid-cascade with no overlay, no `partial`, no refresh.
  - add two branches firing only when `msg.err != nil && len(m.actionStatus.partial) > 0`,
    mirroring the existing `err == nil` ones at 2031 and 2043:
    - `refresh` → `delete(m.fresh, n)` per partial number + `m.backgroundRefresh()`
    - `rerunCI` → `m.ciRerun[n] = stamp` per partial number + `delayedRefreshCmd()`

  Nothing else in the handler changes; `applyOptimisticAction` needs no
  `update-branch` case (it has none today, and the row's checks come from the
  refetch).

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 5: dispatch and prompt wiring**

  `internal/ui/actions.go`:
  - **re-entrancy**, at the top of both `runAction` and `startBulk`:
    `if m.cascade != nil && !a.ExitsTUI { return nil }`. This blocks every
    *keymap-dispatched* action that would set `m.actionStatus`, while exits-TUI
    worktree actions stay usable mid-cascade.

    It does **not** cover everything, and the run must not depend on it: seven
    sites assign `m.actionStatus` directly, bypassing both functions —
    `expanded.go:378`, `:383`, `:397` (`r`/`R` on the expanded Checks tab) and
    `logview.go:360`, `:366`, `:378`, `:382` (`o`/`Y` in the log view). Entering
    those views during a run the plan budgets at up to 45s *per link* is
    ordinary, and `openHoveredCheck` even sets `settled: true` with
    `clearStatusCmd()`, so `actionClearMsg` nils `m.actionStatus` 3s later
    (`prlist.go:2052`).

    That is why **`cascadeRun` owns its own `*actionStat`** (Step 2). Badge
    wording and `partial` are written through `r.stat`, never
    `m.actionStatus`, and the settle re-asserts `m.actionStatus = r.stat`
    immediately before emitting `actionDoneMsg{cascade: true}`. Without that,
    three things break on a foreign stat: a nil deref when the badge writes on
    the next beat; a permanent brick if the settle loses the race, since the
    handler's `if m.actionStatus == nil { return … }` (`prlist.go:2010`) returns
    before `m.cascade = nil`; and a refresh that iterates the *foreign* `nums`
    (`prlist.go:2031`), inverting C7. Run ownership removes the whole class
    rather than patching four more call sites.
  - `startBulk` — build the plan when **all three** hold:
    `a.Command.Native == "update-branch"`, `m.section.(*PRSection)` succeeds, and
    **`m.detailSource != nil`**. The detail-source condition is what implements
    C5's nil-source degradation: with no probe mechanism there is no wait, and an
    unwaited cascade is forbidden, so `m.pendingCascade` stays nil and `u` takes
    today's single-update path. (`m.detailSource` is legitimately nil — see
    `nativeMutationFn`'s own guard at `actions.go:252`.) When the guard fails,
    **set `m.pendingCascade = nil`** so no earlier plan can be read by a later
    prompt. Then add `|| m.pendingCascade.cascades()` to the existing confirm
    disjunction.
  - `confirmAnswer` — on `true`, take the cascade path when
    `m.pendingCascade.cascades()`; clear `m.pendingCascade` on both answers.
  - new `func (m *Model) runCascade(a action.Action, p *cascadePlan) tea.Cmd` —
    build `stat := statForBulk(a, p.count())` with `refresh`/`rerunCI`/`nums`
    (the whole plan) as `runBulkNative` does, then set both
    `m.cascade = &cascadeRun{plan: p, stat: stat, skipped: p.dropped}` (which is
    where `plan.dropped` enters `run.skipped`) and `m.actionStatus = stat`, call
    `m.invalidateLaunchCache(nums...)` and `m.sel.clear()`, and return
    `tea.Batch(m.cascadeMutateCmd(), m.startSpinner())`.
  - also new here: `func (m *Model) cascadeMutateCmd() tea.Cmd`, which resolves
    the current link's closure via
    `m.nativeMutationFn("update-branch", link.pr)` — reusing the `p.ID == ""`
    stale-cache guard rather than re-implementing it — and returns
    `func() tea.Msg { return cascadeUpdatedMsg{err: fn()} }`.

  `internal/ui/prlist.go`:
  - the two `m.pending = &a` sites (2194, 2357) also set `m.pendingCascade = nil`.
  - `confirmQuestion` gains a leading branch for the cascade wording
    (`Update branch for N PRs in stack?`).

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 6: the run's tea shell**

  The three run messages are already declared (Step 4).

  `internal/ui/prlist.go` — three `Update` cases, each returning early when
  `m.cascade == nil` (a beat already in flight when a chain failed arrives after
  the settle — C6):
  - `cascadeUpdatedMsg` → `onUpdated(msg.err)` → dispatch the next step.
  - `cascadeProbeMsg` → fire one `FetchDetails([number])` cmd that returns
    `cascadeProbedMsg`, never `fetchFailedMsg` (C5 — it must not blank the board).
  - `cascadeProbedMsg` → **fold the detail only when `msg.err == nil && msg.ok`**,
    then `onProbed` → dispatch. An unconditional fold would write
    `m.detail[n] = gh.PRDetail{}` and `m.fresh[n] = true` on an errored probe,
    blanking that PR's cached mergeability and suppressing its refetch — the
    opposite of C5's "otherwise ignored". The successful fold mirrors the
    `detailsBatchMsg` branch (1893) for `m.detail`/`m.fresh`/`m.cache`.

  A `cascadeSettle` decision sets `run.stat.partial = run.updated()`, re-asserts
  `m.actionStatus = run.stat` (in case a foreign action replaced or nilled it —
  Step 5), sets `m.cascadeReport = run.report()` when `run.failed()`, and emits
  `actionDoneMsg{cascade: true, fail: run.badge()}` with `err` non-nil **iff
  `run.errored()`** — while `m.cascade` is still live, so Step 4's handler clears
  it after reading it.

  The beat is `tea.Tick(cascadeProbeEvery, …)`; per-step badge wording goes on
  `run.stat.run`, never `m.actionStatus.run`.

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 7: the report overlay**

  `internal/ui/cascade.go` — `func (m Model) cascadePanel() string`, built with
  `titledBox`/`titledBoxTinted` like `confirmPanel` (`prlist.go:2611`).

  `internal/ui/prlist.go`:
  - `renderInner`'s overlay switch (2460) gains a **last** case:
    `case len(m.cascadeReport) > 0: return overlayTop(board, m.cascadePanel(), …)`
    — after `pending`/`picker`/`legend`/`actions`, so a surface the user opened
    always wins.
  - the board's `tea.KeyMsg` case: when `m.cascadeReport` is non-empty, any key
    clears it and returns. It sits **after** the `m.pending` check (2076) and
    **after** the `m.filtering` check (2082) — a run settling while the filter
    input is focused must not eat the next keystroke.
  - `previewMouseBounds` (2405) gains `len(m.cascadeReport) > 0` to its
    obscured-overlay list.

  Known and accepted: `renderInner` returns early for `logView`, `expanded`, and
  the omni suggestion dropdown (`prlist.go:2441`, `2448`, `2457`), so the report
  waits rather than showing while any of those is open — C8 names the first two
  and the third behaves identically.

  Verify: `go build ./... && go test ./internal/ui`.

- [ ] **Step 8: driver tests against fakes**

  `internal/ui/cascade_test.go`. The existing helpers cannot drive this machine:
  `driveBulk` (`mutationsource_test.go:73`) invokes a batch's sub-cmds and
  returns the first `actionDoneMsg`, never feeding a message back into
  `m.Update`, so the run would never advance past its first step. And the beat is
  a real `tea.Tick(1500ms, …)`, so any helper that blindly invokes the returned
  cmd sleeps through every probe.

  So specify the pump:

  ```go
  // driveCascade runs a cascade to settle by pumping messages through Update.
  // It invokes ordinary cmds, but NEVER invokes the probe beat's tea.Tick —
  // it synthesizes cascadeProbeMsg{} instead, so a 30-probe timeout test runs
  // instantly instead of sleeping 45s.
  func driveCascade(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Msg)
  ```

  A `tea.Cmd` is an opaque `func() tea.Msg`, so the pump cannot inspect one to
  tell a mutate cmd (which it **must** invoke, since the test asserts
  `fakeMutationSource.updateBranchCalls`) from a beat (which it must not). The
  discriminator is `cascadeRun.step` (Step 2): after each `Update` the pump reads
  `m.cascade.step`, and when it is `cascadeProbe` it **discards** the returned
  cmd and feeds `cascadeProbeMsg{}` instead. Otherwise it invokes the cmd
  normally. The tick cmd is deliberately never called in tests.

  A sequenced detail fake, since `fakeDetailSource` (`detailbatch_test.go:12`)
  returns a fixed map every call and never errors:

  ```go
  // seqDetailSource returns queued details per call, so a test can script a
  // mergeability transition the way GitHub actually serves one. The last entry
  // repeats once exhausted.
  type seqDetailSource struct {
      got  [][]int
      ret  []map[int]gh.PRDetail
      errs []error // per-call error, for the errored-probe case
  }
  ```

  It must satisfy `gh.DetailSource` in full —
  `FetchDetails([]int) (map[int]gh.PRDetail, map[int][]byte, error)` — so it
  synthesizes the raws map the same way `fakeDetailSource` does
  (`detailbatch_test.go:17`).

  Cases: a 3-link stack driven end to end fires `UpdateBranch` in
  `StackPosition` order, one per link, **each after its wait** (the finding-1
  regression, at model level); a mid-chain failure stops that chain, settles with
  `partial` set to the succeeded numbers, and the `actionDoneMsg` handler then
  issues the refresh + `ciRerun` stamps; `m.cascade == nil` asserted directly
  after the settle; a stale `cascadeProbeMsg` after settle is a no-op; a nil
  `DetailSource` leaves `m.pendingCascade` nil so `u` takes today's
  single-update path; `u` on a non-stacked PR byte-identical to today (assert
  against the existing expectations at `perf_actions_test.go:202` and `:299`); an
  errored probe consuming budget without touching `m.detail`/`m.fresh`; and a
  press on the **merged board** where one of two non-stacked PRs fails, settling
  with today's `N of M failed` badge (AC 5's second half, which regression tests
  alone do not pin).

  One table test over the per-selected keys — `L`, `A`, `M`, `o`, `W` — on a
  stacked PR asserting none of them prompts an update-branch question or
  cascades (AC 6), plus one on an `IssueSection` exercising the
  `m.section.(*PRSection)` half of C1.

  Verify: `go test ./internal/ui`.

- [ ] **Step 9: the gate**

  In order, capturing real output for the PR body:
  `go build ./...`, `go vet ./...`, `go test ./...`,
  `golangci-lint run` (whole-repo — this repo's canonical invocation, and a
  scoped run would lose the cross-package type info its analyzers need),
  `treefmt`.

- [ ] **Step 10: docs move**

  Move `SPEC.md` to
  `docs/superpowers/specs/2026-09-07-cascade-update-branch-design.md`, matching
  the repo's `docs/superpowers/{specs,plans}` convention, and update this plan's
  spec reference. Kept separate from Step 9 so the gate output quoted in the PR
  body is captured against the final tree, not invalidated by a later move.

  Verify: `go build ./... && go test ./...` (a docs move must not touch code, and
  this proves it).

## Out of scope (from the spec's non-goals)

No `MutationSource`/`DetailSource` change; no cascading downward; no bridging a
gap in held positions; no change to `merge`/`approve`/`auto-merge`; no new
keybinding; no cascade from a user-reconfigured `Scope: "single"` `u`.

