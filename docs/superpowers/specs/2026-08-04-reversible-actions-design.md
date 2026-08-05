# Reversible auto-merge and mark-ready

Issue: #90

## Problem

`A` (auto-merge, squash) and `M` (mark ready) are one-way. `actions.go:234` calls
`EnableAutoMerge` and `:243` calls `MarkReady`; neither has an inverse anywhere
in the codebase. Arming auto-merge on the wrong PR, or marking a draft ready
before it was actually ready, currently means leaving prdash to undo it in the
browser — which is exactly the workflow prdash exists to remove.

Both are also the two actions `ConfirmOthers` guards, which is an admission that
they are the easiest to fire by accident.

## Verified API surface

Both inverses exist in the live schema **and** in the pinned `githubv4`, with the
identical single-field input shape as the calls already in
`mutations_graphql.go`:

| Direction | Mutation | githubv4 input |
|---|---|---|
| arm | `enablePullRequestAutoMerge` | `EnablePullRequestAutoMergeInput` |
| disarm | `disablePullRequestAutoMerge` | `DisablePullRequestAutoMergeInput{PullRequestID}` |
| ready | `markPullRequestReadyForReview` | `MarkPullRequestReadyForReviewInput` |
| draft | `convertPullRequestToDraft` | `ConvertPullRequestToDraftInput{PullRequestID}` |

So the new source methods are structural copies of the existing two.

## Reversibility audit

Recorded because the question "what else?" deserves a real answer rather than
another round of investigation later.

| Key | Action | Reverse | Verdict |
|---|---|---|---|
| `A` | Auto-merge | `disablePullRequestAutoMerge` | **In scope** |
| `M` | Mark ready | `convertPullRequestToDraft` | **In scope** |
| `R` | Assign reviewers | `requestReviews` without `union` replaces the set, so deselecting removes | Optional |
| `L` | Approve | `dismissPullRequestReview` needs `viewerLatestReview.id` **and a mandatory message** | Out — a prompted action, not a toggle |
| `m` | Merge | none | Terminal by nature |
| `u` | Update branch | none | No inverse exists |
| `r` | Rerun checks | cancelling a run is a different REST call | Not an inverse |
| `X` | Clean up branch | none | Local deletion |

## Behavior

### One key, direction from state

`A` and `M` keep their keys. The direction, label, and progress/past strings all
derive from the **cursor PR's** current state:

| Cursor state | `A` does | Label |
|---|---|---|
| not armed | arm | `Auto-merge (squash)` |
| armed | disarm | `Disable auto-merge` |

| Cursor state | `M` does | Label |
|---|---|---|
| draft | mark ready | `Mark ready` |
| ready | convert to draft | `Convert to draft` |

`Action` carries static `Label` / `Progress` / `Past` strings, so this needs two
Action variants per key resolved at dispatch, and `actionHints` must pick by
cursor state so the docked panel shows the correct verb **before** the key is
pressed. The panel label is the affordance that makes a state-dependent key
safe; without it this is a trap.

### Mixed selections: the cursor decides

Both actions are `per-selected`. With three rows selected — two armed, one not —
**the focused PR's state picks one direction and it applies to every selected
row**. It is idempotent on those already in the target state.

Rejected alternative: flipping each row independently. It is the more literal
reading of "toggle", but one keystroke then produces three different outcomes,
and a second press does not undo the first — it inverts again. Predictability
beats literalism for a fan-out.

Also rejected: refusing mixed selections. Safe, but it makes the operator narrow
the selection by hand every time it is not uniform, for a case the label already
disambiguates.

### Confirmation

`ConfirmOthers` continues to apply in both directions. Disarming and
converting-to-draft are, if anything, more disruptive than their forward
directions — converting a ready PR to draft pulls it out of everyone's review
queue — so neither direction gets a discount.

### Optimistic paint must reverse

`prlist.go:231` optimistically sets `AutoMergeRequest = &gh.AutoMergeRequest{MergeMethod: "SQUASH"}`
on success. The disarm path must set it back to `nil`, or the glyph stays lit
until the next background refresh and the board contradicts the action that just
succeeded.

The same applies to `IsDraft` for `M`. Note that under #88 the draft state owns
**gutter cell 1** rather than a trailing tag, so both specs modify this path —
whichever lands second inherits the other's shape.

## Approach

| Approach | Idea | Verdict |
|---|---|---|
| **A. Same key, two Action variants resolved by cursor state** | `A`/`M` keep their bindings; dispatch picks the variant | **Chosen** — no new keys to learn, and the panel label makes the direction visible before commit |
| B. New keys for the reverse directions | Explicit, no state dependence | Rejected — spends two more keys on the inverse of existing ones, and the board already shows the state that makes them redundant |
| C. Single Action with a dynamic label callback | One entry per key | Rejected — turns `Action`'s declarative strings into functions for two of thirteen actions; the variant pair keeps the struct plain and testable |

Approach A keeps `Action` a value type with static strings, which is what makes
the existing action table trivially unit-testable, and confines the state
dependence to dispatch and hint rendering.

## Testing

- Source methods: `DisableAutoMerge` and `ConvertToDraft` issue the expected
  mutation with the PR id, mirroring the existing enable/mark-ready tests.
- Variant resolution: armed cursor resolves to the disarm variant and vice
  versa; draft cursor to mark-ready, ready cursor to convert-to-draft.
- `actionHints` shows the verb matching the cursor, and it changes when the
  cursor moves between an armed and an unarmed PR.
- Mixed selection: cursor-armed fires disarm for all three; cursor-unarmed fires
  arm for all three. Assert the direction is uniform, not per-row.
- Optimistic paint: disarm clears `AutoMergeRequest` to nil; convert-to-draft
  sets `IsDraft` true, and the row's gutter cell 1 follows (once #88 lands).
- `ConfirmOthers` prompts on both directions for a PR the viewer did not author.

## Out of scope

- `L` / approve dismissal — needs a review id and a mandatory message, so it is a
  distinct prompted action rather than a toggle.
- Reviewer removal via `requestReviews` — listed as optional above; splitting it
  out keeps this issue to the two mutations that are exact inverses.
- Cancelling in-flight check runs, which is a new capability rather than the
  reverse of `r`.
