# Declined: merged-PR grace period in the open list

Decision record. **Outcome: declined — the need it addressed was met differently
by #66 / PR #67.** Written 2026-07-15, never implemented; preserved here because
it was the sole content of the `feat/merged-pr-grace` branch, now deleted.

## Why declined

The requirement, as stated when #66 was scoped, was narrower than this design: a
PR **prdash itself merged** should not vanish on the automatic post-merge
refetch, and should stay until an explicit `ctrl+r`. That is local,
event-driven and untimed. This spec is none of those — it is a timed window fed
by a companion `is:merged` query, whose whole purpose is to catch merges prdash
did not perform (a teammate's, or one done on github.com).

What #67 shipped instead: a `mergedSticky` overlay, re-injected the way
`applyCIRerun` already overlays local post-action state, cleared only by
`ctrl+r`. No companion query, no expiry tick, nothing persisted.

The design below also predates the githubv4 rewrite (#47) — it counts `gh` calls
and searches via `gh pr list` — so it would need reworking against `GraphSource`
regardless.

**If it is ever revisited**, the trigger would be wanting *other people's* merges
to linger too, which #67 deliberately does not do. The companion-query and
expiry-tick mechanics below are still the right shape for that, and the two
approaches can coexist: neither depends on the other.

---

## Problem

When a PR merges it vanishes from the open list instantly. The open list is
just the result of `gh pr list --search "is:open …"`, and a merged PR no longer
matches `is:open`. There is no moment where the user sees "it landed" — the row
is simply gone on the next fetch. The user wants a merged PR to linger in the
open list for a short grace period (default ~1 minute) as a visual confirmation,
then drop out.

## Approach

The feature is **additive**, not a filter tweak: because merged PRs are absent
from the open query's result set, the open view must *pull them back in* with a
companion `is:merged` query and drop each one once it ages past the window.

The window is computed directly from each PR's `MergedAt` timestamp
(`now − MergedAt < grace`) — no local "first seen" bookkeeping, no persistence.
Any PR merged within the window shows, whether merged from prdash, github.com,
or by a teammate.

### 1. Configuration

- Env var **`PRDASH_MERGED_GRACE`**, parsed with `time.ParseDuration`
  (`"1m"`, `"90s"`, …). Default **1 minute** when unset or unparsable.
- A value `≤ 0` **disables** the feature: the open view behaves exactly as
  today (no companion query, no timer).
- Read in `main.go`; applied to the model via a new `m.SetMergedGrace(d)` setter,
  matching the existing `SetRunner` / `SetRepo` pattern. `NewModel`'s signature
  and its tests stay untouched.
- No CLI flag: prdash ships no flags today and configures via env
  (`XDG_STATE_HOME`); a flag would be the odd one out.

### 2. Companion fetch (open state only)

Only when `m.state == "open"` and `grace > 0`. The companion query is
`is:merged <body> merged:>=<cutoff>`, `cutoff = now − grace` formatted RFC3339.
Results are additionally filtered client-side to `now − MergedAt < grace`
(exact, and robust to GitHub search's coarser date matching), then appended to
the open PRs before they reach the section.

- **All / custom-author view** (single fetch via `fetchCmd`): one extra query
  with the same body.
- **Mine view** (dual fetch via `mineFetchCmd`, `author:@me` +
  `review-requested:@me`): one extra merged query per half, each blended into its
  own category ("Mine" / "Review requested"). So 4 gh calls instead of 2 while
  in the open mine view.

Merged and open states are the only ones affected — the merged and closed boards
are unchanged.

### 3. Placement & rendering

Merged PRs already render correctly wherever they land: the mauve merge mark, the
merged glyph, and the `MergedAt`-based age all key off `IsMerged()` in
`RenderRow`. No rendering change is needed.

**Ordering: natural placement.** The blended merged PRs sort and group through
the existing open-view machinery (actionability rank, author/category grouping),
distinguished only by the merge glyph. They are *not* pinned to the top — pinning
fights the author-grouping (a merged PR by author X belongs in X's group) for
little gain, and the glyph already makes a landed PR recognizable wherever it
sits. Pinning remains a cheap follow-up if it feels wrong in practice.

`prRank` is unchanged; a merged PR falls through to whatever rank its CI/review
signals imply, which is acceptable for a transient row.

### 4. Aging-out

prdash has no unconditional periodic refresh — `backgroundRefresh` runs only
during CI polling (30s, while checks run), after an action, or on manual refresh.
So a merged row would otherwise linger until the next such event.

When the open view holds ≥1 grace-period PR, prdash arms a **one-shot
`tea.Tick`** at the earliest PR's expiry (`grace − (now − MergedAt)`). When it
fires it triggers a `backgroundRefresh`; the companion query's cutoff has moved,
the expired PR no longer matches (and is filtered out client-side regardless),
and the row drops. The tick is only re-armed while grace PRs remain, so the loop
self-terminates.

## Components touched

- `main.go` — read `PRDASH_MERGED_GRACE`, call `SetMergedGrace`.
- `internal/ui/prlist.go` — `mergedGrace` field + `SetMergedGrace`; companion
  fetch in `fetchCmd`/`mineFetchCmd` (or the message handlers that blend
  results); the expiry `tea.Tick` arm/handle.
- `internal/ui/filter_presets.go` — helper to build the `is:merged … merged:>=`
  companion query from a body + cutoff (mirrors `searchFor`).
- New message type for the merged-grace tick (mirrors existing tick msgs in
  `messages.go`).

## Testing

- `searchFor`-style query builder: correct `is:merged <body> merged:>=<ts>`.
- Client-side window filter: PR merged inside window kept, outside dropped,
  boundary handled.
- Blend: companion merged PRs appended to open PRs in `setPRs` / `setMine`
  paths; grace = 0 → no companion, no timer; non-open state → no companion.
- Expiry tick: armed when a grace PR is shown, fires a refresh, not armed when
  none remain.

## Out of scope

- Pinning merged rows to the top of the open list.
- A CLI flag (env var only).
- Any change to the merged / closed boards.
- Persisting grace state across prdash restarts (derived from `MergedAt`, so
  restart re-derives it for free).
