package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
)

var rateNow = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func graphqlSnap(remaining int, in time.Duration) gh.RateSnapshot {
	return gh.RateSnapshot{Limit: 5000, Remaining: remaining, Reset: rateNow.Add(in), Resource: "graphql"}
}

func TestRateSegmentText(t *testing.T) {
	tests := []struct {
		name string
		snap gh.RateSnapshot
		want string // "" ⇒ segment absent
	}{
		{
			name: "healthy graphql bucket is unlabelled",
			snap: graphqlSnap(4832, 23*time.Minute),
			want: rateGlyph + " 4832/5000 · 23m",
		},
		{
			name: "countdown rounds up to the next whole minute",
			snap: graphqlSnap(4832, 22*time.Minute+30*time.Second),
			want: rateGlyph + " 4832/5000 · 23m",
		},
		{
			name: "under a minute",
			snap: graphqlSnap(4832, 30*time.Second),
			want: rateGlyph + " 4832/5000 · <1m",
		},
		{
			name: "core bucket carries a label",
			snap: gh.RateSnapshot{Limit: 5000, Remaining: 120, Reset: rateNow.Add(41 * time.Minute), Resource: "core"},
			want: warnGlyph + " core 120/5000 · 41m",
		},
		{
			// Nothing observed yet: no segment rather than a zeroed one.
			name: "no snapshot",
			snap: gh.RateSnapshot{},
			want: "",
		},
		{
			// The window rolled over, so Remaining describes a spent budget.
			name: "reset already passed",
			snap: graphqlSnap(12, -time.Second),
			want: "",
		},
		{
			name: "reset exactly now",
			snap: graphqlSnap(12, 0),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rateSegment(tc.snap, rateNow, 40)
			if ansi.Strip(got) != tc.want {
				t.Errorf("rateSegment() = %q, want %q", ansi.Strip(got), tc.want)
			}
		})
	}
}

func TestRateSegmentStyleTiers(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      string
	}{
		{"healthy is dim", 4832, dimStyle.Render(rateGlyph + " 4832/5000 · 23m")},
		{"at the warn threshold", 1249, pendStyle.Render(rateGlyph + " 1249/5000 · 23m")},
		{"critical warns", 499, failStyle.Render(warnGlyph + " 499/5000 · 23m")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rateSegment(graphqlSnap(tc.remaining, 23*time.Minute), rateNow, 40)
			if got != tc.want {
				t.Errorf("rateSegment() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateSegmentDroppedWhenItDoesNotFit(t *testing.T) {
	snap := graphqlSnap(4832, 23*time.Minute)
	full := rateSegment(snap, rateNow, 40)
	w := lipgloss.Width(full)

	if got := rateSegment(snap, rateNow, w); lipgloss.Width(got) != w {
		t.Errorf("segment dropped at exactly its own width (avail = %d)", w)
	}
	if got := rateSegment(snap, rateNow, w-1); got != "" {
		t.Errorf("rateSegment() = %q with one cell too few, want it dropped", got)
	}
	if got := rateSegment(snap, rateNow, 0); got != "" {
		t.Errorf("rateSegment() = %q with no room, want it dropped", got)
	}
}

func TestHeaderCarriesRateSegment(t *testing.T) {
	m := NewModel("/repo", "", nil)
	m.SetRepo("owner/repo")
	m.width, m.height = 100, 40
	m.rate = gh.RateSnapshot{Limit: 5000, Remaining: 4832, Reset: time.Now().Add(23 * time.Minute), Resource: "graphql"}

	head := m.header()
	if !strings.Contains(ansi.Strip(head), "4832/5000") {
		t.Fatalf("header should carry the rate segment: %q", head)
	}
	if w := lipgloss.Width(head); w != m.width {
		t.Fatalf("header width = %d, want exactly model width %d (segment is right-aligned):\n%s", w, m.width, head)
	}
}

// The rate segment is the lowest-priority header element: it takes only what the
// refresh spinner, action badge and selection count left over, so it must never
// widen the header past the model width — nor add to the overflow a busy header
// already has at a narrow width.
func TestHeaderRateSegmentYieldsWidth(t *testing.T) {
	busy := func(w int, rate gh.RateSnapshot) Model {
		m := NewModel("/repo", "", nil)
		m.SetRepo("owner/some-long-repo-name")
		m.width, m.height = w, 40
		m.refreshing = true
		m.rate = rate
		m.actionStatus = &actionStat{settled: true, ok: "approved #12"}
		p := gh.PR{Number: 12, Title: "t"}
		p.Author.Login = "alice"
		m.setPRs([]gh.PR{p})
		m.sel.toggle(0)
		return m
	}
	snap := gh.RateSnapshot{Limit: 5000, Remaining: 412, Reset: time.Now().Add(8 * time.Minute), Resource: "graphql"}

	for w := 20; w <= 120; w++ {
		bare := lipgloss.Width(busy(w, gh.RateSnapshot{}).header())
		got := lipgloss.Width(busy(w, snap).header())
		if want := max(bare, w); got > want {
			t.Fatalf("width %d: header width = %d with the segment, %d without; want <= %d",
				w, got, bare, want)
		}
	}
}
