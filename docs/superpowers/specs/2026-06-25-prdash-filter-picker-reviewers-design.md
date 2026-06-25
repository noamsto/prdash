# prdash — filter switching, member picker & reviewer assignment (design)

**Date:** 2026-06-25
**Status:** Approved (brainstormed; pending implementation plan)
**Author:** Noam
**Builds on:** the merged Plans 1–4 and the A/B/C TUI redesign.

## Purpose

Today the PR list is locked to a single hardcoded query (`is:open author:@me`)
seeded in `main.go`. This feature gives the user control over **who** and **what**
the list shows, and adds a **reviewer-assignment** action — all sharing one
reusable multi-select **member picker**. It also surfaces the "no reviewers
requested" gap in the quick window so the user sees, and can fix, an un-reviewed
PR in one place.

Three user-facing capabilities, one design:

1. **Filter presets** — cycle the list query between common views.
2. **Member picker** — a generic multi-select overlay of assignable users.
3. **Reviewer assignment** — toggle a PR's requested reviewers via the picker.

Non-goal: persisting the chosen filter across launches; config-driven preset
lists (built-in for now); a full reviewer-suggestion engine.

## A. Filter presets — cycle with `f`

Replace the single `m.filter` string with an ordered preset list. Each preset is
a name + a gh `--search` query:

| Preset | Search |
|---|---|
| `mine` | `is:open author:@me` |
| `review-requested` | `is:open review-requested:@me` |
| `all` | `is:open` |

- `f` advances to the next preset (wraps to the first); refetches the list,
  resets the cursor to 0, and the header shows the active preset name + count.
- The cache is **already keyed by the filter string**
  (`cache.Key("pr", filter, …)`), so each preset caches independently and
  stale-while-revalidate keeps working unchanged.
- `main.go` seeds the default preset (`mine`).

**State.** A small value type holds the presets and the active index; an
`activeFilter() string` derives the current query. The author filter (B/C below)
is a distinct mode — entering it sets a free-form filter string; pressing `f`
returns to the preset cycle.

## B. Member picker — reusable multi-select overlay

A new overlay mode mirroring the existing `showActions` pattern: a scrollable
list of assignable users (`login` + display name), a fuzzy-filter text input,
`space` toggles a row, `enter` confirms, `esc` cancels.

The picker is **generic** — it knows nothing about filtering vs assigning. It is
configured with:

- `candidates []gh.User` — the people to choose from,
- `checked map[string]bool` — pre-selected logins,
- an `onConfirm(selected []string)` continuation.

**Member source.** `gh api graphql` →
`repository(owner,name){ assignableUsers(first:100){ nodes{ login name } } }` —
exactly the set GitHub permits as reviewers/assignees. Lazy-fetched on first
open, cached under a new `members:<repo>` key. A fetch failure renders an error
line **inside the overlay**; `esc` closes it. The list is never blanked.

**New units.**
- `internal/gh/members.go` — `FetchAssignableUsers(r Runner, dir string) ([]User, error)`.
- `internal/ui/picker.go` — the generic picker model (state, update, view),
  decoupled from its callers.

## C. The two pickers

- **`F` → filter by author.** Opens the picker, nothing pre-checked. On confirm,
  sets the filter to `is:open ` + one `author:<login>` term per pick (replacing
  the active preset; header shows `author: alice, bob`). An empty selection is a
  no-op. `f` cycles back to the presets.
- **`R` → assign reviewers** (acts on the cursor PR). Pre-checks the PR's current
  `ReviewRequests` logins (from the cached detail; if detail isn't cached yet,
  fetch it first). On confirm, **diffs** the selected set against the current set
  and runs a single `gh pr edit <number> --add-reviewer <added…> --remove-reviewer <removed…>`,
  omitting whichever flag is empty. No change → no command. Then refetch the list
  so the review column updates.

## D. Quick-window "no reviewers" indicator

The quick window (side preview / triage card) gains a **reviewers line** derived
from `PRDetail.ReviewRequests`:

- empty → `⚠ no reviewers` in the warn (pending) color, to draw the eye;
- otherwise → a dim `reviewers: alice, bob`.

This sits with the triage card so an un-reviewed PR is visible exactly where `R`
assigns one.

## Key bindings (all currently free)

| Key | Action |
|---|---|
| `f` | cycle filter preset |
| `F` | open member picker → filter by author(s) |
| `R` | open member picker → assign reviewers (cursor PR) |

(`r` lowercase stays "rerun failed"; reviewers is `R`.)

## Data flow

- `f`: cycle preset → set filter → `fetchCmd` (cache-first) → list updates.
- `F`: open picker(authors, none checked) → confirm → set author filter → fetch.
- `R`: open picker(reviewers, current pre-checked) → confirm → `gh pr edit` → refetch.

## Error handling

- Member-fetch failure: error line scoped to the overlay; `esc` closes; list
  untouched.
- `gh pr edit` failure: scoped status message via the existing `fetchFailedMsg`
  path — does not blank the list.

## Testing

- **Preset cycle:** `f` advances the index and wraps; `activeFilter()` returns the
  expected query for each preset.
- **Picker model:** `space` toggles a row; the fuzzy filter narrows candidates;
  `enter` returns the selected logins; `esc` returns nil.
- **Reviewer diff:** current `{a,c}` + selected `{b,c}` → add `[b]`, remove `[a]`;
  unchanged selection → no command emitted.
- **GraphQL parse:** a fixture response → `[]gh.User{ {login,name}, … }`.
- **Quick window:** empty `ReviewRequests` renders the warn "no reviewers" line;
  non-empty renders the dim reviewers list.

## Delivery

One spec, two implementation plans:

1. **Infra** — filter presets + cycle, the generic picker, `FetchAssignableUsers`,
   the `members` cache key, and the quick-window reviewers line.
2. **Uses** — wire `F` (author filter) and `R` (reviewer assign, with the diff) on
   top of the picker.
