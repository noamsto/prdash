package ui

import "strings"

type filterPreset struct{ name, search string }

// prStates / issueStates are the `s`-toggle cycles per board. Order = cycle order.
var prStates = []string{"open", "merged", "closed"}
var issueStates = []string{"open", "closed"}

// searchFor composes a gh search from a state and an optional body qualifier.
func searchFor(state, body string) string {
	s := "is:" + state
	if body == "" {
		return s
	}
	return s + " " + body
}

// splitState strips a leading is:<state> token, returning the state (default
// states[0] when none is present) and the remaining body. Inverse of searchFor.
func splitState(search string, states []string) (state, body string) {
	search = strings.TrimSpace(search)
	for _, s := range states {
		tok := "is:" + s
		if search == tok {
			return s, ""
		}
		if rest, ok := strings.CutPrefix(search, tok+" "); ok {
			return s, strings.TrimSpace(rest)
		}
	}
	return states[0], search
}

// nextState returns the state after s in states, wrapping; unknown → first.
func nextState(s string, states []string) string {
	for i, st := range states {
		if st == s {
			return states[(i+1)%len(states)]
		}
	}
	return states[0]
}

// mineBody / reviewBody are the two state-agnostic qualifiers the PR "mine" view
// combines. Issues use a single assignee qualifier (assigneeBody), no dual fetch.
const (
	mineBody     = "author:@me"
	reviewBody   = "review-requested:@me"
	assigneeBody = "assignee:@me"
)

var defaultPresets = []filterPreset{
	{"mine", mineBody},
	{"all", ""},
}

var issuePresets = []filterPreset{
	{"mine", assigneeBody},
	{"all", ""},
}

// statesFor / presetsFor select the cycle tables for the active board mode.
func statesFor(mode string) []string {
	if mode == "issue" {
		return issueStates
	}
	return prStates
}

func presetsFor(mode string) []filterPreset {
	if mode == "issue" {
		return issuePresets
	}
	return defaultPresets
}

// nextPreset returns the index after i, wrapping to 0.
func nextPreset(i int, presets []filterPreset) int { return (i + 1) % len(presets) }

// presetIndexFor returns the index of the preset whose body equals body, or -1
// when it is a custom (author) query.
func presetIndexFor(body string, presets []filterPreset) int {
	for i, p := range presets {
		if p.search == body {
			return i
		}
	}
	return -1
}
