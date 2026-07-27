# Approve Action + Update-branch Checks Design

**Status:** Approved (design reviewed)
**Issue:** #58

---

## Part 1 — Approve action

### GraphQL mutation

New `GraphSource.ApprovePR(prID string) error` in `internal/gh/mutations_graphql.go`, alongside `MarkReady`/`UpdateBranch`:

```graphql
addPullRequestReview(input: {pullRequestId: $id, event: APPROVE})
```

No body, no commit SHA. The method is added to the `MutationSource` interface in `internal/gh/source.go` and to the `MockMutationSource` (or equivalent fake) used in tests.

### Dispatch

`nativeMutationFn` (`internal/ui/actions.go`) gains `case "approve"` with three client-side pre-checks resolved synchronously on the calling goroutine, matching the existing pattern:

| Condition | Error returned |
|---|---|
| `p.ID == ""` | added to the existing stale-cache switch (`:204`) |
| `p.State != "OPEN"` | `PR #N is not open` |
| `p.Author.Login == m.viewerLogin` (viewer known) | `can't approve your own PR #N` |

The self-approve guard matters most in bulk: GitHub returns a 422 for own-PR approvals; without the guard a mixed Mine/Others selection half-fails with an opaque aggregate error.

When `viewerLogin == ""` (not yet resolved), the self-approve guard is skipped — the mutation fires and GitHub returns an error if it fires on the viewer's own PR. This is acceptable: `viewerLogin` is fetched at startup and is almost always available; skipping a probabilistic guard rather than blocking is consistent with the `mergePreCheck` pattern for `mergeable`.

### Default binding

`internal/action/defaults.go`:

```go
"L": {Key: "L", Label: "Approve",
    Command:       Command{Native: "approve"},
    Scope:         "per-selected", Confirm: true, Refresh: true,
    Progress:      "Approving", Past: "Approved", Fail: "Approve failed"},
```

- `Scope: "per-selected"` — fans out across a bulk selection via the existing `startBulk` → `runBulkNative` path, settling to an `Approved ×N` badge.
- `Confirm: true` — `startBulk` prompts before firing. `confirmQuestion` already renders `Approve #123?` for one row and `Approve for 5 PRs?` for a selection; no new prompt logic needed.
- `Refresh: true` — after success, `actionDoneMsg` invalidates the touched PRs' detail cache and calls `backgroundRefresh()`, so `ReviewDecision` flips in the list.

`L` is free on the board (verified against `prlist.go:1494–1632`).

### Triage integration

`triage.Compute` and `triage.Preliminary` gain a `viewer string` parameter. When `viewer != ""` and `pr.Author.Login == viewer`, the awaiting-review card omits the action key:

```go
// REVIEW_REQUIRED
card := Card{Kind: KindAwaitingReview, Headline: awaitingHeadline(d), JumpTab: "reviews"}
if viewer == "" || pr.Author.Login != viewer {
    card.ActionKey, card.ActionLabel = "L", "approve"
}
return card
```

Call sites:
- `internal/ui/preview.go:334,336` — pass `m.viewerLogin`
- `internal/ui/prlist.go:1917` — pass `m.viewerLogin`
- `internal/ui/expanded.go:154` — pass `m.viewerLogin`

Triage tests pass `""` (unknown viewer) → no approve suggestion; a separate test covers the own-PR suppression.

---

## Part 2 — Update-branch → checks re-run

### Problem

`UpdateBranch` triggers a new push, GitHub re-queues CI workflows. The immediate `backgroundRefresh()` races the queue; the row returns the pre-push rollup (green ✓ or whatever was there), and stays stale until the next periodic refresh.

The same staleness applies to `RerunChecks` (`r`).

### Optimistic override via `ciRerun`

New field on `Model`:

```go
ciRerun map[int]time.Time  // PR number → expiry (now + 2 minutes)
```

Stamped for all touched PR numbers after a successful `update-branch` or `rerun-failed` action (in `actionDoneMsg` handling). Cleared per PR when either:
1. The next fetched rollup already contains at least one `QUEUED`/`IN_PROGRESS` check (reality caught up — stop faking), or
2. The expiry has passed.

Applied in `hydratePRs` (or equivalent where fresh PRs enter the model from a fetch): before handing a PR to `setSections`, if `ciRerun[p.Number]` is non-zero and unexpired, rewrite its `StatusCheckRollup` entries to `State: "IN_PROGRESS"`. All display sites — rows (`section.go`), preview, expanded, triage, filter — call `p.CIState()` and see "pending" without each needing its own override.

This is a single mutation point, and it only fires on fetched data, not stale cache — so it can't make a genuinely-finished run look in-progress after the fact.

### Wording

- `u` `Past` → `"Branch updated — checks re-running"`
- `r` `Past` → `"Checks rerun — re-running"` (currently `"Checks rerun"`)
- Progress and Fail are unchanged.

### Delayed second refresh

For `update-branch` and `rerun-failed`, `actionDoneMsg` schedules a second `backgroundRefresh()` ~12 seconds after the first. By then GitHub has queued the new runs and the rollup reflects reality, making the `ciRerun` override unnecessary — it naturally expires or clears on the real pending state.

Implementation: return a `tea.Tick(12*time.Second, func(t time.Time) tea.Msg { return delayedRefreshMsg{} })` alongside the immediate `backgroundRefresh()`, and handle `delayedRefreshMsg` as an alias for the refresh trigger.

---

## Testing

### Approve mutation
- `internal/gh/mutations_graphql_test.go`: table test using the existing httptest GraphQL harness, asserting the mutation variable shape (event: APPROVE, no body).

### `nativeMutationFn` pre-checks
- Unit tests alongside existing `mark-ready` cases: own-PR (viewer known), own-PR (viewer unknown → passes), closed PR, stale ID — verifying the returned error message.

### Bulk self-approve skip
- A selection spanning two PRs (one Mine, one Others) exercises `runBulkNative`: the Mine call returns the self-approve error, Others succeeds; the aggregate badge reads "1 of 2 failed", not a GraphQL surprise.

### Triage approve suggestion
- `own PR + REVIEW_REQUIRED`: `Compute(pr, d, viewerLogin)` where `pr.Author.Login == viewerLogin` → `ActionKey == ""`.
- `others' PR + REVIEW_REQUIRED`: `ActionKey == "L"`.
- `viewer == "" + REVIEW_REQUIRED`: `ActionKey == ""` (safe default).

### `ciRerun` expiry and clear paths
- Expiry: stamp a PR, advance time past 2 min, verify next hydrate produces no override.
- Real-pending clears: stamp a PR, hydrate with a rollup containing `IN_PROGRESS`, verify `ciRerun` entry removed.
