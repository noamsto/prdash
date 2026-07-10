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

type fetchFailedMsg struct {
	err    error
	filter string // set for list fetches; a background prewarm failure is dropped
}

type membersFetchedMsg struct{ users []gh.User }

type detailDebounceMsg struct{ seq int }

// spinnerTickMsg advances the header refresh spinner; the loop runs only while a
// fetch is in flight.
type spinnerTickMsg struct{}

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
