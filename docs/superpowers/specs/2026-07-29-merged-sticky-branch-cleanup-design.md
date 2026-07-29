# Sticky Merged Rows + Local-Branch Cleanup Design

**Status:** Approved (design reviewed)
**Issue:** #66

---

## Relationship to the 2026-07-15 grace-period spec

`docs/superpowers/specs/2026-07-15-merged-pr-grace-design.md` (on the unmerged
`feat/merged-pr-grace` branch, never implemented, written for the `gh`-subprocess
era) solves a different problem: it pulls PRs merged *anywhere* — by teammates,
on github.com — back into the open board for a timed window via a companion
`is:merged` query, aged out by a `tea.Tick`.

This design is local, event-driven and untimed: only a merge **prdash itself
performed** sticks, and it sticks until the user explicitly refreshes. No
companion query, no timer, no new fetch. The two could coexist later; neither
depends on the other.

## Part 1 — A merged PR stays on the open board

### State

`Model.mergedSticky map[int]gh.PR` — PR number → the merged snapshot to keep
showing. Initialized in `NewModel` alongside `ciRerun`. Never written to the
results cache, so quitting prdash clears it and "or re-open" needs no code.

`actionStat` gains `merged []gh.PR`: the PR values a merge action targeted,
snapshotted **at dispatch** in `runBulkNative` (and `singleNativeCmd`) where
`Command.Native == "merge-squash"` and the `gh.PR` is already in hand. Capturing
at dispatch rather than looking PRs up on completion avoids needing a
by-number lookup on `PRSection`, which has none.

### Promotion on success

In the `actionDoneMsg` handler, when `msg.err == nil`, each snapshot is stamped
`State: "MERGED"`, `MergedAt: time.Now()` and filed into `mergedSticky`. Its
number is also deleted from `m.ciRerun` — a merged PR's checks are moot, and
dropping the stamp means `applyCIRerun` can never paint a landed row as
checks-in-progress, so the two overlays stay order-independent.

A **partially failed** bulk merge reports `err != nil` (the bulk path aggregates),
so it produces no sticky rows at all even though some PRs did merge. The refetch
then shows the truth. Marking a subset from an aggregate error is not worth the
machinery.

### Re-injection

```go
// applyMergedSticky appends the merged PRs prdash landed this session that the
// fetch no longer returns.
func (m *Model) applyMergedSticky(prs []gh.PR) []gh.PR
```

Mirrors `applyCIRerun`: fast-returns when `mergedSticky` is empty, and no-ops
unless `m.mode == "pr" && m.state == "open"` — so the merged board (which
returns these PRs naturally) and the closed board (`is:unmerged`, where a merged
PR would be a lie) are untouched. PRs already present by number are skipped, so
nothing double-lists.

Called from both list-building paths:

- `setPRs` — after `applyCIRerun`.
- `setSections` — appended to `open` *before* categorization, so a landed PR
  falls into its author's Mine/Others group naturally rather than needing a
  pinned position. Its old "Review requested" category is not preserved: a
  merged PR is no longer awaiting review.

Both paths are also what `hydrate` feeds, so a cache-warm filter switch keeps the
row too.

### Clearing

Only the `ctrl+r` key handler (`prlist.go:1616`) clears the map, before calling
`backgroundRefresh()`. Every other caller of that same function — post-action
refetch, the 30s CI poll, `switchToFilter`, `hydrate` — leaves it intact. The row
disappears when that manual fetch's results land, not on the keypress itself;
a sub-second lag on an explicitly requested refresh needs no extra machinery.

### Rendering

`RowOpts` gains `Landed bool`, set in `renderList` from `mergedSticky` (only for
a `PRSection`). `rowKey` gains `landed` too, or the row cache would keep serving
the pre-merge row.

`renderItemRow` renders a dim ` landed` tag next to the title, following the
existing `[draft]` tag exactly (including subtracting its width from
`titleRoom`). The merge glyph and `MergedAt` age already come free from
`RenderRow`'s `IsMerged()` branch. Draft and landed can't co-occur — a draft
can't merge.

## Part 2 — `X` cleans up the local branch

### Git helpers (`internal/gh/localgit.go`, new)

Matching `RepoFromGit`'s existing exec-git pattern in `repo.go`:

| Function | Command |
|---|---|
| `BranchExists(dir, branch string) bool` | `git -C dir show-ref --verify --quiet refs/heads/<branch>` |
| `WorktreeForBranch(dir, branch string) (string, bool)` | parses `git -C dir worktree list --porcelain` |
| `RemoveWorktree(dir, path string) error` | `git -C dir worktree remove <path>` |
| `DeleteBranch(dir, branch string) error` | `git -C dir branch -D <branch>` |

`RemoveWorktree` deliberately omits `--force`, so a worktree with uncommitted
work blocks the cleanup instead of discarding it.

`DeleteBranch` uses `-D`, not `-d`: a squash merge — prdash's default — leaves the
branch a non-ancestor of `main`, so `-d` would refuse essentially every branch
this action targets. The force is sound because the action is gated on GitHub
reporting the PR as `MERGED`, which is a stronger authority on "did this land"
than local ancestry.

### Action

A new builtin `cleanup-branch`, joining `copy-branch`/`rerun-failed` in
`ui/actions.go`'s builtin switch, bound by default to `X`:

```go
"X": {Key: "X", Label: "Clean up branch",
    Command: Command{Builtin: "cleanup-branch"},
    Confirm: true, Scope: "single",
    Progress: "Cleaning up", Past: "Branch cleaned up", Fail: "Cleanup failed"},
```

`Refresh` is false — nothing changed on GitHub.

Sequence, on the calling goroutine's closure: resolve the worktree → remove it if
present → delete the branch. Worktree first, since git refuses to delete a branch
checked out in one; and an aborted worktree removal must not leave the branch
deleted, so the order is also the safe failure order.

Failures surface through the normal fail badge:

| Condition | Message |
|---|---|
| PR not merged | `#N is not merged` |
| No local branch | `no local branch <branch>` |
| Worktree is prdash's own `m.dir` | `can't remove the worktree prdash is running in` |
| `git worktree remove` failed (dirty) | git's own error, verbatim |

The cwd guard covers the common case: merging the PR for the branch you are
currently sitting in. It compares cleaned absolute paths and also refuses when
`m.dir` is *inside* the worktree.

### Availability

The `X` hint shows on any **merged** row — landed or on the merged board — and
the action reports `no local branch <branch>` when there is nothing to clean.
Availability is deliberately *not* gated on `BranchExists`: that would exec git
on every row of every frame, and per-frame cost is already the board's tightest
budget. The check happens once, when the key is pressed.

`confirmQuestion` names the branch for this builtin rather than only the PR
number — a force-delete prompt should say what it is deleting:

```
Clean up branch feat/58-approve-action (#61)?  (y/n)
```

## Testing

**`internal/gh`** — against a real repo built in `t.TempDir()` (`git init`,
commit, branch, `worktree add`), skipped when git is absent:

- `BranchExists` true for a real branch, false for an absent one.
- `WorktreeForBranch` finds an added worktree's path; false when the branch has none.
- `RemoveWorktree` removes a clean worktree; refuses a dirty one.
- `DeleteBranch` deletes a squash-style unmerged branch (the `-D` case this exists for).

**`internal/ui`:**

- `applyMergedSticky`: appends a missing sticky PR on the open board; skips one
  the fetch already returned; no-ops on non-open state, on the issue board, and
  when empty.
- Promotion: a successful merge stamps `MERGED`/`MergedAt`, files the snapshot,
  and clears that PR's `ciRerun` stamp; a failed merge files nothing.
- Survival: the row persists across `backgroundRefresh` and a filter switch, and
  is gone after `ctrl+r`.
- Rendering: the `landed` tag appears for a sticky row and not for an ordinary
  merged-board row; `rowKey` difference forces a re-render.
- Cleanup guards: not-merged, missing branch, and the prdash-cwd worktree each
  report their message and run no git mutation.
