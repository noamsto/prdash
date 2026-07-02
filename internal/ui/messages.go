package ui

import "github.com/noamsto/prdash/internal/gh"

type prsFetchedMsg struct {
	filter string // the search this result is for; "" means the current foreground fetch
	prs    []gh.PR
	raw    []byte
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

// actionDoneMsg reports an inline action's completion so the header can show
// success/failure.
type actionDoneMsg struct {
	label string
	err   error
}

// actionClearMsg wipes a settled action status after its dwell time.
type actionClearMsg struct{}
