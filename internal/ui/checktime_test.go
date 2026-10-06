package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/triage"
)

func TestFmtElapsedAndTook(t *testing.T) {
	cases := []struct {
		d             time.Duration
		elapsed, took string
	}{
		{-5 * time.Second, "0s", "0s"},
		{0, "0s", "0s"},
		{59 * time.Second, "59s", "59s"},
		{60 * time.Second, "1m", "1m 0s"},
		{80 * time.Second, "1m", "1m 20s"},
		{59*time.Minute + 59*time.Second, "59m", "59m 59s"},
		{time.Hour, "1h 0m", "1h 0m"},
		{72*time.Minute + 30*time.Second, "1h 12m", "1h 12m"},
	}
	for _, c := range cases {
		if got := fmtElapsed(c.d); got != c.elapsed {
			t.Errorf("fmtElapsed(%v) = %q, want %q", c.d, got, c.elapsed)
		}
		if got := fmtTook(c.d); got != c.took {
			t.Errorf("fmtTook(%v) = %q, want %q", c.d, got, c.took)
		}
	}
}

var timingNow = time.Date(2026, 7, 30, 9, 4, 0, 0, time.UTC)

func TestRenderChecksTiming(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "IN_PROGRESS", Name: "build", StartedAt: "2026-07-30T09:00:00Z"},
		{Conclusion: "SUCCESS", Name: "lint", StartedAt: "2026-07-30T09:00:00Z", CompletedAt: "2026-07-30T09:01:20Z"},
		{State: "PENDING", Context: "ci/legacy"},
		{State: "QUEUED", Name: "queued"},
		{State: "IN_PROGRESS", Name: "skewed", StartedAt: "2026-07-30T09:10:00Z"},
		{State: "IN_PROGRESS", Name: "garbage", StartedAt: "not-a-time"},
		{Conclusion: "SUCCESS", Name: "nostop", StartedAt: "2026-07-30T09:00:00Z"},
		{State: "SUCCESS", Context: "ci/done"},
		{Conclusion: "SKIPPED", Name: "skipped", StartedAt: "2026-07-30T09:00:00Z", CompletedAt: "2026-07-30T09:00:00Z"},
	}}
	lines := strings.Split(ansi.Strip(strings.TrimRight(renderChecks(pr, 60, 0, timingNow), "\n")), "\n")
	want := []string{"build · 4m", "lint · took 1m 20s", "ci/legacy", "queued", "skewed · 0s", "garbage", "nostop", "ci/done", "skipped"}
	for i, w := range want {
		if !strings.HasSuffix(lines[i], w) {
			t.Errorf("line %d = %q, want suffix %q", i, lines[i], w)
		}
	}
	for _, i := range []int{2, 3, 5, 6, 7, 8} {
		if strings.Contains(lines[i], "·") {
			t.Errorf("line %d must carry no timing: %q", i, lines[i])
		}
	}
}

func TestRenderChecksTimingFitsWidth(t *testing.T) {
	pr := gh.PR{StatusCheckRollup: []gh.Check{
		{State: "IN_PROGRESS", Name: "a-very-long-check-name-indeed", StartedAt: "2026-07-30T09:00:00Z"},
	}}
	for _, w := range []int{14, 20, 40} {
		line := ansi.Strip(strings.TrimRight(renderChecks(pr, w, 0, timingNow), "\n"))
		if got := len([]rune(line)); got > w {
			t.Errorf("w=%d: line %q is %d cells wide", w, line, got)
		}
	}
	// Too narrow for label + suffix: the suffix gives way, not the name.
	line := ansi.Strip(renderChecks(pr, 9, 0, timingNow))
	if strings.Contains(line, "4m") {
		t.Errorf("suffix should drop at w=9: %q", line)
	}
}

func TestRenderCardRunningShowsElapsed(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksRunning, Headline: "Checks running…",
		Running: []triage.RunningCheck{
			{Label: "build", StartedAt: "2026-07-30T09:00:00Z"},
			{Label: "legacy"},
		}}
	out := ansi.Strip(renderCard(c, 40, timingNow))
	if !strings.Contains(out, ciRunningGlyph+" build · 4m") {
		t.Errorf("elapsed missing: %q", out)
	}
	if !strings.Contains(out, ciRunningGlyph+" legacy\n") {
		t.Errorf("untimed check should show label only: %q", out)
	}
}

// The elapsed time advances on the existing 1s tick with no refetch: the
// expanded body is stored viewport content, so the tick must re-render it.
func TestElapsedAdvancesOnTickWithoutRefetch(t *testing.T) {
	writeState(t, "")
	now := timingNow
	m := NewModel("/repo", "is:open", nil)
	m.width, m.height = 120, 30
	m.now = func() time.Time { return now }
	m.setPRs([]gh.PR{{Number: 1, StatusCheckRollup: []gh.Check{
		{State: "IN_PROGRESS", Name: "build", StartedAt: "2026-07-30T09:00:00Z"},
	}}})
	m.detail[1] = gh.PRDetail{}
	m.expandedTab = tabChecks
	m.enterExpanded()
	if !strings.Contains(ansi.Strip(m.expandedView()), "build · 4m") {
		t.Fatalf("initial elapsed missing:\n%s", ansi.Strip(m.expandedView()))
	}
	now = now.Add(2 * time.Minute)
	next, _ := m.Update(themePollMsg{lastMod: m.themeModTime})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.expandedView()), "build · 6m") {
		t.Fatalf("elapsed did not advance on tick:\n%s", ansi.Strip(m.expandedView()))
	}
}

// Frozen rows (merged, closed, held) must show no elapsed time through the real
// render paths, and must not keep the tick reflowing.
func TestFrozenRowsShowNoElapsed(t *testing.T) {
	run := []gh.Check{{State: "IN_PROGRESS", Name: "build", StartedAt: "2026-07-30T09:00:00Z"}}
	cases := []struct {
		name   string
		pr     gh.PR
		held   bool
		frozen bool
	}{
		{"live", gh.PR{Number: 1, StatusCheckRollup: run}, false, false},
		{"merged", gh.PR{Number: 1, State: "MERGED", StatusCheckRollup: run}, false, true},
		{"closed", gh.PR{Number: 1, State: "CLOSED", StatusCheckRollup: run}, false, true},
		{"held", gh.PR{Number: 1, StatusCheckRollup: run}, true, true},
		{"no start stamp", gh.PR{Number: 1, StatusCheckRollup: []gh.Check{{State: "IN_PROGRESS", Name: "build"}}}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewModel("/repo", "is:open", nil)
			m.width, m.height = 120, 30
			m.now = func() time.Time { return timingNow }
			m.setPRs([]gh.PR{c.pr})
			m.detail[1] = gh.PRDetail{MergeStateStatus: "UNSTABLE"}
			if c.held {
				m.held[1] = "merged"
			}
			m.expandedTab = tabChecks
			checks := ansi.Strip(m.expandedBody(60))
			overview := ansi.Strip(m.renderOverview(60))
			for name, out := range map[string]string{"checks": checks, "overview": overview} {
				if got := strings.Contains(out, "build · 4m"); got == c.frozen {
					t.Errorf("%s: elapsed shown = %v, frozen = %v:\n%s", name, got, c.frozen, out)
				}
			}
			if got := m.focusedHasTimedPending(); got == c.frozen {
				t.Errorf("focusedHasTimedPending = %v, frozen = %v", got, c.frozen)
			}
		})
	}
}
