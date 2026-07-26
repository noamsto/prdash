# Confirm auto-merge / mark-ready on PRs that aren't yours

## Problem

`A` (auto-merge, squash) and `M` (mark ready) fire the instant the key is
pressed. Both are "arm it and walk away" actions: they change a PR's fate
without the operator staying to watch. Doing that to *someone else's* PR by a
stray keystroke is the accident worth guarding against.

Plain merge (`m`) already always confirms, so it is out of scope. Lower-stakes
mutations (`u` update-branch, `r` rerun) stay immediate.

## Behavior

Applies to `A` and `M` only.

- **Single target** (cursor row, or a one-PR selection): prompt **only when the
  PR's author is not the viewer**.
- **Bulk** (two or more selected rows): **always** prompt, ownership-agnostic —
  the fan-out itself earns a beat.
- Confirming runs the action across the whole target set (existing `runBulk`
  behavior — unchanged). Cancelling changes nothing.

### Ownership-unknown fallback

The viewer's login (`m.viewerLogin`) is fetched once at launch and may be empty
for the first seconds. When it is empty, a single-target `A`/`M` **prompts** —
"can't prove it's mine" is treated the same as "not mine". This is conservative
by design: at worst it shows one extra prompt during the launch window, rather
than silently arming auto-merge on a PR whose ownership is unproven.

## Approach

Chosen among three:

| Approach | Idea | Verdict |
|---|---|---|
| **A. New `ConfirmOthers bool` on `Action`** | Declarative flag set on `A`/`M`; the UI's `startBulk` owns the ownership check | **Chosen** — mirrors the existing `Confirm bool`, unit-testable, survives user-remapped keys |
| B. Hardcode keys `"A"`/`"M"` in `startBulk` | No struct change | Rejected — actions are user-configurable; buries policy in the UI, not testable |
| C. Replace `Confirm bool` with a `ConfirmMode` enum | Unify always/others/never | Rejected — YAGNI; migrates a working field for no gain today |

The `Action` declares *intent*; the UI owns the *ownership check*, because
`viewerLogin` and the live selection are UI state. Per-selected actions already
route through `startBulk` from both dispatch sites (direct key and action menu),
so the decision centralizes there.

## Changes

1. **`internal/action/action.go`** — add `ConfirmOthers bool` to `Action`.
   Doc comment: "prompt before running when a target PR was authored by someone
   other than the viewer, or when the action spans a bulk selection."

2. **`internal/action/defaults.go`** — set `ConfirmOthers: true` on the `A`
   (auto-merge) and `M` (mark ready) actions.

3. **`internal/ui/actions.go` `startBulk`** — extend the "should I prompt?"
   decision. Today it prompts on `a.Confirm || overThreshold`. Add: when
   `a.ConfirmOthers`, prompt if `len(targets) > 1`, or if the single target's
   `Author != m.viewerLogin` (empty `viewerLogin` → prompt). All conditions fold
   into the one existing `m.pending = &a; return nil` branch.

4. **`internal/ui/prlist.go` `confirmPanel`** — base the wording on the target
   *count* rather than `Scope`, and name the author on a foreign single target:
   - single foreign: `Auto-merge (squash) #123 by alice?`
   - bulk: `Auto-merge (squash) for 3 PRs?` (unchanged shape)

No new dependencies. The ownership signal already exists: `m.viewerLogin`
(prlist.go) vs `section.VarsAt(i).Author` (the PR's author login).

## Testing

New cases in `internal/ui/actions_test.go`:

- Single **own** PR + `ConfirmOthers` → runs, no `m.pending`.
- Single **foreign** PR → sets `m.pending`.
- **Bulk** (two own PRs) → sets `m.pending` (always-in-bulk).
- Empty `viewerLogin` on a single PR → sets `m.pending`.
- Plain merge (`m`, `Confirm: true`) unaffected — still always confirms.

A `confirmPanel` wording assertion (single-foreign names the author; bulk shows
the count) in `prlist_test.go` or `actions_test.go` as fits the existing layout.
