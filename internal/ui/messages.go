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

// mineFetchedMsg carries both halves of the "mine" view (authored +
// review-requested) so it can render them as two sections.
type mineFetchedMsg struct {
	state              string // the PR state (open/merged/closed) this result is for
	mine, review       []gh.PR
	mineRaw, reviewRaw []byte
}

// sectionsFetchedMsg carries the two async halves of the empty-default open view
// (review-requested + the limit-100 open list) so the handler can compose the
// Review/Mine/Others sections via setSections. Generalizes mineFetchedMsg.
type sectionsFetchedMsg struct {
	state              string // the PR state (open/merged/closed) this result is for
	review, open       []gh.PR
	reviewRaw, openRaw []byte
}

type fetchFailedMsg struct {
	err    error
	filter string // set for list fetches; a background prewarm failure is dropped
}

type membersFetchedMsg struct{ users []gh.User }

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

// checksPollMsg fires the live-checks poll beat; the loop runs only while some
// shown PR has a running check.
type checksPollMsg struct{}
