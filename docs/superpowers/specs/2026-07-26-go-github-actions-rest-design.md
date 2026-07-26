# Adopt go-github for the Actions REST layer

Closes #54 (follow-up to #47 / PR #53).

## Problem

PR #53 landed a hand-rolled Actions REST client in `internal/gh/actions_rest.go`
(251 lines): request construction, header pinning, pagination params, JSON
parsing, two rerun POSTs, and manual 302/`Location` handling for job logs. All
of it is maintenance we own for no differentiation. `google/go-github` v89
provides typed, maintained equivalents for every piece except the two that
actually carry prdash-specific behavior.

## Scope

Item 1 of #54 only. Items 2 (`cli/go-gh` for token sourcing) and 3
(`shurcooL/githubv4` → `hasura/go-graphql-client`) are recorded in the issue as
decided-no-action and stay that way.

`ActionsSource`'s method signatures do not change. `internal/action`,
`internal/ui`, and their fakes are untouched.

## Design

### Client wiring

`GraphSource` gains one field, `actions *github.Client`, built in
`NewGraphSource` from the existing oauth2 client via
`github.WithHTTPClient(hc)`. `WithHTTPClient` copies the `http.Client` by value,
so `hc.Timeout = graphTimeout` carries into every `client.Do` path.

Two fields are deleted:

- `apiBase` — tests point the client at their `httptest.Server` with
  `github.WithURLs(&srv.URL, nil)` instead. go-github's `parseURL` appends the
  trailing slash the client requires, so callers don't handle it.
- `token` — the raw token lived on the struct solely so `JobLog` could hand-set
  `Authorization`. go-github's request goes through the oauth2 transport, so
  prdash stops storing the raw token entirely.

`github.NewClient` returns an error, so `NewGraphSource` becomes
`NewGraphSource(token, repo string) (GraphSource, error)`. The error is
unreachable with our options, but discarding it would be an escape hatch;
`main.go` fails fast on it alongside the existing token check. This is the only
change outside `internal/gh`.

### Method mapping

| Current | Replacement |
|---|---|
| `ListRunsForBranch` | `Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, &ListWorkflowRunsOptions{Branch, ListOptions{PerPage: 20}})` |
| `RerunFailedJobs` | `Actions.RerunFailedJobsByID(ctx, owner, repo, runID)` |
| `RerunJob` | `Actions.RerunJobByID(ctx, owner, repo, jobID)` |
| `JobLog` first hop | `Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 0)` |

Deleted: `githubAPIVersion`, `restBase()`, `restRequest()`, `postAction()`,
`ListRunsForBranch`'s response struct and `json.Unmarshal`, and `JobLog`'s
manual request build, header set, no-redirect client, and 302/`Location` check.

`repoParts()` stays — go-github takes `owner, repo` separately while
`GraphSource.repo` is `owner/name`.

Kept ours: `nativeLogToGHFormat` (the `##[group]`/`##[error]` → gh-format
converter) and `JobLog`'s second, unauthenticated blob fetch. go-github has no
equivalent for either.

`WorkflowRun`'s pointer accessors (`r.GetConclusion()`) preserve the existing
`"conclusion": null → ""` mapping without a nil check.

The file is renamed `actions_rest.go` → `actions.go` (and its test likewise),
since what remains is a thin go-github wrapper plus log-format conversion, not
hand-rolled REST.

### Token safety

The load-bearing property from #53 is preserved exactly, and slightly
strengthened. `GetWorkflowJobLogs` with `maxRedirects: 0` issues a single
`Transport.RoundTrip`, never follows a redirect, and returns the parsed
`Location`. The GitHub token reaches `api.github.com` and nowhere else; the blob
fetch stays a separate, unauthenticated request we make ourselves. go-github
adds its own `checkRedirectHost` guard on the follow path we don't use.

`TestJobLogRedirectDropsAuthOnFollowup` — two `httptest` servers asserting the
api hop carries the token and the blob hop does not — is agnostic to who issues
the first hop, so it is the before/after gate for this swap and survives with
only its construction changed.

### Timeouts

`GetWorkflowJobLogs` calls `Transport.RoundTrip` directly, bypassing
`http.Client.Do` and therefore `Client.Timeout`. Today's no-redirect client
carries `graphTimeout`; dropping it naively would let the first hop hang
forever. Every method therefore derives its own deadline from a small
`actionsCtx()` helper wrapping `context.WithTimeout(context.Background(),
graphTimeout)`, which keeps the bound unit-testable — the existing
`TestJobLogClientsCarryTimeout` goes vacuous otherwise, since only one client
remains and go-github owns the redirect refusal.

The other three methods go through `client.Do` and inherit `s.http`'s
`Timeout`; they take the same ctx for consistency.

## Behavior changes

1. **Rerun success is any 2xx, not exactly 201.** go-github's `CheckResponse`
   accepts `200..299` (and converts 202 to `*AcceptedError`). GitHub documents
   201 for both rerun endpoints, so this is a loosening we accept, not a
   correctness change. The test renames to `...Non2xxIsError`.
2. **`Accept` header becomes `application/vnd.github.v3+json`** (go-github's
   `mediaTypeV3`) instead of `application/vnd.github+json`. Both are valid;
   go-github owns the choice now.
3. **Error text changes** on every failure path, from our `fmt.Errorf` strings to
   go-github's `*ErrorResponse`. No caller matches on these strings — tests
   assert only that an error is non-nil.

## Testing

`internal/gh/actions_test.go`, adapted from the current suite:

- `testSource` builds the go-github client with `WithHTTPClient` +
  `WithURLs(&srv.URL, nil)`.
- `requireHeaders` asserts the new `Accept` value; the API-version header is
  asserted as the literal `2022-11-28` so a future go-github default bump is
  visible rather than silent.
- `TestListRunsForBranch` asserts `r.URL.Query()` values for `branch` and
  `per_page` rather than an exact query string — the current exact-match
  assertion would couple the test to go-querystring's struct field ordering.
- `nativeLogToGHFormat` tests are unchanged; that code is untouched.
- New: a test that `actionsCtx()` returns a ctx whose deadline is
  ~`graphTimeout`, replacing `TestJobLogClientsCarryTimeout`.

Verification beyond the suite: run
`TestJobLogRedirectDropsAuthOnFollowup` before and after the swap (must pass
both ways), `nix build` to confirm the new `vendorHash`, and measure the binary
size delta — go-github ships every service in one package, so the growth is
worth reporting rather than assuming.

## Cost

One sizable, well-maintained dependency. `go.mod`, `go.sum`, and `flake.nix`'s
`vendorHash` all change. Net roughly −115 lines in `internal/gh` (251 → ~135).
