package ui

import "github.com/noamsto/prdash/internal/gh"

type prsFetchedMsg struct {
	prs []gh.PR
	raw []byte
}

type fetchFailedMsg struct{ err error }

type membersFetchedMsg struct{ users []gh.User }

// filterDebounceMsg fires after the f-key settles; gen guards against stale
// timers from rapid presets cycling, so only the final filter fetches.
type filterDebounceMsg struct{ gen int }
