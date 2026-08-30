package ui

import "github.com/noamsto/prdash/internal/gh"

// issueDetailMsg carries a fetched issue body, cached to disk so the preview
// paints instantly next launch (mirrors prDetailMsg).
type issueDetailMsg struct {
	number int
	detail gh.IssueDetail
	raw    []byte
}

type prsFetchedMsg struct {
	filter string // the search this result is for; "" means the current foreground fetch
	prs    []gh.PR
	raw    []byte
}

type issuesFetchedMsg struct {
	filter string // the search this result is for; "" means the current foreground fetch
	issues []gh.Issue
	raw    []byte
}

// sectionsFetchedMsg carries the async thirds of the empty-default open view
// (review-requested + reviewed-by-me + the limit-100 open list) so the handler
// can compose the Review/Mine/Others sections via setSections.
type sectionsFetchedMsg struct {
	state                           string // the PR state (open/merged/closed) this result is for
	review, reviewed, open          []gh.PR
	reviewRaw, reviewedRaw, openRaw []byte
}

// issueSectionsFetchedMsg carries the three halves of the issue sections view
// so the handler can compose Mine/Others. Unlike sectionsFetchedMsg it carries
// no state field: issueSectionFilters pins the literal "open", so there is no
// other state a result could be for and no stale-state comparison to make.
type issueSectionsFetchedMsg struct {
	assigned, authored, open          []gh.Issue
	assignedRaw, authoredRaw, openRaw []byte
}

// detailsBatchMsg carries one batched detail fetch — the whole prefetch window
// resolved in a single request (githubv4 aliased query).
type detailsBatchMsg struct {
	details map[int]gh.PRDetail
	raws    map[int][]byte
}

// fetchFailedMsg's mode discriminates which board a filter belongs to:
// searchFor("pr", "open", "") and searchFor("issue", "open", "") are both
// "is:open", so filter alone cannot tell a PR-side failure from an issue-side
// one. Empty for senders that carry no filter — those must always surface
// regardless of board.
type fetchFailedMsg struct {
	err    error
	mode   string
	filter string // set for list fetches; a background prewarm failure is dropped
}

// membersFetchedMsg carries the assignable-users list; raw is the marshaled
// []User for the members cache (see hydrateMembers/membersKey).
type membersFetchedMsg struct {
	users []gh.User
	raw   []byte
}

// viewerFetchedMsg carries the authenticated user's login, fetched once per
// launch and cached indefinitely (see viewerKey).
type viewerFetchedMsg struct{ login string }

type detailDebounceMsg struct{ seq int }

// omniDebounceMsg fires ~250ms after the omni server-qualifier last changed; only
// the latest seq issues the SWR refetch for the composed query.
type omniDebounceMsg struct{ seq int }

// spinnerTickMsg advances the header refresh spinner; the loop runs only while a
// fetch is in flight.
type spinnerTickMsg struct{}

// fetchSkippedMsg is emitted at launch when the current view's cache is still
// fresh, so no list fetch runs. It only clears the refresh state the hydrated
// view was painted under.
type fetchSkippedMsg struct{}

// actionDoneMsg reports an inline action's completion so the header can settle
// its status badge. The running wording is already held on m.actionStatus; ok
// and fail optionally override the settled text (used by bulk aggregate counts).
type actionDoneMsg struct {
	err      error
	ok, fail string
}

// actionClearMsg wipes a settled action status after its dwell time.
type actionClearMsg struct{}

// delayedRefreshMsg is the second, later refetch scheduled after an action that
// re-triggers CI: the immediate refetch races GitHub's queueing and returns the
// pre-push rollup, so a follow-up is what actually brings the new runs in.
type delayedRefreshMsg struct{}

// checksPollMsg fires the live-checks poll beat; the loop runs only while some
// shown PR has a running check.
type checksPollMsg struct{}

// checksFetchedMsg carries one poll beat's rollups, keyed by PR number. It
// replaces the rollup on rows the board already holds — no row is added,
// removed or reordered by a beat.
type checksFetchedMsg struct {
	checks map[int][]gh.Check
}

// logFetchedMsg carries a fetched job log back to the log sub-view. all
// distinguishes the full-log variant from failed-only so a stale in-flight
// fetch for the other variant is ignored.
type logFetchedMsg struct {
	job string
	all bool
	raw []byte
	err error
}
