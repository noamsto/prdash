package ui

type filterPreset struct{ name, search string }

var defaultPresets = []filterPreset{
	{"mine", "is:open author:@me"},
	{"review-requested", "is:open review-requested:@me"},
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
