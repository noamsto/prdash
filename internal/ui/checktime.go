package ui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
)

// minTimedLabel is the narrowest label worth keeping beside a time suffix;
// below it the suffix is dropped so the name stays readable.
const minTimedLabel = 4

// fmtElapsed renders a running check's age: 45s, 4m, 1h 12m. Seconds are
// dropped past a minute so the text changes at most once a minute.
func fmtElapsed(d time.Duration) string {
	s := int(max(d, 0) / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	default:
		return fmt.Sprintf("%dh %dm", s/3600, s%3600/60)
	}
}

// fmtTook renders a finished check's duration, keeping seconds under an hour:
// 45s, 1m 20s, 1h 12m.
func fmtTook(d time.Duration) string {
	s := int(max(d, 0) / time.Second)
	if s < 60 || s >= 3600 {
		return fmtElapsed(d)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}

// parseCheckTime reads gh.Check's RFC3339 stamps; ok is false for empty or
// unparsable input.
func parseCheckTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// elapsedSince is the compact age of a start stamp; "" when there is none or
// now is zero (a frozen rollup, see liveClock).
func elapsedSince(startedAt string, now time.Time) string {
	started, ok := parseCheckTime(startedAt)
	if !ok || now.IsZero() {
		return ""
	}
	return fmtElapsed(now.Sub(started))
}

// checkTiming is the dim suffix text for one check: elapsed while running,
// "took X" once finished, "" when the timestamps don't say.
func checkTiming(c gh.Check, now time.Time) string {
	if c.Result() == "pending" {
		return elapsedSince(c.StartedAt, now)
	}
	started, ok := parseCheckTime(c.StartedAt)
	if !ok {
		return ""
	}
	done, ok := parseCheckTime(c.CompletedAt)
	if !ok {
		return ""
	}
	d := done.Sub(started)
	if d <= 0 { // skipped jobs stamp start == completion
		return ""
	}
	return "took " + fmtTook(d)
}

// timedLabel fits label plus a " · timing" suffix into avail cells, truncating
// the label first. The styled suffix is returned separately so the caller keeps
// styling the label itself; it is dropped when too little room would be left.
func timedLabel(label, timing string, avail int) (string, string) {
	if timing == "" {
		return truncate(label, avail), ""
	}
	sfx := " · " + timing
	room := avail - lipgloss.Width(sfx)
	if room < min(minTimedLabel, lipgloss.Width(label)) {
		return truncate(label, avail), ""
	}
	return truncate(label, room), dimStyle.Render(sfx)
}

// liveClock is the time to age pr's running checks against: the zero time for a
// merged, closed or held row, whose rollup is frozen and would otherwise show an
// ever-growing elapsed time.
func (m Model) liveClock(pr gh.PR) time.Time {
	if frozenRow(pr, m.held) {
		return time.Time{}
	}
	return m.clock()
}

// hasTimedPending reports whether pr has a running check whose elapsed time is
// on screen, i.e. one that needs repainting as the clock advances.
func hasTimedPending(pr gh.PR) bool {
	for _, c := range pr.Checks() {
		if c.Result() == "pending" {
			if _, ok := parseCheckTime(c.StartedAt); ok {
				return true
			}
		}
	}
	return false
}
