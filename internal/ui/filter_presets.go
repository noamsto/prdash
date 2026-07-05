package ui

type filterPreset struct{ name, search string }

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
