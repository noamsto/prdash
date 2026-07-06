package ui

import "strings"

type filterPreset struct{ name, search string }

// prStates is the PR state cycle for the `s` toggle. Order = cycle order.
var prStates = []string{"open", "merged", "closed"}

// searchFor composes a gh search from a state (open/merged/closed) and an
// optional body qualifier (e.g. "author:@me"). Empty body yields "is:<state>".
func searchFor(state, body string) string {
	s := "is:" + state
	if body == "" {
		return s
	}
	return s + " " + body
}

// splitState strips a leading is:<state> token, returning the state (default
// "open" when none is present) and the remaining body. Inverse of searchFor.
func splitState(search string) (state, body string) {
	search = strings.TrimSpace(search)
	for _, s := range prStates {
		tok := "is:" + s
		if search == tok {
			return s, ""
		}
		if rest, ok := strings.CutPrefix(search, tok+" "); ok {
			return s, strings.TrimSpace(rest)
		}
	}
	return "open", search
}

// nextState returns the state after s in prStates, wrapping; unknown → first.
func nextState(s string) string {
	for i, st := range prStates {
		if st == s {
			return prStates[(i+1)%len(prStates)]
		}
	}
	return prStates[0]
}

// mineFilter / reviewFilter are the two searches the "mine" view combines into
// its Mine and Review-requested sections. mineFilter doubles as the preset's
// identity (presetIndexFor keys on it).
const (
	mineFilter   = "is:open author:@me"
	reviewFilter = "is:open review-requested:@me"
)

var defaultPresets = []filterPreset{
	{"mine", mineFilter},
	{"all", "is:open"},
}

// nextPreset returns the index after i, wrapping to 0.
func nextPreset(i int) int { return (i + 1) % len(defaultPresets) }

// presetIndexFor returns the index of the preset whose search equals filter,
// or -1 when the filter is a custom (e.g. author) query.
func presetIndexFor(filter string) int {
	for i, p := range defaultPresets {
		if p.search == filter {
			return i
		}
	}
	return -1
}
