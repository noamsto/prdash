# Sectioned default view + omni-filter (issue #29) — Design

**Date:** 2026-07-13
**Status:** Approved (design), pending implementation plan
**Issue:** #29
**Builds on:** the #5 layout sweep (on `main`), the sectioning infra (`section.go`), the local fuzzy filter (`filter.go`), the SWR cache (`prKey`/`hydrate`/`switchToFilter`), and the member picker (`openPicker`/`fetchMembersCmd`).

## Problem

The PR list makes the user assemble the view by hand from discrete toggles: `f`
cycles presets (`mine`/`all`), `F` opens an author picker, `R` a reviewer picker,
`s` cycles state, and `/` does a local-only fuzzy filter. That's five overlapping
controls for one question — *"which PRs should I look at?"* — and none of them is
the obvious default. The user lands on `mine · open` and has to reason about which
knob gets them to "PRs waiting on my review, plus my own, plus everything else."

Issue #29 replaces the discrete filter toggles with **one always-on default view**
plus **one unified omni-filter** (`/`) that subsumes author/text/qualifier
filtering into a single input.

## Already built (out of scope for this spec)

These landed in earlier plans and this spec **reuses** them unchanged unless noted:

- **Sectioning infra** — `internal/ui/section.go`: the `Section` interface, `PRSection`
  with `SetCategorized(prs, cats, order)`, `groupByCategory`, `groupLabel`,
  `Haystacks()`, and the `renderList()` group-header loop that draws headers when
  `PRSection.grouped` is true.
- **State-aware terminal rows** — `RenderRow` state switch (merged/closed glyph +
  merge/close-time age) and `sortPRs(prs, state)` (chronological for terminal
  states, actionability for open). Delivered by the 2026-07-13 merged-view redesign.
- **Local fuzzy filter** — `internal/ui/filter.go`: `haystack(gh.PR)`, `matchIdx`;
  `applyFilter()` feeds `m.section.SetShown(matchIdx(...))`. Today `/` is
  **local-only**.
- **Two-section "mine" view** — `setMine(mine, review)` → `Mine` + `Review requested`
  via `SetCategorized`, reached through the `f` preset cycle; gated by `isMineView()`.
- **Member picker + fetch** — `openPicker("author"|"reviewer")`, `fetchMembersCmd()`,
  `m.members []gh.User`, `hydrateMembers()`, `membersKey`.
- **SWR cache** — `cachedPRs`/`prKey`, `hydrate()`, `switchToFilter()` (hydrate cached
  → instant, then live fetch reconcile), `backgroundRefresh()`, `mineFetchCmd()`
  (fetches `author:@me` + `review-requested:@me` halves), `launchFetchCmds()` prewarm.
  Cache keys scope by repo + filter (`prKey(repo, filter)`).
- **`gh.PR` already carries** `MergedAt`/`ClosedAt`/`State`/`IsDraft`. It has **no**
  `Body` field, and `prFields`/`PRListArgs` do not fetch it. `defaultLimit = 20`.

## Goals

- One always-on default: an open-PR list sectioned **Review requested → Mine → Others**,
  with no toggle needed to reach it.
- One filter entry point (`/`) that handles author, qualifier, and free-text filtering.
- Fewer keys: drop `f` and `F`; keep `s`, `D`, `R`, and every action key unchanged.
- Server qualifiers refetch via SWR (instant cached rows, background reconcile);
  bare text filters the loaded set locally, live.
- Sections apply to the **open** state only; `s` merged/closed keeps author grouping.

## This spec

### 1. Always-on 3-section default view

Replace the `f`-toggled 2-section `mine` preset with a single default the model is
**born in**: open PRs sectioned `Review requested` → `Mine` → `Others`.

**Three sources, reconciled into one categorized set:**

| Section | Source | Fetch |
|---|---|---|
| `Review requested` | the existing `review-requested:@me` query | cached; `mineFetchCmd`'s review half, prewarmed by `launchFetchCmds` |
| `Mine` | `author == <viewer>` from the open list | one `gh pr list --search is:open -L 100` |
| `Others` | the remainder of the open list | same fetch |

Composition (a generalization of today's `setMine`, call it `setSections`):

1. Start from the `review-requested:@me` result → every PR number is category
   `Review requested`.
2. Walk the open list. A PR already in `Review requested` is skipped (first match
   wins — precedence is `Review requested` > `Mine` > `Others`). Otherwise it lands
   in `Mine` when `Author.Login == viewerLogin`, else `Others`.
3. `SetCategorized(all, cats, []string{"Review requested", "Mine", "Others"})`.

`sortPRs(state=="open")` orders rows **within** each section by actionability
(unchanged); `groupByCategory` clusters them in header order.

**Scope of the 3-section partition (chosen answer).** `setSections` runs **only for the
empty-default open view** — `state == "open"` **and no omni server qualifier active**.
The three sections need the `review-requested:@me` set + the viewer login; a composed
query like `is:open label:bug` has no matching `review-requested:@me label:bug` fetch, so
`Review`/`Mine`/`Others` cannot be reconstructed from it without fetches this spec does
not add. Therefore the moment **any** server qualifier is active (or the board leaves
`open`), the partition is abandoned and the board becomes a flat/author-grouped filtered
list via the ordinary `setPRs` path (§2b, §3). Bare-text-only queries (no server
qualifier) keep the sectioned set and flatten per §3.

**Reconcile rendezvous.** The empty default has three async inputs — the
`review-requested:@me` set, the limit-100 `is:open` list, and the viewer login — that
must arrive before `setSections` can paint. Generalize the existing `mineFetchedMsg`
pattern (`prlist.go:818-833`) into a **`sectionsFetchedMsg{state, review, reviewRaw,
open, openRaw}`** produced by a `sectionsFetchCmd` (the successor to `mineFetchCmd`): it
fetches the review half (`review-requested:@me`, limit 20) and the open list (`is:open`,
`openListLimit`), caches both raws under their `prKey`s, and its handler calls
`setSections(review, open, m.viewerLogin)`. The viewer login is resolved separately and
cached (`m.viewerLogin`, §1 login paragraph); until it lands, `setSections` uses the
`author:@me` pre-login fallback for `Mine`, and a later `viewerFetchedMsg` re-runs
`setSections` to re-partition precisely. The handler ignores the message when a server
qualifier is active or `msg.state != m.state` (mirroring `mineFetchedMsg`'s guard at
`prlist.go:823`).

**Resolving the viewer login (chosen answer).** `Mine`/`Others` need the actual
login to partition one list; the `author:@me` *server qualifier* can't do a client
split. Add `gh.FetchViewerLogin(r, dir) (string, error)` → `gh api user --jq .login`,
cached under a new **host-scoped** key `viewer:<host>` (the login is identical across
every repo on a host, so it is not repo-scoped) and hydrated on launch alongside
`hydrateMembers`. Prewarm it in `launchFetchCmds` next to the members fetch — one
cheap call, cached indefinitely. Until it resolves, the partition falls back to
`author:@me`'s *server* result for `Mine` (see below), so the view is never blocked
on it.

- **Considered and rejected — the overlap trick:** derive `Mine` as the PR numbers
  returned by the already-cached `author:@me` query and set `Others = openList \
  (Mine ∪ Review)`, needing no login string. It works and needs no new fetch, but it
  couples `Mine` to a second server query (contradicting "*partition one `gh pr list`
  fetch client-side*") and makes `Others` a set-difference of three fetches that can
  disagree at the edges (limit-20 `author:@me` vs limit-100 open list). The explicit
  login is simpler to reason about and is the honest reading of the issue. We keep the
  cached `author:@me` result only as the **pre-login fallback** for `Mine`.

**Default seed.** `main.go` keeps seeding `is:open` (drop `author:@me` from the
seed) — the default view is now `is:open` partitioned client-side, not a server
`author:@me` query. `NewModel` starts with the 3-section default active.

**State (`s`) interaction (chosen answer).** Sectioning is **open-only**. `s` still
cycles `open → merged → closed`. Leaving `open` drops the 3-section categorization and
reverts to the existing author-grouped terminal view (merged/closed already sort
chronologically and group by author via `groupByAuthor`). Concretely: `setSections`
runs only for `state == "open"`; for terminal states the model uses the plain
`setPRs` path over the `is:<state>` list. Returning to `open` restores the sections.
No `Review requested`/`Mine`/`Others` headers ever appear on a terminal board.

### 2. The omni-filter (`/`)

`/` enters a transient **omni mode** with a focused input bar. The input is parsed on
every keystroke into two disjoint parts:

- **Server qualifiers** — tokens that address the `gh search` query:
  - `@name` → `involves:name` (strip the `@`).
  - any token containing `:` whose prefix is a known search qualifier (`is:`,
    `label:`, `author:`, `review-requested:`, `assignee:`, `involves:`,
    `reviewed-by:`, `head:`, `base:`, …) → passed through verbatim.
  - Composed query = `is:<state>` + the joined qualifiers, keyed and fetched exactly
    like a preset (see SWR below).
- **Bare free-text** — every remaining token (no `:`, not `@`-prefixed). Joined with
  spaces and fed to the existing local fuzzy filter (`applyFilter` → `matchIdx` over
  `Haystacks()`). Live, no fetch.

A new pure helper `parseOmni(input) (serverQuery, bareText string)` does the split;
it is unit-testable in isolation.

#### 2a. Interaction model — how single-key actions are preserved (the hard part)

**The tension.** A focused text input must consume printable keys to build free-text
(`improvement`, `flaky`, …). But action keys (`m`, `r`, `u`, `M`, `a`, `space`, `D`,
`R`, `s`) are also printable single letters. The two cannot fire on the *same* key at
the *same* time — intercepting `m` as "merge" would make the bare word `improvement`
untypeable. Trying to make letters be both text and actions is the wrong problem.

**Chosen model — commit-on-Enter, actions on the committed result.** The omni-filter
is *transient*, not a keyboard-capturing modal like the picker/actions overlay. It
defines the collision out of existence by never running letter-actions while the input
is focused:

- **`/`** enters omni mode and focuses the input.
- **Printable keys** (letters, digits, `@`, `:`, space, backspace) edit the query.
  The server/bare split re-runs each keystroke: bare text re-filters instantly;
  server qualifiers refetch (debounced, §2b).
- **Cursor keys pass through while focused** so the user can scan matches without
  committing: `↑`/`↓`, `ctrl+n`/`ctrl+p`, `pgup`/`pgdn` move the list cursor (and drive
  the side preview) live. (`j`/`k` are printable, so they type — arrows are the
  in-mode movement keys, mirroring the picker.)
- **`enter`** commits: blur the input, exit omni mode, **keep the filter applied**
  (the server query stays fetched/reconciled; the bare text stays in the haystack
  filter). Control returns to the list, where the **entire single-key action
  vocabulary** (`m`/`r`/`u`/`M`/`a`/`space`/`V`/`D`/`R`/`s`/…) now operates on the
  filtered rows. *This* is the sense in which the omni-filter "preserves all single-key
  actions": it hands the keyboard back with its result live, instead of holding it
  hostage.
- **`esc`** clears the query, reverts to the default 3-section view, exits.
- **`backspace` on an empty query** exits the mode (nice-to-have).

This is a deliberate generalization of today's `m.filtering` block (Enter already
blurs-and-keeps; Esc already clears), extended with: the server/bare split, `@`
autocomplete, in-mode cursor passthrough, and the render switch. Rejected alternative:
firing actions from a modifier (`alt+m`) while typing — it satisfies the letter, not
the spirit, adds a second muscle-memory, and no existing key uses `alt`.

#### 2b. Server qualifiers → SWR (cache key + no-clobber reconcile)

When `parseOmni` yields a server query that differs from the one currently fetched:

- **Debounce** the refetch ~250 ms after the last keystroke (a new
  `omniDebounceMsg{seq}` mirroring `detailDebounceMsg`) so we don't spawn a `gh search`
  per character. Bare-text filtering is **not** debounced — it's local and instant.
- Run the existing SWR path: hydrate `cachedPRs(composed, defaultLimit)` for instant
  paint, mark `refreshing`, fetch live to reconcile. This is `switchToFilter` factored so
  it does **not** reset the omni input (today it resets cursor/selection — keep that; do
  not touch `m.filterInput`).
- **Reconcile handler (chosen answer).** A server-qualifier query reconciles through the
  ordinary **`prsFetchedMsg` → `setPRs`** path, **not** `setSections`. Because `SetPRs`
  clears `cats`/`catOrder` (`section.go:53-54`), the result is the flat/author-grouped
  filtered board — never the 3 sections (which only `setSections`/the empty default
  produce). `sectionsFetchCmd`/`sectionsFetchedMsg` is used **only** when the composed
  query is empty (the default view returning as qualifiers are deleted).
- **Cache key:** `prKey(m.repo, composed, defaultLimit)` — the same keying scheme as
  presets, now carrying the explicit `limit` argument §5 adds. A composed omni query is a
  filtered open list, so it fetches at `defaultLimit` (20); each distinct `composed`
  string caches independently and scopes by repo.

**No-clobber guarantee (chosen answer).** A stale server response must not overwrite
the user's in-progress bare-text filter:

1. `setPRs`/`setSections` always end by calling `applyFilter()`, which reads
   `m.filterInput`'s **current** value. So however much bare text the user has typed
   since the fetch was issued, the reconcile re-applies *that* — the fetch supplies
   rows, the live input supplies the filter.
2. `prsFetchedMsg` already drops responses whose `filter != m.filter` (see
   `prlist.go:792`). Reusing this, a response for a superseded composed query (the user
   kept typing and changed a qualifier) is discarded, never painted.

Together: the freshest server rows for the *current* qualifiers, filtered by the
*current* bare text — no clobber, no older query flashing in.

#### 2c. `@` inline member autocomplete

When the cursor sits immediately after an `@` word boundary, show an inline completion
list from `m.members` (already fetched/cached; `fetchMembersCmd` on first need), fuzzy-
narrowed by the partial login. `tab`/`enter`-on-suggestion completes the token to
`@<login>` (which `parseOmni` maps to `involves:<login>`). This reuses the picker's
candidate set, not its modal UI — it renders as a dropdown under the input bar and does
not capture the keyboard beyond `tab`.

### 3. Render switch — flat when bare text is present

The section headers make sense for a browsable list but fight a fuzzy search, where
rank order is the point. The precise rule:

- **Sections (`Review`/`Mine`/`Others` headers) ⇔ empty server query AND no bare text**
  — i.e. only the empty-default open view. This is the sole state that runs
  `setSections`/`SetCategorized`.
- **Any bare text present** → **flat, fuzzy-ranked**: `applyFilter` sets the shown set to
  `matchIdx(...)` order directly and suppresses headers (whether the underlying set is the
  sectioned default or a server-qualifier result).
- **Server qualifier active, no bare text** → the board is the **flat/author-grouped**
  filtered list produced by the `setPRs` reconcile path (§2b) — `SetPRs` has already
  cleared `cats`, so there are no category sections; `setShownOrdered`'s author grouping
  applies as on any non-mine view.

Mechanism: `matchIdx` already returns indices in descending score order.
`setShownOrdered` currently *re-clusters* categorized/multi-author sets, destroying
rank. Add `PRSection.SetForceFlat(bool)` (or thread a `flat` arg): when set,
`setShownOrdered` skips both `groupByCategory` and `groupByAuthor`, sets
`grouped = false`, and keeps `idx` as-is. `applyFilter` calls `SetForceFlat(bareText
!= "")` before `SetShown`. `renderList` already suppresses headers when
`ps.grouped == false`, so no render-loop change is needed.

### 4. Key deltas + legend

| Key | Before | After |
|---|---|---|
| `f` | cycle preset (`mine`/`all`) | **removed** — the default view is always-on; `all`/author filtering is reachable via `/` (`is:` / `@name`) |
| `F` | author picker | **removed** — author filtering is `@name` → `involves:` in `/` |
| `/` | local fuzzy only | **omni-filter** (server qualifiers + bare text + `@` autocomplete) |
| `s` | state cycle | unchanged |
| `D` | drafts toggle | unchanged |
| `R` | reviewer picker | unchanged |
| all actions | — | unchanged |

- **`legendView`** (`prlist.go:1386`): drop the `f`/`F` entries from the `filters` row;
  rewrite the `/` hint from "find" to something like `/ filter (@user, is:, text)`.
  Keep `s`/`D`/`R`. The docked keys/actions panel (`navHintsFor`, `defaultActionHints`)
  loses no action keys — only the removed `f`/`F` hints go.
- The omni bar itself renders a short inline hint (`@user · is: · text`) while focused.

### 5. PR body in the fuzzy haystack + the open-list limit

- **`internal/gh/prs.go`:** add `Body string \`json:"body"\`` to `PR`; add `"body"` to
  `prFields`. `body` is a valid `gh pr list --json` field. **Adding a field to
  `prFields` changes the shape of every cached PR row, so this alone mandates a
  `schemaVer` bump** (per the `prlist.go` schema-version rule) — the bump is required by
  the `Body` addition, independent of the limit change below.
- **`internal/ui/filter.go`:** append `p.Body` to `haystack(p)`'s `parts`.

**Open-list limit — thread a real `limit` through `prKey` (chosen answer).** The open
default view needs the tail, so its `is:open` fetch runs at `openListLimit = 100`, while
the `review-requested:@me` half and the terminal boards stay at `defaultLimit = 20` (the
review section is focused; a bigger tail there is a non-goal).

The blocker: `prKey(repo, filter)` (`prlist.go:293-294`) hardcodes `defaultLimit` into
`cache.Key` and takes **no** limit argument, so a limit-20 and a limit-100 `is:open`
fetch produce a **byte-identical key**. `launchFetchCmds` (`prlist.go:764-765`) already
prewarms `is:open` at limit 20 under that key, so `cacheFresh(prKey(repo,"is:open"))`
would be satisfied by the 20-row prewarm and **skip** the 100-row default fetch — the
default view would silently paint only 20 rows. (`schemaVer` is a single global constant:
a one-time wipe, it never disambiguates two live limits going forward.)

Fix, concretely:

- Give `prKey` a real `limit int` parameter: `prKey(repo, filter string, limit int)` →
  `cache.Key("pr", repo+"\x00"+filter, limit, schemaVer)`. Every caller passes the limit
  it actually fetches/reads at, so the 100-row and 20-row `is:open` entries key distinctly.
- Thread `openListLimit` through **both** the writer (the open-list fetch command) and the
  reader (`hydrate`/`cachedPRs`/`cacheFresh`) for the open default view; the review half,
  terminal fetches, and issue path keep passing `defaultLimit`.
- Update `launchFetchCmds:764-765`'s `is:open` prewarm to fetch **and key at
  `openListLimit`** — the same limit the default view reads — so the prewarm warms the
  entry the view will actually hydrate, not a stale limit-20 one.

## Resolved ambiguities (summary)

1. **Single-key actions while omni is focused** → *commit-on-Enter*. Letters type
   while focused; Enter commits and hands the full action vocabulary back to the list
   with the filter live. Arrows/`ctrl+n,p` move the cursor in-mode; letter-actions never
   fire mid-typing (they'd collide with free-text). §2a.
2. **Viewer login for the Mine/Others split** → new `gh.FetchViewerLogin` (`gh api user
   --jq .login`), cached host-scoped under `viewer:<host>`, prewarmed on launch; the
   cached `author:@me` result is the pre-login fallback for `Mine`. §1.
3. **State (`s`) inside the sectioned model** → sections are open-only. Merged/closed
   drop the categorization and keep chronological author grouping. §1.
4. **Omni SWR cache key + no-clobber** → key is `prKey(repo, composed, defaultLimit)`
   (`prKey` gains a real `limit` arg, §5); `applyFilter` re-reads the live input on every
   reconcile, and the `filter != m.filter` guard drops stale server responses. §2b.
5. **`boardView`/preset migration** → `f`/`F` gone means the preset cycle is dead code.
   Remove `defaultPresets`/`issuePresets` cycling for PRs (keep `issuePresets` only if
   issues still cycle — issues keep `assignee:@me`/`all`? **Decision:** issues are out of
   #29's scope; leave the issue board's `f` behavior untouched by keeping the issue path
   of the `f` handler and removing only the PR path — or, cleaner, drop `f` entirely and
   have issues also default to their `mine`+`all`… **chosen:** remove `f`/`F` for PRs
   only; the issue board is unchanged (its presets and `f` stay). `presetIdx`/`isMineView`
   on the PR side are replaced by the always-on section flag; `boardView.presetIdx`
   remains for the issue board's saved state.)

## Non-goals (v1)

- **Infinite scroll / GraphQL cursors.** The open list is a single limit-100 fetch; to
  see beyond it, type a qualifier (`author:alice`, `label:bug`) that fetches the
  relevant tail. No pagination.
- **Sections on terminal states.** `Review requested`/`Mine`/`Others` apply to `open`
  only.
- **Issues #30** (context-aware `Enter`, log viewer) and **#3** (checks rerun UI, label
  chips, responsive, preview perf) — untouched.
- The issue board's filter behavior — this spec changes the **PR** board only.

## Affected files

- `internal/gh/prs.go` — `PR.Body` field + `"body"` in `prFields`.
- `internal/gh/viewer.go` (new) — `FetchViewerLogin(r, dir) (string, error)`.
- `internal/ui/filter.go` — `p.Body` in `haystack`; `parseOmni` split helper.
- `internal/ui/section.go` — `SetForceFlat` + flat branch in `setShownOrdered`.
- `internal/ui/prlist.go` — `setSections` (3-section composer) + `sectionsFetchCmd`/
  `sectionsFetchedMsg` (the empty-default rendezvous, generalizing `mineFetchCmd`/
  `mineFetchedMsg`), `viewerFetchedMsg` + `m.viewerLogin` hydrate/prewarm, `prKey` gains a
  `limit` arg threaded through `hydrate`/`cachedPRs`/`cacheFresh`, `launchFetchCmds` open
  prewarm re-keyed at `openListLimit`, omni-mode key handling (replacing the `m.filtering`
  block), server-qualifier SWR + `omniDebounceMsg`, `@` autocomplete, render switch call,
  `openListLimit`, `NewModel` default, removal of the `f`/`F` PR handlers, `legendView`
  rewrite.
- `internal/ui/filter_presets.go` — retire the PR preset cycle (`defaultPresets`) usage;
  keep `searchFor`/`splitState`/state cycle and the issue presets.
- `cmd`/`main.go` — seed `is:open` (drop `author:@me`).

## Acceptance criteria (mirroring the issue)

- On launch the PR board shows one open list sectioned `Review requested → Mine →
  Others`, no toggle pressed, populated from the cached review query + one limit-100
  `is:open` fetch partitioned by the viewer login.
- `/` opens the omni-filter. `@ali` offers inline member autocomplete; accepting it
  rewrites the query to `involves:alice` and SWR-refetches (cached-instant, background
  reconcile).
- Typing `label:bug` (a `:` token) refetches server-side; typing `flaky` (bare) fuzzy-
  filters the loaded set locally with no fetch; both together do both.
- The `Review`/`Mine`/`Others` sections appear **only on the empty-default open view**
  (no server qualifier, no bare text). A server-qualifier query (e.g. `label:bug`) shows a
  flat/author-grouped filtered list — not the 3 sections; deleting the qualifier back to
  empty restores the sections. Any bare text flattens the list to fuzzy-ranked (no
  headers).
- `enter` commits the filter and every single-key action works on the filtered rows;
  `esc` restores the default view.
- `f` and `F` no longer bound on the PR board; `s`, `D`, `R`, and all action keys behave
  as before. The legend reflects the new key set.
- Fuzzy search matches on PR body text (e.g. a word only in the description finds the PR).
- `s` merged/closed shows the chronological author-grouped terminal view — no
  `Review requested`/`Mine`/`Others` sections.

## Testing

- **`parseOmni`:** `@alice` → server `involves:alice`, bare ``; `label:bug flaky` →
  server `label:bug`, bare `flaky`; `foo @bob is:open bar` → server
  `involves:bob is:open`, bare `foo bar`.
- **`setSections`:** review `{1}`, open `{1(mine),2(mine),3(other)}`, viewer `me` →
  `1`∈Review, `2`∈Mine, `3`∈Others; a PR in both review and mine stays under Review.
- **Render switch:** bare text present → `PRSection.grouped == false` and shown order ==
  `matchIdx` order; cleared → sections restored.
- **Omni SWR no-clobber:** issue query A, type more bare text, deliver A's response →
  bare filter still applied; deliver a superseded query's response → dropped
  (`filter != m.filter`).
- **Viewer login:** `FetchViewerLogin` parses `gh api user` output → login; cache hit
  skips the call; pre-login fallback partitions `Mine` from `author:@me`.
- **Haystack:** a PR whose match token is only in `Body` is returned by `filterPRs`.
- **Keys:** `f`/`F` are no-ops on the PR board; `enter` in omni mode commits and a
  following `m` runs the merge action on the cursor row.

## Risks

- **Login-fetch latency / failure.** If `gh api user` is slow or fails, `Mine`/`Others`
  can't split precisely. Mitigated by the `author:@me` pre-login fallback and permanent
  caching; a hard failure degrades to "everything in `Others`" rather than an error
  board.
- **Debounce vs. reconcile races.** Rapid qualifier edits can issue overlapping
  fetches; the `filter != m.filter` guard drops stale ones, but the debounce window and
  the guard must be tested together to avoid a flash of stale rows.
- **Limit-100 open fetch cost.** Larger payload than today's 20; acceptable for a single
  open list and gated by the 60 s launch freshness TTL, but worth watching on huge repos.
- **`@` autocomplete ↔ text-input focus.** The dropdown must not steal `enter`/`tab`
  when no suggestion is active, or committing the filter breaks. Needs a clear "is a
  suggestion selected?" gate.
- **Discoverability.** Folding author/preset/text into `/` removes visible affordances
  (`f`/`F`). The legend + inline omni hint must carry that weight; if users can't find
  author filtering, reconsider a one-line hint on the empty board.
