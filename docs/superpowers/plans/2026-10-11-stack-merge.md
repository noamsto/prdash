# Plan — merge a GitHub stack up to the cursor PR (#169)

Spec: `docs/superpowers/specs/2026-10-11-stack-merge-design.md` (S1–S7).
Orchestration consult: not tripped — four packages, but every seam mirrors an
existing one (REST helper in `actions_rest.go`, run/probe shape in
`cascade.go`, confirm flow in `actions.go`), so an opus plan is sufficient.

Gate commands (scoped; CI runs the full `nix build -L
.#checks.x86_64-linux.go-gate`):

```
go build ./... && go vet ./internal/gh ./internal/ui ./internal/action .
golangci-lint run ./...
nilaway -include-pkgs=github.com/noamsto/prdash ./...
go test ./internal/gh ./internal/ui ./internal/action
go test -race -short ./internal/gh ./internal/ui ./internal/action
```

## Key choice

`S` (spec S1). Verified free: board key switch (`internal/ui/prlist.go`
`case "a" "s" "z" "D" "g" "R" "/" "?" "q" "V" "p" j k l h 1-6`) and
`action.DefaultPRActions()` (`m A r y Y b o O W u M L X`).

## Steps

### Step 1 — gh client (`implement: sonnet`)

Files: `internal/gh/stack_rest.go` (new), `internal/gh/stack_rest_test.go`
(new), `internal/gh/source.go`, `internal/gh/graphql.go` (doc comment list
only), `internal/ui/mutationsource_test.go`, `internal/ui/held_test.go` (fake
stubs so `internal/ui` still compiles).

1. Types in `stack_rest.go`:
   - `StackMember{Number int; State string; IsDraft bool}` — `State` is
     `OPEN|CLOSED|MERGED` (REST `merged_at` non-null/non-empty ⇒ `MERGED`,
     else `strings.ToUpper(state)`).
   - `Stack{Number int; BaseRefName string; Members []StackMember}` bottom →
     top, from the REST `number`, `base.ref`, `pull_requests` order.
   - `StackMergeResult{Status, Message, UUID, SHA string}` plus constants
     `StackMergePending|Merged|Enqueued|Failed = "pending"|"merged"|"enqueued"|"failed"`.
2. `restRequestJSON(method, path string, payload []byte) (*http.Request, error)`
   beside `restRequest` (same Accept / API-version headers, plus
   `Content-Type: application/json`, body `bytes.NewReader(payload)`).
   `restRequest` itself is unchanged.
3. `func (s GraphSource) FetchStackForPR(prNumber int) (Stack, bool, error)`:
   `GET /repos/{o}/{n}/stacks?pull_request={N}`; 200 → decode `[]wire`;
   empty ⇒ `(Stack{}, false, nil)`; else first entry, `true`. 404 ⇒ error
   `stacked PRs aren't available for this repo`; other non-200 ⇒
   `fmt.Errorf("stack lookup: %s: %s", resp.Status, body)`.
4. `func (s GraphSource) BaseUsesMergeQueue(baseRef string) (bool, error)`:
   raw GraphQL POST to `s.restBase()+"/graphql"` (in production that is
   `githubGraphQLURL`; the `restBase` route is what makes it testable), query
   `query($owner:String!,$name:String!,$branch:String!,$qualified:String!){repository(owner:$owner,name:$name){mergeQueue(branch:$branch){id} ref(qualifiedName:$qualified){rules(first:50){nodes{type}}}}}`
   with `qualified = "refs/heads/"+baseRef`. True iff `mergeQueue` non-null or
   any rule `type == "MERGE_QUEUE"`. A non-200 or a non-empty `errors` array ⇒
   error (the caller falls back to false; spec S7).
5. `func (s GraphSource) MergeStack(prNumber int, mergeQueue bool) (StackMergeResult, error)`:
   `PUT /repos/{o}/{n}/pulls/{N}/merge-async`, body
   `{"merge_method":"squash","merge_action":"direct_merge"}` or, when
   `mergeQueue`, `{"merge_action":"merge_queue"}` (struct with
   `merge_method,omitempty`). 200/202 ⇒ decode `{status, details{message,
   uuid, sha}}`. 404 ⇒ `async stack merge isn't available for this repo`;
   409 ⇒ `a merge for this stack is already in progress`; other ⇒
   `fmt.Errorf("stack merge: %s: %s", resp.Status, trimmed body)` (prdash keeps
   the body gh-stack's go-gh client discards).
6. `func (s GraphSource) StackMergeStatus(prNumber int, uuid string) (StackMergeResult, error)`:
   `GET /repos/{o}/{n}/pulls/{N}/merge-async/{url.PathEscape(uuid)}`; 200 ⇒
   decode; else status+body error.
7. `source.go`: `MutationSource` gains `MergeStack(prNumber int, mergeQueue
   bool) (StackMergeResult, error)` and `StackMergeStatus(prNumber int, uuid
   string) (StackMergeResult, error)`, its doc comment noting these two take
   the PR **number** (REST), unlike the node-ID GraphQL methods. New
   `StackSource interface { FetchStackForPR(int) (Stack, bool, error);
   BaseUsesMergeQueue(string) (bool, error) }`. Add both to the `GraphSource`
   doc list in `graphql.go`.
8. Fakes: `fakeMutationSource` records `stackMergeCalls []stackMergeCall{number
   int; mergeQueue bool}` and returns configurable `stackMergeResult` /
   `stackPollResults []gh.StackMergeResult` (consumed in order) /
   `stackPollErr`; `partialMergeSource` gets zero-value stubs.

Tests (`stack_rest_test.go`, `testSource` + `requireHeaders` from
`actions_rest_test.go`), each asserting method, exact path, headers and, for
PUT/POST, the decoded body:

- FetchStackForPR: list with merged/closed/draft/open members → normalized
  states, order kept, `BaseRefName`; empty list ⇒ `false`; 404 ⇒ error.
- BaseUsesMergeQueue: `mergeQueue` non-null ⇒ true; only a `MERGE_QUEUE` rule ⇒
  true; neither ⇒ false; the posted variables carry `refs/heads/<base>`.
- MergeStack direct: PUT body exactly
  `{"merge_method":"squash","merge_action":"direct_merge"}`, Content-Type
  JSON; 202 pending with uuid decoded; 200 merged with sha.
- MergeStack queue: body exactly `{"merge_action":"merge_queue"}`.
- MergeStack 404 / 409 / 422 error texts.
- StackMergeStatus: GET path ends `/merge-async/<uuid>`, failed status +
  message decoded.

Gate: the gate commands above.

### Step 2 — pure core (`implement: sonnet`), after step 1

Files: `internal/ui/stackmerge.go` (new), `internal/ui/stackmerge_test.go`
(new), `internal/ui/messages.go` (only the new `actionDoneMsg.stackMerge bool`
field — the run's settle sets it; the other msg types land in step 3).

1. Board snapshot, built on the Update goroutine so the async load never reads
   `m`:
   ```go
   type stackBoardRow struct {
       pr             gh.PR
       mergeable, mss string // mergeState(pr, m.detail[n], ok)
       mutableErr     error  // m.mutable(pr)
   }
   ```
2. `stackRange(target int, st gh.Stack) ([]gh.StackMember, error)` — members
   bottom → up to and including `target` **by number**, MERGED dropped.
   Errors: target absent ⇒ `PR #t is no longer in this stack — ctrl+r to
   refresh`; target MERGED or CLOSED ⇒ `PR #t is not open`.
3. `stackNeedsDetail(members []gh.StackMember, board map[int]stackBoardRow) []int`
   — numbers off the board, or on it with `mergeUnresolved(mergeable)`, in
   member order.
4. `checkStackLayers(members, board, fresh map[int]gh.PRDetail) ([]gh.PR, error)`
   — per member bottom first, first failure wins:
   CLOSED ⇒ `PR #N is not open`; on board and `mutableErr != nil` ⇒ it;
   `IsDraft` ⇒ `PR #N is a draft`; reading = `fresh[N]`'s
   `Mergeable`/`MergeStateStatus` if present (overriding the board reading,
   the merge-squash refetch rule), else the board reading, else none;
   `mergeConflicted` ⇒ `PR #N has conflicts`; `mergeUnresolved` (including
   "no reading at all") ⇒ `PR #N merge state is still pending — try again
   shortly`. Returns each layer as its board `gh.PR` or `gh.PR{Number, State,
   IsDraft}` for off-board ones.
5. `type stackMergePlan struct { target int; layers []gh.PR; mergeQueue bool }`
   with `numbers() []int` and `question() string` →
   `Merge stack up to #142 (3 PRs: #140 #141 #142)?` (`1 PR` singular), via
   the existing `numList`.
6. `loadStackPlan(t gh.PR, board map[int]stackBoardRow, stacks gh.StackSource, details gh.DetailSource) (*stackMergePlan, error)`
   — the off-goroutine body: FetchStackForPR (error ⇒ `Stack merge failed:
   <err>`; `false` ⇒ `PR #t is no longer in a stack — ctrl+r to refresh`) →
   stackRange → stackNeedsDetail → `details.FetchDetails(needs)` when non-empty
   (an error leaves `fresh` empty, so those layers fall to "still pending";
   nil `details` likewise) → checkStackLayers → `BaseUsesMergeQueue(st.BaseRefName)`
   (error ⇒ false) → plan.
7. Run:
   ```go
   const (
       stackMergePollEvery = 2 * time.Second
       stackMergePollBeats = 90
   )
   type stackMergeStep int // stackMergePoll | stackMergeSettle
   type stackMergeRun struct {
       id      int // matches beat msgs to this run
       plan    *stackMergePlan
       uuid    string
       beats   int
       lastErr error
       stat    *actionStat
       done    actionDoneMsg // the settle, valid once step == stackMergeSettle
   }
   func (r *stackMergeRun) onSubmitted(res gh.StackMergeResult, err error) stackMergeStep
   func (r *stackMergeRun) onPolled(res gh.StackMergeResult, err error) stackMergeStep
   ```
   `onSubmitted`: err ⇒ settle `Stack merge failed: <err>`; else `resolve`.
   `onPolled`: `beats++`; err ⇒ `lastErr = err`, poll unless budget spent;
   else `resolve`; budget spent (`beats >= stackMergePollBeats`) ⇒ settle
   `Stack merge still running on GitHub — check the PRs` (`… (last check:
   <lastErr>)` when the final beat errored). `resolve(res)`: merged ⇒ settle
   `{stackMerge: true, merged: plan.layers, ok: "Stack merged ×N"}`; enqueued
   ⇒ `{stackMerge: true, ok: "Stack queued ×N"}`; failed ⇒ error `Stack merge
   failed: <message> — nothing merged`; pending with uuid ⇒ store uuid, poll;
   pending without ⇒ error `Stack merge did not start`; any other status ⇒
   poll (unknown is not done). Every error settle carries `stackMerge: true`,
   `err`, `fail` = the same text.

Tests (`stackmerge_test.go`, values only, no Model):

- stackRange: target at bottom (1 layer), middle (layers 1..k), top (all);
  merged bottom layer dropped; layers above target excluded; target absent;
  target merged; target closed.
- stackNeedsDetail: off-board and unresolved members listed, resolved
  on-board ones not.
- checkStackLayers: refusal naming a closed, a draft, a conflicted (board
  reading and fresh reading), and a pending layer (unresolved after refetch;
  off-board with no fresh detail); first offender wins when two layers fail;
  held-row `mutableErr` surfaced; off-board clean layer passes as
  `gh.PR{Number}`.
- question: count + numbers; singular.
- loadStackPlan with a fake StackSource/DetailSource: off-board member
  fetched via FetchDetails with exactly the needed numbers; FetchDetails error
  ⇒ pending refusal; queue detection true/false/error ⇒ `mergeQueue`.
- run: merged on submit ⇒ `merged` == every plan layer; enqueued ⇒ no
  merged; failed message; pending→pending→merged across polls; probe errors
  keep polling and the exhausted settle names the last error; budget
  exhaustion with no error; empty uuid; submit error.

Gate: the gate commands above.

### Step 3 — Model wiring, action, legend (`implement: opus`), after step 2

Files: `internal/action/defaults.go`, `internal/action/defaults_test.go`,
`internal/ui/messages.go`, `internal/ui/prlist.go`, `internal/ui/actions.go`,
`main.go`, `internal/ui/stackmerge_model_test.go` (new), plus whichever
existing layout/hint tests assert the action list (`gridhints_test.go`,
`actions_test.go`).

1. `defaults.go`: `"S": {Key: "S", Label: "Merge stack (squash)", Command:
   Command{Native: "merge-stack"}, Scope: "single", Refresh: true, Progress:
   "Merging stack", Past: "Stack merged", Fail: "Stack merge failed"}` with a
   one-line comment that it prompts itself after loading the stack (so
   `Confirm` is false). Extend `defaults_test.go`'s expectations.
2. `messages.go` (`actionDoneMsg.stackMerge` already exists from step 2):
   `stackPlanLoadedMsg{seq int;
   plan *stackMergePlan; err error}`; `stackMergeSubmittedMsg{id int; res
   gh.StackMergeResult; err error}`; `stackMergeProbeMsg{id int}`;
   `stackMergePolledMsg{id int; res gh.StackMergeResult; err error}`.
3. Model fields (prlist.go struct): `stackSource gh.StackSource`,
   `stackLoadSeq int`, `stackLoading bool`, `pendingStackMerge
   *stackMergePlan`, `stackMerge *stackMergeRun`, `stackRunID int`.
   `SetStackSource`; `main.go` calls `m.SetStackSource(gs)` beside
   `SetDetailSource`.
4. `runAction`: first thing after the cascade guard,
   `if a.Command.Native == "merge-stack" { return m.startStackMerge() }`.
   `startStackMerge` (in `stackmerge.go` or `actions.go`): not a `*PRSection`
   or empty ⇒ nil; cursor PR `t`; `t.Stack == nil` ⇒ `statusHint("PR #t is not
   in a stack")` semantics (status set, `clearStatusCmd`); `m.mutable(t)` ⇒
   that error as a settled status; `m.stackLoading || m.pendingStackMerge !=
   nil || m.stackMerge != nil` ⇒ `return nil` (silent; see below);
   `m.stackSource == nil` ⇒ status `Stack merge unavailable`. The
   already-running case is a **silent no-op** (`return nil`, badge
   untouched), exactly like the `m.cascade != nil` guard: `statusHint` is a
   no-op while `actionRunning()`, which holds for the whole load and run, and
   writing the status directly would steal the run's badge mid-poll. Else build the
   board snapshot from every `ps.prAt(i)`, `stackLoadSeq++`,
   `stackLoading = true`, running badge `Loading stack` with the spinner, and
   return a cmd running `loadStackPlan` → `stackPlanLoadedMsg`.
5. Update arm `stackPlanLoadedMsg`: `seq != m.stackLoadSeq` ⇒ ignore.
   `stackLoading = false`. In the err and drop branches below, write the
   settled stat (err text / cancel hint) **directly** — `statusHint` is a no-op
   while the loading stat runs — and only when `m.actionStatus` is still the
   loading stat (leave a foreign badge alone); then `clearStatusCmd`; the drop
   test asserts `!m.actionRunning()`. err ⇒ settled failed status with the err
   text. A modal is open or the context moved (`m.pending != nil ||
   m.showPicker || m.showActions || m.showLegend || m.logView || m.expanded ||
   m.filtering || m.mode != "pr"`) ⇒ drop with hint
   `Stack merge cancelled — press S again`. Else clear the loading badge
   (`m.actionStatus = nil` only if it is still the loading stat),
   `m.pendingStackMerge = plan`, `m.pending = &a` where `a` is
   `m.actions["S"]`-equivalent: store the triggering action on the load (pass
   it through the msg or keep `m.stackLoadAction`) so labels survive a
   user-remapped key.
6. Every site that sets `m.pending` for a different action
   (`startBulk`, the two `m.pendingCascade = nil // this path never builds a
   cascade plan` sites in `prlist.go`) also nils `m.pendingStackMerge`.
   `confirmQuestion`: `m.pendingStackMerge != nil` ⇒
   `m.pendingStackMerge.question()` (checked before the cascade branch).
   `confirmAnswer`: snapshot and nil `pendingStackMerge` with the others; if
   it was non-nil, `yes` ⇒ `m.runStackMerge(*a, plan)`; `no` ⇒ nil. It must
   be consumed before the `per-selected` / `runAction` fallthrough so a stack
   prompt never re-enters `runAction`.
7. `runStackMerge(a, plan)`: `stackRunID++`; stat = `statForBulk(a,
   len(plan.layers))`, `refresh = true`, `nums = plan.numbers()`, `native =
   "merge-stack"`; `m.stackMerge = &stackMergeRun{id, plan, stat}`;
   `m.actionStatus = stat`; `m.invalidateLaunchCache(nums...)`;
   `m.clearSelection()`; cmd `MergeStack(plan.target, plan.mergeQueue)` →
   `stackMergeSubmittedMsg{id}`, batched with `m.startSpinner()`.
8. Update arms: `stackMergeSubmittedMsg` / `stackMergePolledMsg` — ignore when
   `m.stackMerge == nil || msg.id != m.stackMerge.id`; call
   `onSubmitted`/`onPolled`; `stackMergePoll` ⇒ `stat.run =
   fmt.Sprintf("Merging stack ×%d · %d/%d", n, beats+1, stackMergePollBeats)`,
   `tea.Tick(stackMergePollEvery, → stackMergeProbeMsg{id})`;
   `stackMergeSettle` ⇒ re-assert `m.actionStatus = r.stat` (as
   `cascadeSettleCmd` does) and return `func() tea.Msg { return r.done }`.
   `stackMergeProbeMsg` (id-guarded) ⇒ cmd calling
   `StackMergeStatus(plan.target, uuid)` → `stackMergePolledMsg{id}`.
9. `actionDoneMsg` arm: `if msg.stackMerge { m.stackMerge = nil }` right
   after `markSessionMerged`, **before** the `m.actionStatus == nil` early
   return (a settle landing on a nil status must still free the run); extend the failure detail-refetch condition to
   `native == "merge-squash" || native == "merge-stack"`. `merged` already
   flows through `markSessionMerged` above the status guard, and the success
   refresh uses `stat.nums` (every layer).
10. Hints: `actionOrder` gains `"S"` after `"m"`; `keyPanes` PR actions gain
    `keyHint{"S", "stack merge", nil}` after `m`. `batchCapable` already hides
    a `single` action in batch mode. Fix the hint/layout tests the new cell
    moves, by updating expectations, never by deleting coverage. Run
   `TestFilterActions` (`actionview_test.go`, expects the `merge` query's
   first hit to be `m`) and adjust only if the new label outranks `m`.

Model tests (`stackmerge_model_test.go`, `newTestModel`/fake sources the
existing tests use, a fake `StackSource`, `driveBulk`-style batch draining):

- `S` on a non-stacked cursor PR ⇒ status `PR #N is not in a stack`, no
  StackSource call, no MergeStack call.
- `S` on a stacked middle layer whose lower layer is off the board ⇒ load cmd
  → `stackPlanLoadedMsg` → `m.pending` set and `confirmQuestion()` ==
  `Merge stack up to #142 (3 PRs: #140 #141 #142)?` (AC2); FetchDetails asked
  for the off-board number.
- `y` ⇒ exactly one `MergeStack(142, false)`; merged result ⇒ the settle
  `actionDoneMsg` has `stackMerge` and `merged` numbers `[140 141 142]`;
  feeding it through Update marks all three session-merged and clears
  `m.stackMerge`.
- `n` ⇒ no MergeStack call, `pendingStackMerge` nil.
- A refused plan (conflicted layer) ⇒ no prompt, status names the PR, no
  MergeStack call.
- A non-`stackMerge` `actionDoneMsg` mid-poll does not clear `m.stackMerge`;
  a stale-id polled msg is ignored.
- A load landing while the legend is open is dropped with the cancel hint.
- Second `S` while a run is live ⇒ no second load/StackSource call, the run's
  badge unchanged.
- A load landing while the `/` filter is focused is dropped with the cancel
  hint (no `m.pending`).

Gate: the gate commands above.

### Step 4 — docs (`implement: haiku`), after step 3

Files: `README.md` (the PR-only key table row at line ~62: add `S` merge
stack), spec/plan already committed. Named edit only.

Gate: none beyond `go build ./...` (docs only).

## Acceptance mapping

| AC | covered by |
|---|---|
| AC1 positions 1/middle/top | step 2 stackRange tests |
| AC1 non-stacked refusal | step 3 model test |
| AC1 conflicted/draft/closed/pending refusal naming the PR | step 2 checkStackLayers tests (+ step 3 conflicted model test) |
| AC1 request method/path/body | step 1 httptest tests; step 3 fake records `MergeStack(142,false)` |
| AC1 every merged PR through `merged` | step 2 run test + step 3 model test |
| AC2 prompt text | step 2 question test + step 3 model test |
| AC3 CI gate | `nix build -L .#checks.x86_64-linux.go-gate` before push |
| AC4 PR body | endpoint + gh-stack `v0.2.1` `29a36d3` citations from the spec |
