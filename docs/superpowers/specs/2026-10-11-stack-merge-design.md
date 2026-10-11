# Spec — merge a GitHub stack up to the cursor PR, atomically (#169)

## Problem

prdash models GitHub stacks (`gh.PR.Stack *PRStack`, `gh.PR.StackPosition`,
`internal/gh/prs.go`) and `u` cascades update-branch up a stack
(`internal/ui/cascade.go`), but merging a stack means pressing `m` on each
layer in order or leaving the TUI for `gh stack merge`. Per-layer `m` is not
atomic: a failure halfway leaves the stack half-merged.

## Goal

From a PR in a GitHub stack, one key merges every not-yet-merged layer from the
bottom of the stack up to and including that PR, in a single all-or-nothing
server-side operation (GitHub's native async stack merge) — squash for a
direct merge, the queue's own configured method when the base merges through
a merge queue — behind a confirmation prompt naming the count and every PR number.

Out of scope: sequential per-layer merging (never a fallback), admin/bypass
merges, merge-method choice, multi-select fan-out, `gh stack link`.

## The API (derived from `github/gh-stack` source)

Source: `github.com/github/gh-stack` at tag `v0.2.1`, commit
`29a36d314be664583705f18e41cebdf2b61ad514`.

- **Submit** — `internal/github/merge_async.go:221-241` (`MergeStackAsync`):
  `PUT /repos/{owner}/{repo}/pulls/{number}/merge-async`, JSON body
  `{"merge_method":"<method>","merge_action":"<action>"}` (`merge_method`
  `omitempty`). "For a stacked PR this merges all members of the stack up to
  and including prNumber" (doc comment, `:207-209`). `merge_action` values
  (`:22-32`): `default` lets the server choose; `direct_merge` / `merge_queue`
  force one, and "the server rejects `merge_queue` on a branch with no queue".
- **Routing as gh-stack actually sends it** — `cmd/merge.go:138-155,471-476`:
  it detects whether the stack's base branch merges through a queue
  (`baseBranchUsesMergeQueue` → `merge_async.go:166-205`, GraphQL
  `repository.mergeQueue(branch: $base)` non-null, or a `MERGE_QUEUE` rule
  among `ref(qualifiedName: "refs/heads/<base>").rules(first: 50)`), then
  sends **`merge_queue` with no `merge_method`** ("The queue picks the merge
  method from its own configuration", `:148-155`) or **`direct_merge` with the
  method**. A failed detection falls back to the direct flow (`:458-469`).
  prdash mirrors this exactly (S7); the doc comment at `merge_async.go:160-165`
  claiming "always sends merge_action default" is stale against `cmd/merge.go`.
- **Poll** — `merge_async.go:247-254` (`GetAsyncMergeResult`):
  `GET /repos/{owner}/{repo}/pulls/{number}/merge-async/{uuid}`, always 200 on
  a valid lookup.
- **Response** (both, `:79-107`): `{"status": "...", "details": {"message",
  "uuid", "merge_method", "merge_action", "expected_head_sha", "sha"}}`, status
  one of `pending` (running in the background; `details.uuid` names it),
  `merged`, `enqueued` (added to the base's merge queue), `failed`
  (`details.message` says why; nothing merged — "Stack merges are atomic",
  `cmd/merge.go:408`). Submit returns 200 (already resolved) or 202 (pending).
- **Errors** (`classifyAsyncMergeError`, `:309-322`): 404 → async stack merge
  is unavailable for this repo/token; 409 → a merge request already exists for
  this stack; 400 → the stack can no longer be merged as requested.
- **Membership** — `internal/github/github.go:415-428, 438-448, 515-526`
  (`FindStackForPR`): `GET /repos/{owner}/{repo}/stacks?pull_request={number}`
  returns a list (empty when the PR is in no stack) whose entries are
  `{"number", "base": {"ref"}, "pull_requests": [{"number", "state":
  "open|closed", "draft", "merged_at"}]}`, PRs ordered bottom to top. Looking
  the stack up **by PR number** sidesteps whether GraphQL `PRStack.Number`
  equals the REST stack number. Merged
  layers stay listed (gh-stack's `mergeCandidates`, `cmd/merge.go:520-536`,
  skips them).
- **Auth/headers**: gh-stack builds a stock go-gh REST client
  (`github.go:66-88`, `api.ClientOptions{Host: host}`) — no preview media type,
  no extra header. prdash's existing `restRequest`
  (`internal/gh/actions_rest.go:39-47`, `Accept: application/vnd.github+json`,
  `X-GitHub-Api-Version: 2022-11-28`) over its oauth2 client carries the same
  bearer token gh-stack would use. Whether the endpoint is enabled for a given
  repo is a server property surfaced as the 404 above, not something only
  gh-stack can do.

## Model

### S1 — the key

`S` — "Merge stack (squash)". Free on both boards (bound today: `a s z D g R /
? q V p j k l h 1-6` plus the action map `m A r y Y b o O W u M L X`). Board
archetype: bare letters are the surface's verbs (`KEYMAP.md`), and `S` reads
as **S**tack (lowercase `s` cycles state; `S` has no paired meaning today and
the convention reserves no capital). A plain `Action` in
`action.DefaultPRActions()` with `Command.Native: "merge-stack"`,
`Scope: "single"`, `Refresh: true`, wording `Merging stack` / `Stack merged` /
`Stack merge failed`. Listed in `actionOrder` (after `m`), the legend's
actions group (`S stack merge`, PR mode), and the issue board's `prOnly` hint
falls out of the existing `DefaultPRActions` lookup.

`Confirm` stays false on the action: the prompt is not the generic
`m.pending` → `runAction` one, because the PR numbers it must name are only
known after S2's fetch. The flow always prompts (S4); nothing merges without
`y`.

### S2 — building the plan (async, before the prompt)

Pressing `S` on the PR board with the cursor on PR *t*:

1. *t* has `Stack == nil` → status hint `PR #t is not in a stack`; nothing
   fetched or merged. (Never a silent single merge.)
2. `m.mutable(t)` fails (merged/closed, or a held row) → that error.
3. A stack merge is already loading, prompting, or running → ignored, silently,
   like the live-cascade guard (a hint would steal the run's badge).
4. Otherwise fire one async load: `FetchStackForPR(t.Number)` (REST
   membership, authoritative for state/draft of every layer, shown or not),
   then mergeability for the in-range layers (S3), then the base branch's
   merge-queue detection (S7), then build the plan. An empty result → refuse
   `PR #t is no longer in a stack — ctrl+r to refresh`.

The plan is the **ordered** membership from the bottom up to and including
*t*, located **by PR number**, not by `StackPosition` — so a renumbering or a
merged-layer offset can never pick the wrong PR. Members already MERGED are dropped
(they need nothing). If *t* is absent from the membership → refuse `PR #t is
no longer in this stack — ctrl+r to refresh`. If *t* itself is already MERGED
→ refuse `PR #t is not open`.

Layers above *t* are never part of the plan.

### S3 — pre-checks (every included layer, bottom first)

`merge-squash`'s client-side checks in `nativeMutationFn`
(`internal/ui/actions.go`: open, held row, conflicted, unresolved-after-refetch)
plus a draft check the task adds (`merge-squash` has none; GitHub rejects a
draft merge), per layer, refusing with the **first** offender named and never
calling the merge API:

| check | source | refusal |
|---|---|---|
| state open | REST membership `state` (closed and not merged) | `PR #N is not open` |
| held row | `m.mutable` for layers on the board | its message |
| not draft | REST membership `draft` | `PR #N is a draft` |
| not conflicted | shown layers: `mergeState(p, m.detail[N], ok)`; layers not on the board, or whose state is unresolved: one batched `DetailSource.FetchDetails` of exactly those numbers, read directly as the detail's `Mergeable`/`MergeStateStatus` (an off-board layer has no `gh.PR` to pass to `mergeState`) | `PR #N has conflicts` |
| mergeability resolved | same, after that refetch | `PR #N merge state is still pending — try again shortly` |

A layer **not on the loaded board** is therefore never assumed fine: its
state/draft come from the REST membership and its mergeability from the
FetchDetails refetch. A FetchDetails error, or a layer the refetch omits, is
the "still pending" refusal for that layer (unknown is never clean).
`FetchStackForPR` failing (404 included) → `Stack merge failed: <err>`,
nothing merged. (`BaseUsesMergeQueue` failing is not a refusal; see S7.)

### S4 — the prompt

The loaded plan opens the standard y/n confirm panel, text:

`Merge stack up to #142 (3 PRs: #140 #141 #142)?`

— count and every PR number, bottom to top, singular `1 PR` for a lone
layer. Never the stack number: `PRStack.Number` "resolves to neither a PR nor
an issue … never render it as #N" (`internal/gh/prs.go`). `y` submits; any
other key cancels and drops the plan.

The plan is a snapshot of values: a background refresh landing during the
prompt cannot change what the prompt promised or what prdash reports merged.
(The server merges its *current* stack up to *t*; a layer added below *t*
between the load and `y` is a server-side race prdash cannot close, the same
as gh-stack's.) If the load lands while another modal
(confirm, picker, actions menu, legend, expanded/log view) is open, the plan is
dropped with a hint rather than stacking a second prompt.

### S5 — the run, its badge and its lifetime

A confirmed plan becomes a `stackMergeRun` held in one Model field
(`m.stackMerge`), the way `cascadeRun` is: it owns the plan, the uuid, the beat
count and **its own `*actionStat`**, assigned to `m.actionStatus` at start and
re-asserted on settle — because sites in `expanded.go` / `logview.go`
overwrite `m.actionStatus` directly (`cascade.go`'s `cascadeRun.stat` doc), so
a run that only lived in the shared slot would lose its badge or its
`nums`/`refresh` to an unrelated action mid-poll.

- Single flight: `m.stackMerge` (and `m.stackLoading` for the S2 fetch) is
  non-nil from `S` until the run's own settle; a second `S` meanwhile is
  ignored per S2.3. Other actions stay reachable, deliberately
  including `m`/`u`/a cascade on plan layers: the server keeps that safe (a
  moved head or an already-merged layer fails the async merge atomically, and
  that failure is the settle), so a second guard would only duplicate it.
- The run's settle message is `actionDoneMsg{stackMerge: true, …}`; the
  `actionDoneMsg` arm clears `m.stackMerge` **only** on a message carrying
  that flag (a copy/rerun/cleanup settle landing mid-poll must not strand the
  run), exactly as `cascade: true` does for `m.cascade`.
- Every beat message (`stackMergeSubmittedMsg`, `stackMergeProbeMsg`,
  `stackMergePolledMsg`) is nil-guarded on `m.stackMerge` and on the run's
  identity, so a stray beat from a finished run is a no-op.
- The load message (`stackPlanLoadedMsg`) carries a sequence number and is
  dropped unless it matches the live load.

`y` → `MutationSource.MergeStack(t.Number, plan.mergeQueue)` (S7) off the Update
goroutine. Badge `Merging stack ×N` with the spinner. `invalidateLaunchCache`
covers every plan number before the call; the stat's `nums` is every plan
number and `refresh` is set.

| response | outcome |
|---|---|
| `merged` | settle `{stackMerge, merged: <every plan PR>}`, badge `Stack merged ×N` |
| `enqueued` | settle `{stackMerge}` (nothing merged yet), badge `Stack queued ×N`; the refresh picks the merge up when the queue lands it |
| `failed` | settle error `Stack merge failed: <details.message> — nothing merged` |
| `pending`, `details.uuid` non-empty | poll (S6) |
| `pending`, empty uuid | settle error `Stack merge did not start` (gh-stack `cmd/merge.go:412-416`) |
| HTTP error | 404 `async stack merge isn't available for this repo`; 409 `a merge for this stack is already in progress`; other → status + body text; all as `Stack merge failed: …` |

Every merged layer is reported through `actionDoneMsg.merged`, so
`markSessionMerged`, the sticky merged rows and the cleanup path cover every
layer, not just *t*. An off-board layer is reported as `gh.PR{Number: N}`
(`markSessionMerged` reads only `Number`; `updatePR` on an absent row is a
no-op). On any error the plan numbers' freshness is dropped and a detail
refetch fires, as `merge-squash` does on failure.

### S6 — polling a pending merge

Bounded by a probe count, like the cascade wait (`cascadeProbeEvery` /
`cascadeWaitProbes`), so a test drives it with no clock: one
`StackMergeStatus(t, uuid)` per beat every 2s, up to 90 beats (~3 min); badge
`Merging stack ×N · <beat>/90`. Each result maps through S5's table. A probe
error does not end the wait on its own (the merge is server-side; a transient
read failure says nothing about it) but the last one is surfaced if the budget
runs out. Budget exhausted → settle error `Stack merge still running on
GitHub — check the PRs` plus a refresh; nothing is reported merged (unknown is
not merged). gh-stack polls 600×1s (`cmd/merge.go:417-425`); prdash's shorter
budget is deliberate — a TUI badge should not spin for ten minutes, and the
exhausted outcome loses nothing because the server keeps going and the
refresh shows the result.

### S7 — the client

`internal/gh`, alongside `MergePR`, over `GraphSource`'s existing oauth2
`http.Client` and `restRequest`:

- `type StackMember struct { Number int; State string; IsDraft bool }` —
  `State` normalized to the GraphQL vocabulary (`OPEN|CLOSED|MERGED`; REST
  `merged_at` non-null ⇒ `MERGED`).
- `type Stack struct { Number int; BaseRefName string; Members []StackMember }`
  (bottom → top).
- `type StackMergeResult struct { Status, Message, UUID, SHA string }`.
- `MutationSource` gains `MergeStack(prNumber int, mergeQueue bool)
  (StackMergeResult, error)` — `mergeQueue` false sends
  `{"merge_method":"squash","merge_action":"direct_merge"}`, true sends
  `{"merge_action":"merge_queue"}` with no method — and
  `StackMergeStatus(prNumber int, uuid string) (StackMergeResult, error)`.
- New `StackSource` (reads, not mutations), wired in `main.go` next to
  `SetDetailSource`: `FetchStackForPR(prNumber int) (Stack, bool, error)`
  (false ⇒ in no stack) and `BaseUsesMergeQueue(baseRef string) (bool,
  error)` — the GraphQL detection above. A detection error falls back to
  `mergeQueue=false`, as gh-stack does; if the base really has a queue the
  server rejects `direct_merge` and that error is the settle.

Request bodies are sent with `Content-Type: application/json`. Tests use the
existing `httptest` + `testSource` pattern (`actions_rest_test.go`) — no real
GitHub call anywhere.

## Acceptance (from the task)

- AC1: unit tests — plan from a cursor PR at the bottom, middle and top
  layer; refusal on a non-stacked PR; refusal naming a conflicted, draft,
  closed and pending layer; the client's method/path/body/headers via
  `httptest`; every merged PR reported through `merged`.
- AC2: the prompt text lists the count and the PR numbers (test).
- AC3: `nix build -L .#checks.x86_64-linux.go-gate` passes.
- AC4: the PR body documents the endpoint and cites the gh-stack file/line
  above.
- No real stack merge against GitHub in testing.
