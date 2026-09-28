# prdash TUI behaviour sweep — 2026-09-28

A sweep of interaction behaviour in the prdash TUI, looking for state that
flickers or goes stale, actions that silently do nothing or report the wrong
result, focus/cursor surprises, and confusing status messages. Findings are
ranked most-annoying first.

**Method.** `internal/ui/` was read skeleton-first; candidate findings were then
pinned with throwaway headless `Model.Update` tests driven against the same
test doubles the package already uses (`go test ./internal/ui -run TestScratch`).
The scratch test file was deleted after the sweep; the repros below describe the
sequences it exercised. No mutating action was ever issued against a real PR.

**Confidence legend.**

- `confirmed` — reproduced with a scratch test run against this tree (the
  observed assertion/state is quoted in the finding). Where a finding says a
  test was run, that test executed and passed the "bug present" assertion.
- `code-read` — established by reading the code; not reproduced at runtime.

**Excluded / adjacent.** Rows vanishing or being replaced on background refresh,
merged-state stickiness, `applyOptimisticAction`'s merge case and
`runBulkNative` partial success, and the index-based cursor (`setPRs`/`setIssues`
clamp) are being fixed in a parallel task and are not reported here. Finding 6 is
selection drift, which is adjacent to that row-reconciliation work and is marked
as such.

---

## 1. `ctrl+c` (and `q`) do not quit while a prompt, picker, actions palette, legend, or filter is open — `confirmed`

**Symptom.** Ctrl+C is the panic-quit in every terminal app, and the README's
keymap promises `q`/`ctrl+c` quit. Once a confirmation prompt, the reviewer
picker, the actions palette, the legend, or the filter bar is open, neither key
quits: `ctrl+c` in the merge confirmation is read as "no", and in the picker it
is handed to the picker's text input. The only ways out are `esc` (cancel) or
completing the surface first. KEYMAP.md:95 even tells the user to "use
`esc`/`ctrl+c`" inside a picker, so the documented escape hatch does not work.

**Repro.** Press `m` on a PR to raise the merge confirmation, then `ctrl+c` —
the app stays open and the prompt just disappears. Or press `R` to open the
reviewer picker and `ctrl+c` — the picker stays up. (`q` behaves the same.)

**Evidence.**

- `internal/ui/prlist.go:2251-2256` — `if m.pending != nil { if msg.String() == "y" { confirm } ; return m, m.confirmAnswer(false) }`: every key other than `y`, `ctrl+c` included, is "no".
- `internal/ui/prlist.go:2320-2345` — picker: only `esc`/`enter`/`space`/`up`/`down` are special-cased; `ctrl+c` falls to `default` and is forwarded to `m.pick.filter.Update(msg)`.
- `internal/ui/prlist.go:2348-2389` — actions palette: same `default` forwarding to `m.actionFilter`.
- `internal/ui/prlist.go:2392-2405` — legend: `default` only appends single-character keys, so `ctrl+c` is ignored (no quit).
- `internal/ui/prlist.go:2257-2315` — filter input: `default` forwards to `m.filterInput` on both boards.

Contrast the surfaces that do handle it: `internal/ui/prlist.go:2489`
(board `q`/`ctrl+c`), `internal/ui/expanded.go:312` and
`internal/ui/logview.go:311` (both `q`/`ctrl+c` quit).

**Suggested fix.** Hoist a `case "ctrl+c": return m, tea.Quit` check above the
overlay dispatch (or before each overlay's `default`), leaving only `q` as
overlay-local input where the docs already carve it out (the picker).
**Size: small.**

**Scratch test run:** `TestScratchCtrlCDoesNotQuitDuringConfirm` and
`TestScratchCtrlCDoesNotQuitInPicker` — both returned a non-quit command and left
the surface open.

---

## 2. A failed PR-sections fetch is silently dropped: no error, spinner never stops — `confirmed`

**Symptom.** On the default open PR board the list is fetched as three
concurrent halves (review-requested, reviewed-by-me, wide). If any half fails
(rate limit, network blip), the failure is discarded: no error is shown, the
header spinner keeps spinning, and the board stays on whatever it had. The user
sees a permanently-refreshing board and has no idea a fetch failed. The issue
board already fixed exactly this; the PR board did not.

**Repro.** `Model.Update(fetchFailedMsg{err, mode:"pr", filter: reviewF})` on a
`sectionsDefault()` board — `m.err` stays `nil` and `m.refreshing` stays `true`.
Or, live: launch with a throttled/offline GitHub and watch the default open board
spin forever.

**Evidence.**

- `internal/ui/prlist.go:1296-1302` — `sectionsFetchCmd` returns `fetchFailedMsg` with the **failing half's** filter (`reviewF`/`reviewedF`/`open`), e.g. `is:open review-requested:@me`.
- `internal/ui/prlist.go:1934-1949` — `fetchFailedMsg` guards with `if msg.filter != "" && msg.filter != m.filter { return m, nil }` **before** clearing `m.refreshing` and setting `m.err`. On a sections-default board `m.filter` is `is:open`, which never equals a half's filter, so the arm bails.
- `internal/ui/prlist.go:1338-1344` — `issueSectionsFetchCmd` reports `boardFilter := searchFor("issue", "open", "")` with the explicit comment that "a half's filter never equals `m.filter`, so the handler's filter guard would bail … and a disabled repo would show 'Loading…' forever". The PR path is missing that same fix.

**Suggested fix.** Mirror the issue side: report `searchFor("pr", state, "")`
(the board filter) for every half's failure, or give `fetchFailedMsg` an explicit
"this belongs to the current view" flag and stop overloading `filter` for the
prewarm guard. **Size: small.**

**Scratch test run:** `TestScratchSectionsFailureDropped` — asserted the half
filter differs from `m.filter`, then that after the failure `err == nil` and
`refreshing == true`.

---

## 3. Any transient fetch error latches for the session and later paints an "Error:" screen over an empty board — `confirmed`

**Symptom.** `Model.err` is set by a failure and then never cleared by a
successful fetch — only by toggling boards. While rows are shown the stale error
is invisible (the board short-circuits to the error screen only when the shown
set is empty), so it lurks. The next time the shown set is legitimately empty —
a fuzzy filter with no matches, or a state with zero PRs — the user gets
`Error: <an old, unrelated failure>` instead of the proper "No open PRs."
empty state, with no way to clear it except Tab and back.

**Repro.** `Update(fetchFailedMsg{err: errors.New("detail prefetch boom")})`,
then repopulate rows successfully, then `setPRs(nil)` — `m.board()` renders the
old error and suppresses "No open PRs.".

**Evidence.**

- `internal/ui/prlist.go:1949` — `fetchFailedMsg` sets `m.err = msg.err`.
- `internal/ui/prlist.go:1662` — the **only** `m.err = nil` in the package, inside `toggleMode`.
- `internal/ui/prlist.go:1859`, `1875`, `1891`, `1909` — the success arms (`prsFetchedMsg`, `issuesFetchedMsg`, `sectionsFetchedMsg`, `issueSectionsFetchedMsg`) never clear `m.err`.
- `internal/ui/prlist.go:2778-2780` — `board()` returns the error screen whenever `m.err != nil && m.section.Len() == 0`, before `renderList`'s proper empty-state hint (`internal/ui/prlist.go:776-786`) can run.

Note the same arm is reached by background detail prefetches
(`internal/ui/preview.go:131-146` `batchDetailCmd` returns a bare
`fetchFailedMsg`), so a detail query failing is enough to poison the board's
error state for the rest of the session.

**Suggested fix.** Clear `m.err` on every successful list/section fetch (and,
better, keep a separate `listErr` from a background `detailErr` so a detail
failure never becomes a board-level error). **Size: small.**

**Scratch test run:** `TestScratchStaleErrorOverEmptyBoard` — confirmed `m.err`
survives a successful `setPRs`, and then that `m.board()` shows the old text and
not the empty-state hint.

---

## 4. Bulk actions report the number of rows attempted, including benign no-ops that changed nothing — `confirmed`

**Symptom.** A bulk ready/draft (or auto-merge) action counts a row as
"actioned" even when its pre-check makes it a no-op. Select one draft and one
already-ready PR, focus the draft, press `M`: the badge reads
`Converted to draft ×2` (or `Marked ready ×2`) although exactly one PR changed.
The count is the number of rows the action touched, not the number it changed,
so the status message overstates the result.

**Repro.** Two PRs, one `IsDraft: true` and one ready; select both, focus the
draft, run `action.DefaultPRActions()["M"]`. `MarkReady`/`ConvertToDraft` is
called once for the non-no-op row, but `actionStatus.ok` is `… ×2`.

**Evidence.**

- `internal/ui/actions.go:663-766` — `runBulkNative` builds one closure per selected row and sets `n := len(calls)` at `:720`; a closure for an already-ready/already-draft (or already-auto-merging) row returns `nil` without calling the mutation source (`internal/ui/actions.go:342-350`, `:361-366`).
- `internal/ui/actions.go:768-775` — `statForBulk` bakes `×n` into the success wording from that attempted count.
- Related overcount on the copy path: `internal/ui/actions.go:212` labels with `len(m.selectedOrCursor())` while `copyPayload` (`:88-103`) silently drops rows whose payload is empty, so "Copied N branches" can exceed the number actually copied.

**Suggested fix.** Have `runBulkNative` distinguish "attempted" from "changed"
(e.g. closures report a changed/no-op result) and word the settle from the
changed count; only fall back to attempted when the action cannot tell.
**Size: medium.**

**Scratch test run:** `TestScratchBulkReadyCountIncludesNoops` — one real
mutation call, badge `"Converted to draft ×2"`.

---

## 5. A concurrent action's status overwrites an in-flight mutation's result, so the mutation's success is swallowed — `confirmed`

**Symptom.** `Model.actionStatus` is a single slot. If a foreign action opens
the same slot while a mutation is in flight — a log-view copy, an expanded-view
check rerun, a status paint — the in-flight mutation's `actionDoneMsg` settles
the *foreign* stat instead of its own. Concretely: start a merge, then copy a log
line; when the merge settles the badge reads "Copied line", the merged row is
never optimistically patched (because the merged/native fields live on the
discarded stat), and the post-mutation refresh is skipped — the PR still looks
open until a manual `ctrl+r`. The GUI lied about a merge.

**Repro.** `m.runBulk(merge)` to put a merge stat in flight, overwrite
`m.actionStatus` with a settled copy stat (what the log-view paths do), then
deliver the merge's `actionDoneMsg`. The foreign stat absorbs the settle and
`actionStatus.merged`/`.native` are empty.

**Evidence.**

- `internal/ui/prlist.go:2121-2124` — `actionDoneMsg` mutates whatever `m.actionStatus` currently is (only a `nil` check), then reads `m.actionStatus.merged`/`.refresh`/`.native` for the optimistic patch (`:2143-2169`).
- `internal/ui/actions.go:221-227, 234-286`; `internal/ui/logview.go:355-383`; `internal/ui/expanded.go:352-371` — the foreign actions that overwrite `m.actionStatus` directly (the comments at `internal/ui/actions.go:798-801` and `internal/ui/cascade.go:254-255` list "seven sites" and work around the same hazard for the cascade run by re-asserting `r.stat` in `cascadeSettleCmd`, `internal/ui/prlist.go:1467-1474`; the non-cascade paths have no such re-assertion).

**Suggested fix.** Carry an action token/id in `actionDoneMsg` and have the
handler drop a settle whose token does not match the current `actionStatus`
(the cascade's re-assert is the existing precedent). **Size: medium.**

**Scratch test run:** `TestScratchForeignActionClobbersInflightBadge` — after
the settle, `actionStatus.ok == "Copied line"` and the merge's optimistic data is
gone.

---

## 6. Multi-selection points at row indexes, so it drifts to different PRs when the list re-categorizes — `confirmed` *(adjacent to the row-reconciliation work)*

**Symptom.** `selection` stores shown-row indexes, not PR numbers. When the
viewer login resolves, `setSections` re-partitions rows into Review/Mine/Others
and reorders them, but does **not** clear the selection. A selection the user
made before the login resolved now silently points at different PRs; a subsequent
bulk action (merge, approve, update-branch) hits the wrong rows. This is the
dangerous member of the index-based-state family, not just a cosmetic one.

**Repro.** Boot with an unresolved viewer, select a row that will move group
when the viewer resolves, then deliver `viewerFetchedMsg` (or call
`setSections(..., viewer)`). The selected index now names a different PR.

**Evidence.**

- `internal/ui/select.go:3` — `type selection struct{ set map[int]bool }`, indexes into the shown set.
- `internal/ui/prlist.go:421-470` — `setSections` rewrites categorization and calls `m.applyFilter()`/`m.homeCursorOnMine()` without `m.sel.clear()`; the same is true of `setIssueSections` (`:471-517`).
- `internal/ui/prlist.go:1960-1984` — `viewerFetchedMsg` calls `m.setSections(...)` from cache to re-split once the login lands.
- Callers that *do* clear (`prsFetchedMsg` `:1865`, `sectionsFetchedMsg` `:1899`, `D` `:2459`) show the intended discipline; the viewer re-split is the gap.

**Suggested fix.** Anchor the selection by PR (or issue) number rather than
index, or at minimum clear it in `setSections`/`setIssueSections`. The
number-keyed approach folds into the row-reconciliation work. **Size: medium.**

**Scratch test run:** `TestScratchSelectionDriftsOnViewerResolve` — the selected
index still reported "selected" but now named a different PR.

---

## 7. A failed member-list fetch leaves the reviewer picker stuck on "Loading…" with no error — `confirmed`

**Symptom.** `R` opens the reviewer picker and, if the assignable-users fetch
fails, the picker shows "Loading…" forever. The failure lands in the board-level
`m.err` (behind the floating picker), so there is no error text inside the
picker and nothing to retry. The user is left on a modal that can never populate.

**Repro.** Open the picker with no cached members, then deliver
`fetchFailedMsg{err}`: the picker stays open and still renders "Loading…".

**Evidence.**

- `internal/ui/prlist.go:1695-1702` — `fetchMembersCmd` returns a bare `fetchFailedMsg{err}` on failure; there is no `membersFetchedMsg`, so `m.pick.cands` is never filled.
- `internal/ui/prlist.go:1951-1958` — `membersFetchedMsg` is the only writer of `m.pick.cands` (guarded by `m.showPicker`), so nothing populates it on the failure path.
- `internal/ui/picker.go:78-84` — `pickerView` renders the nil-cands case as `Loading…` unconditionally.

**Suggested fix.** Add a members-error field (or a `membersFetchedMsg` carrying
`err`) and have `pickerView` render a failure message plus a retry hint instead
of "Loading…". **Size: small.**

**Scratch test run:** `TestScratchPickerStuckLoadingOnMemberFetchFailure` — the
open picker still contained "Loading" and `m.err` was the only trace.

---

## 8. An earlier action's status-clear timer wipes a later action's badge early — `confirmed`

**Symptom.** Every settle schedules a 3-second `actionClearMsg`. The arm clears
whatever settled status is present, regardless of which action scheduled it. If
two actions settle within 3s of each other, the first one's timer clears the
second one's badge — often before the user has read it.

**Repro.** Settle action A, then settle action B (both within A's 3s window),
then deliver A's `actionClearMsg`: B's badge is gone.

**Evidence.**

- `internal/ui/prlist.go:2874-2877` — `clearStatusCmd` is a flat 3-second `tea.Tick` with no identity.
- `internal/ui/prlist.go:2222-2225` — `actionClearMsg` clears `m.actionStatus` whenever it is `settled`, with no check that the tick belongs to the current stat.

**Suggested fix.** Tag the timer with a per-action sequence/generation (or a
pointer to the stat it belongs to) and only clear when it still matches.
**Size: small.**

**Scratch test run:** `TestScratchActionClearWipesLaterBadge` — after the
tick, `actionStatus` was `nil`.

---

## 9. `p` (unfold preview) is accepted on the issue board but does nothing — `confirmed`

**Symptom.** `p` toggles `previewExpanded`, but only the PR Overview tab's
timeline reads it. On the issue board the key is still handled, flips the flag,
and changes nothing on screen — a silent no-op. Because a round-trip through the
issue board resets the flag (`internal/ui/prlist.go:1657`), the no-op also
discards a fold the user set on the PR board.

**Repro.** On the issue board, press `p`: `previewExpanded` becomes `true` and
the rendered frame is byte-identical.

**Evidence.**

- `internal/ui/prlist.go:2499-2502` — the `p` case has no `m.mode` guard.
- `internal/ui/preview.go:381` — the only reader of `previewExpanded`, inside `renderOverview` (PR Overview only); `issuePreviewParts` (`internal/ui/preview.go:388-401`) never consults it.

**Suggested fix.** Guard `p` with `if m.mode != "pr" { return m, nil }`, matching
`right`/`left`/`1`-`6`/`R`/`D`, or give the issue preview a fold to toggle.
**Size: small.**

**Scratch test run:** `TestScratchIssueBoardPIsNoOp`.

---

## 10. Moving the list cursor with the Checks tab open leaves the check cursor stale — `confirmed`

**Symptom.** With the side layout showing the Checks tab, the check highlight is
`m.checkCursor`. Moving the list cursor to another PR (`j`/`k`/arrows) does not
reset it; when the new PR has fewer checks the highlight vanishes entirely, and
`r`/`enter` then operate on an out-of-range or unexpected check until the user
presses `j`/`k` inside the tab.

**Repro.** Side layout, Checks tab, set `checkCursor` beyond the next PR's check
count, then move the list cursor: `checkCursor` is unchanged and `renderChecks`
highlights nothing.

**Evidence.**

- `internal/ui/prlist.go:2503-2509` — board `down`/`up` call `moveCursor` and `++detailSeq` but never touch `m.checkCursor`; `right`/`left`/`1`-`6` do reset it (`:2511`, `:2523`, `:2530`).
- `internal/ui/expanded.go:71-85` — `renderChecks` highlights only when `i == cursor`, so an out-of-range cursor highlights no row; `moveCheckCursor` (`:328-338`) clamps only once the user is already inside the tab.

**Suggested fix.** Reset `m.checkCursor = 0` wherever `m.cursor` changes (fold it
into `moveCursor`/`setPRs`/`setSections`). **Size: small.**

**Scratch test run:** `TestScratchCheckCursorNotResetOnCursorMove`.

---

## 11. `D` (hide drafts) is not part of the saved board view, so it resets on a board round-trip — `confirmed`

**Symptom.** Set hide-drafts on the PR board, press Tab twice (issues and back):
drafts are shown again. The per-board `boardView` saves state/body/filter but not
the draft filter, so the preference silently resets.

**Repro.** `m.hideDrafts = true; toggleMode(); toggleMode()` → `false`.

**Evidence.**

- `internal/ui/prlist.go:30-32` — `type boardView struct { state, body, filter string }` carries no `hideDrafts`.
- `internal/ui/prlist.go:1641-1660` — `toggleMode` saves/restores only `state/body/filter` and resets `m.hideDrafts = false`.
- `internal/ui/prlist.go:2450-2455` — `D` toggles `m.hideDrafts` and re-applies the filter; nothing persists it per board.

**Suggested fix.** Add `hideDrafts` to `boardView` and restore it in
`toggleMode`. **Size: small.**

**Scratch test run:** `TestScratchHideDraftsResetsAcrossBoardToggle`.

---

## 12. Keys that are PR-only silently no-op on the issue board rather than doing something sensible — `code-read`

**Symptom.** `R` (assign reviewers) and `D` (hide drafts) are bound to the
issue board's raw key handler and return `nil` immediately
(`internal/ui/prlist.go:2446-2452` for `D`, `:2457-2463` for `R`), so pressing
them on the issue board does nothing and gives no feedback. `r` (rerun checks),
`M`, `L`, `X`, and `A` are absent from `action.DefaultIssueActions`
(`internal/action/defaults.go:78-95`) and therefore also fall through to no-ops
since they are not in the issue action map. The documented "PR only" wording
exists in the README but not in the TUI, so the key just feels dead.

**Repro.** On the issue board, press `R`: no picker, no status message.

**Evidence.** `internal/ui/prlist.go:2446-2452` (`D`) and `:2457-2463` (`R`) return `nil` on the issue board; `internal/action/defaults.go:78-95` omits the PR mutation keys; no issue-board status hint names the unsupported keys.

**Suggested fix.** Give the issue board's no-op keys a one-line status
("Assigning reviewers is PR-only") or remove them from the hinted key set so the
footer does not imply they work. **Size: small.**

**Confidence: code-read** (the no-op is direct from the guard; no runtime repro
was needed).

---

## Summary

| # | Finding | Confidence | Size | Category |
|---|---------|-----------|------|----------|
| 1 | `ctrl+c`/`q` swallowed by prompts, picker, palette, legend, filter | confirmed | small | surprising keys |
| 2 | PR-sections fetch failure dropped; spinner never stops | confirmed | small | stale/status |
| 3 | `m.err` latches; stale error paints over empty board | confirmed | small | stale state |
| 4 | Bulk count includes no-op rows | confirmed | medium | wrong result |
| 5 | Foreign action swallows an in-flight mutation's result | confirmed | medium | wrong result |
| 6 | Index-based selection drifts on viewer re-split *(adjacent)* | confirmed | medium | wrong target |
| 7 | Reviewer picker stuck on "Loading…" after member fetch failure | confirmed | small | no-op/status |
| 8 | Earlier action's clear timer wipes a later badge | confirmed | small | status lifecycle |
| 9 | `p` no-op on the issue board | confirmed | small | surprising keys |
| 10 | Check cursor not reset on list cursor move | confirmed | small | cursor |
| 11 | `D` hide-drafts resets on board round-trip | confirmed | small | stale state |
| 12 | PR-only keys silently no-op on the issue board | code-read | small | surprising keys |

Eleven of the twelve (all but #12) are `confirmed`; the report's confidence
threshold (at least half `confirmed`) is met.
