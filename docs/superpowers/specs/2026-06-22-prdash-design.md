# prdash — a lean, worktree-first PR/issue TUI (design — LOCKED v2)

**Date:** 2026-06-22
**Status:** LOCKED (two adversarial passes). Defaults in §"Decisions" taken as approved.
**Author:** Noam

## Purpose

A small, hackable terminal dashboard for GitHub PRs and *my* issues — the slice
of gh-dash actually used, minus the bloat, plus a worktree-first action model, a
bulk "explode my work into per-window worktrees" fan-out, and nicer comment
rendering. Owned project (no upstream AI-policy friction), reusing the existing
bubbletea stack (`wtc`, `tmux-state`) and the `feat/results-cache` cache concept.

Non-goal: re-implementing gh-dash.

## Scope

**MVP:** PR list + my-issues list, both **cwd-scoped to the current repo**
(worktrees are per-repo; cross-repo items can't be fanned out — hard constraint).
Filters: PRs = mine / review-requested / involved; issues = assigned /
created-by-me. Columns — PRs: number · title · author · CI rollup · age; issues:
number · title · author · labels · age. Config-driven actions (single + bulk),
worktree-first. Bulk fan-out → one worktree + tmux window per selected item, same
session. Action view (discoverable menu). Preview with tables + chroma; comments
folded "latest N + older". Instant-launch cache + background refresh.

**Designed-for, not built:** notification kind; inline review-thread comments.

**Out of scope:** multi-repo, notification UI, PR/issue creation, review-comment
authoring, theming UI (inherit lazytmux Catppuccin overlay).

## Architecture

### Data fetch — `gh` CLI, not GraphQL
Lists: `gh pr list --search "<filter>"` / `gh issue list --search` (the `-S`
flag) — returns the full field set incl. `statusCheckRollup` (verified populated
under `--search`), `reviewDecision`, `labels`, `author`, `updatedAt`, `url`, in
one call. Preview: `gh pr view --json comments,reviews,latestReviews`. No GraphQL
client/structs. Cache stores raw `gh` JSON per section.

**Known gaps (verified against `gh --json`):**
- **Inline review-thread comments** (file/line code-review comments) are not in
  `gh --json`. MVP shows conversation comments + review summaries only. Escape
  hatch (later): one `gh api graphql` call for `reviewThreads` — `gh` ships
  GraphQL, no separate client/auth.
- No cursor pagination (refetch at higher `--limit`); no payload trimming. Both
  moot given the cache.

### Section (extensibility seam)
`Section` = `Kind` (`pr` | `issue` | later `notification`) + `Fetch` (a `gh`
invocation) + `RowRenderer` + `Actions` + `CacheKeyPrefix`. MVP registers `pr` +
`issue`. Cache kind-agnostic (rows = `json.RawMessage`).

### Action model (config-driven, worktree-first)
Each action:
```
key:        "enter"
label:      "Open worktree"
command:    {argv: ["wt","switch","pr:{{.Number}}"]}   # OR {builtin: "rerun-failed"} OR {shell: "..."}
exits-tui:  true        # routed via handoff file + tmux run-shell (see Orchestration)
scope:      single      # single | per-selected
confirm:    false
```
Three command forms: **argv** (discrete args, no shell — default, injection-safe),
**builtin** (multi-step Go, see below), **shell** (opt-in `sh -c` for user actions).
Vars: `{{.Number}} {{.HeadRefName}} {{.BaseRefName}} {{.Url}} {{.Repo}}
{{.Author}} {{.Branch}}`.

Defaults:
| key | label | command | exits-tui | scope |
|-----|-------|---------|-----------|-------|
| enter | Open worktree | argv `wt switch pr:{{.Number}}` (PR) / `wt switch -c {{.Branch}}` (issue) | yes | single |
| W | Fan out to worktrees | same per item | yes | per-selected |
| m | Merge (squash) | argv `gh pr merge {{.Number}} --squash` | no (confirm, default No) | single |
| r | Rerun failed | **builtin:rerun-failed** | no | single |
| y | Copy branch | **builtin:copy** (OSC52 + tmux buffer) | no | single |
| o | Open in browser | argv `gh … view {{.Number}} --web` | no | single |
| d | Diff in nvim | argv (diff → nvim) | yes | single |

**Built-ins** (compound/non-templatable):
- `rerun-failed`: `gh run list --branch {{.HeadRefName}} -L1 --json databaseId`
  → `gh run rerun <id> --failed`. (gh's `run rerun` needs a run id, not a repo.)
- `copy`: emit `{{.Branch}}` via OSC52 + `tmux set-buffer` (popup/SSH-safe; bare
  `wl-copy` doesn't survive the popup).

### Orchestration (the lazytmux boundary) — corrected
prdash never drives tmux, and the orchestrator does **not** run inside the popup
(the popup process *is* the wrapper; `wt`/`tmux` calls from inside the `-E` client
misbehave). Instead the **tmux keybinding** is two sequential commands:
```
bind-key G {
  display-popup -E -w 90% -h 90% -d '#{pane_current_path}' \
    -e PRDASH_ACTION_FILE=/tmp/prdash-#{pane_id} <prdash>
  run-shell -b "<prdash-apply> /tmp/prdash-#{pane_id}"
}
```
- prdash, on an `exits-tui` action (single or bulk), appends one line per item to
  `$PRDASH_ACTION_FILE` (`<action>\t<argv-json>`, template-expanded) and exits.
- After the popup closes, tmux runs `<prdash-apply>` via **`run-shell -b`** in the
  **session** context — this is where worktrees/windows get created. It reads the
  file, then deletes it.
- Per-pane file path avoids races between concurrent popups.

### Worktree orchestration (`prdash-apply`, idempotent)
For each line: resolve/create the worktree **idempotently** — branch/worktree
exists ⇒ plain `wt switch` (reuse); else `wt switch -c <branch>` (never `-c` an
existing branch). worktrunk's `post-switch` hook makes the window. Bulk: create
windows detached (no focus bounce across N), land on the first.

### Issue → branch derivation
`{type}/{number}-{slug}`: `type` = `feat` default, overridden by a
`feat|fix|chore|docs|refactor` label; `slug` = lowercased title, non-alnum→`-`,
collapsed, ≤40 chars. e.g. #213 "Seed avatars by id" → `feat/213-seed-avatars-by-id`.

### Action view
Key `a` → overlay listing the section's actions (`key · label · resolved
command`), fuzzy-filterable; `enter` runs honoring `exits-tui`/`scope`.
Auto-generated from the action config. Reuses `fuzzyselect`/bubbles list.

### Multi-select
`space` toggles; `V` selects all. `scope: per-selected` applies to selection, or
the cursor row if none selected.

### Fuzzy filter (client-side, all-fields)
Two distinct search layers:
- **Section filter (server)** — `gh --search` defines *which* items are fetched
  (mine / review-requested / involved). Exact GitHub syntax.
- **Fuzzy filter (client)** — `/` enters a live fuzzy filter over the **loaded**
  rows, matching a composite haystack per item
  (`#{number} {title} {author} {assignees…} {labels…} {headRef} {baseRef}
  {reviewDecision} {CIState}`) with `sahilm/fuzzy`; results re-ranked by score,
  matched runs highlighted. `esc` clears.

Scope limit (stated): fuzzy only sees fetched rows (≤ section `limit`); it is not
a repo-wide search. Optional later: a zero-match fuzzy query offers to re-run as a
server `gh search`. Requires the `assignees` field in the fetch (schemaVer → v2).

### Preview / comments
glamour + ported chroma style, **no pipe-strip** (tables + pipe code render).
Comments timeline = conversation comments + review summaries (no inline threads in
MVP). Interactive list, latest `N` (default 3) expanded, older collapsed to
`▸ {count} earlier comments`. Per-item state.

### Cache (ported concept)
JSON at `$XDG_STATE_HOME/prdash/results-cache.json`, stale-while-revalidate,
7-day prune, atomic write under a full lock (rename-clobber fix from
`feat/results-cache`). Key = `{kind}:{filter}\x00{limit}\x00{schemaVer}` where
`schemaVer` is a hash of the requested `--json` field set, so changing fields is a
clean miss, not a corrupt hydrate. Stores raw `gh` JSON.

## Data flow
launch → (no repo? friendly empty state) → load cache → hydrate rows (instant
paint) → background `gh` fetch → replace + persist → navigate → action: inline
(in-place) OR append handoff + exit → tmux `run-shell` orchestrator creates
worktrees/windows.

## Error handling
- Not in a GitHub repo → empty state with a hint; no crash.
- Cache failure → cold fetch (never fatal).
- `gh`/action non-zero exit → transient footer message; TUI stays unless exits-tui.
- Fetch failure with cached rows → keep rows + stale indicator; else empty/error.
- Worktree/handoff errors surface from `prdash-apply` (outside prdash).

## Testing
- Cache: ported unit tests + schemaVer-miss test.
- Action templating + argv build: var expansion, injection-safety, scope routing.
- Built-ins: `rerun-failed` run-id resolution; `copy` OSC52 emission (table tests).
- Issue branch derivation: type/label + slug rules.
- Comment folding: latest-N split + expand.
- Handoff: file format round-trip. (tmux binding + `prdash-apply` = manual integration test.)
- Section registry: pr + issue wiring.

## Decisions (taken as approved)
1. Issue branch `type` = `feat` default + label override.
2. `gh`-CLI fetch; inline review-threads deferred.
3. my-issues in MVP.
4. Repo `noamsto/prdash`, Nix-bootstrapped.
5. merge squash+confirm(No) · latest-N 3 · action-view `a` · select `space`/`V`.
