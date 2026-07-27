# Declined: adopt go-github for the Actions REST layer

Decision record for #54 item 1 (follow-up to #47 / PR #53). **Outcome: declined
after building and measuring it.** The Actions REST layer stays hand-rolled.

Item 1 was the last open item in #54; items 2 (`cli/go-gh` for token sourcing)
and 3 (`shurcooL/githubv4` → `hasura/go-graphql-client`) were already recorded as
decided-no-action. All three findings now land the same way.

## What was evaluated

Replacing `internal/gh/actions_rest.go`'s four REST calls with
`google/go-github` v89: `ListRepositoryWorkflowRuns`, `RerunFailedJobsByID`,
`RerunJobByID`, and `GetWorkflowJobLogs`.

It was implemented, tested, and built in `4c1a3eb`, reverted by `bdce4f8`, so
the numbers below are measured rather than estimated. If this is ever revisited,
cherry-pick `4c1a3eb` rather than starting over.

## Why declined

**go-github breaks its module path roughly every three weeks.** Nine major
versions shipped in seven months:

| Version | Released |
|---|---|
| v80 | 2025-12-04 |
| v82 | 2026-01-27 |
| v84 | 2026-02-27 |
| v86 | 2026-05-08 |
| v87 | 2026-05-18 |
| v88 | 2026-05-21 |
| v89 | 2026-07-06 |

Every major changes the import path (`/v89/github` → `/v90/github`), so each
bump touches three Go files, `go.mod`, `go.sum`, and forces a `vendorHash`
regeneration. prdash has no CI and no Renovate, so that is all manual.

**What it replaced has no such cost.** Those four endpoints are pinned to
`X-GitHub-Api-Version: 2022-11-28`. GitHub dates that header precisely so a
dated version does not change shape — breaking changes ship as a new date. The
endpoints have not moved in years. The code was written once in #53, tested, and
would sit untouched. Its real maintenance cost is approximately zero.

**Measured trade:**

| Gave up | Got |
|---|---|
| 77 lines of write-once code against four frozen endpoints (~0 maintenance/yr) | A dependency demanding a manual migration every ~3 weeks |
| 4.3 MB of binary — 16.99 → 21.31 MB, **+25%** (`-ldflags="-s -w"`) | Typed accessors and pagination this code barely uses |

The binary cost is structural: go-github ships every service in one package, so
the linker prunes little of the ~90% prdash never calls.

That inverts the premise of #54, which assumed the hand-rolled surface carried
ongoing burden. Owning *frozen* code is cheap; tracking a v89-and-climbing
dependency is not.

The general point: a client library earns its keep when a project uses a lot of
its surface — many endpoints, real pagination, rate limiting, retries. prdash
makes four calls. 77 lines of request-and-parse is not complexity worth
outsourcing.

## The counterargument, and why it loses

Go modules never force an upgrade. Pin v89, never bump, and the churn cost is
also zero.

But that forfeits the entire justification. "It's maintained upstream" was the
reason to adopt it; pinning forever means paying 4.3 MB and a dependency for a
frozen snapshot of code that already existed and already passed its tests. And
if a fix or a new endpoint is ever needed, the result is a multi-major jump
instead of a small local edit.

## What would change this decision

- prdash needs substantially more of the REST API — say a dozen endpoints, real
  pagination, or rate-limit handling. Then the library amortizes.
- go-github adopts a stable module path (no more `/vNN` bumps).
- Renovate or Dependabot lands in this repo *and* CI verifies the bump, making
  the migration cost near-zero rather than manual.

## Verified API facts (so nobody re-derives them)

Confirmed against go-github v89.0.0 source, worth keeping even though unused:

- `GetWorkflowJobLogs(ctx, owner, repo, jobID, maxRedirects)` with
  `maxRedirects: 0` issues exactly one `Transport.RoundTrip`, follows nothing,
  and returns the parsed `Location`. It would have preserved #53's token-safety
  property exactly — the token reaches `api.github.com` only, with the blob
  fetch staying separate and unauthenticated.
- That path bypasses `http.Client.Do` and therefore `Client.Timeout`, so
  `graphTimeout` would have had to ride on a `context` instead. Easy to miss;
  without it the log view can hang forever.
- `WithHTTPClient` copies the `*http.Client` by value, so a `Timeout` set on it
  does carry over.
- `WithURLs` + `parseURL` append the required trailing slash themselves.
- `CheckResponse` accepts any 2xx (202 becomes `*AcceptedError`), which would
  have loosened the rerun calls' exact-201 check.

## #53's token-safety guard is confirmed live

`TestJobLogRedirectDropsAuthOnFollowup` was mutation-tested during this work:
pointing the blob fetch at the oauth2 client made it fail with
`leaked Authorization: "Bearer supersecrettoken"`. The guard on #53's
token-safety property is confirmed live, not vacuous.
