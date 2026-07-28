package ui

import (
	"fmt"
	"math"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// Fraction-remaining thresholds for the rate segment: below warn the budget
// stops being background information, below crit it is about to bite.
const (
	rateWarnFrac = 0.25
	rateCritFrac = 0.10
)

// rateGap is the minimum blank run between the header's inline content and the
// right-aligned rate segment, so the two never read as one string.
const rateGap = 2

// rateSegment renders the API budget as "<glyph> <remaining>/<limit> · <reset>".
// It returns "" — no segment at all — when there is nothing worth showing: no
// snapshot observed yet, a window that has already rolled over (Remaining then
// describes a budget that has since refilled), or fewer than the needed cells in
// avail. Taking now as an argument keeps every countdown and threshold case
// testable without a clock.
func rateSegment(s gh.RateSnapshot, now time.Time, avail int) string {
	if s.Limit == 0 || !s.Reset.After(now) {
		return ""
	}

	// graphql is the bucket nearly every call spends from, so it reads unlabelled;
	// a label means the slot was taken by a bucket that is tighter still.
	label := ""
	if s.Resource != "graphql" {
		label = s.Resource + " "
	}

	glyph, style := rateGlyph, dimStyle
	switch frac := float64(s.Remaining) / float64(s.Limit); {
	case frac < rateCritFrac:
		glyph, style = warnGlyph, failStyle
	case frac < rateWarnFrac:
		style = pendStyle
	}

	text := fmt.Sprintf("%s %s%d/%d · %s", glyph, label, s.Remaining, s.Limit, resetIn(s.Reset, now))
	if lipgloss.Width(text) > avail {
		return ""
	}
	return style.Render(text)
}

// resetIn is the wait until the budget refills, rounded up so it never promises a
// window that hasn't ended yet. GitHub's windows are an hour, so minutes suffice.
func resetIn(reset, now time.Time) string {
	left := reset.Sub(now)
	if left < time.Minute {
		return "<1m"
	}
	return fmt.Sprintf("%dm", int(math.Ceil(left.Minutes())))
}
