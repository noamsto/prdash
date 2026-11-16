package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/noamsto/prdash/internal/triage"
)

func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []triage.FailedCheck{{Label: "lint"}, {Label: "e2e"}}, ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "2 checks failing") {
		t.Fatalf("headline missing: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "e2e") {
		t.Fatalf("failing checks missing: %q", out)
	}
	if !strings.Contains(out, "r") || !strings.Contains(out, "rerun checks") {
		t.Fatalf("suggested action missing: %q", out)
	}
}

func TestRenderCardShowsFailingAndRunningGlyphs(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 failing · 1 running",
		Failing: []triage.FailedCheck{{Label: "lint"}}, Running: []triage.RunningCheck{{Label: "build"}},
		ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "✗ lint") {
		t.Fatalf("failing glyph/label missing: %q", out)
	}
	if !strings.Contains(out, ciRunningGlyph+" build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
}

func TestRenderCardRunningOnlyHasNoFailGlyph(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksRunning, Headline: "Checks running…",
		Running: []triage.RunningCheck{{Label: "build"}}}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, ciRunningGlyph+" build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("unexpected fail glyph on running-only card: %q", out)
	}
}

func TestRenderCardReadyNoAction(t *testing.T) {
	out := renderCard(triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}, 40, time.Now())
	if !strings.Contains(out, "Ready to merge") {
		t.Fatalf("headline missing: %q", out)
	}
}

func TestRenderCardShowsAutoMergeLine(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)", AutoMerge: true}
	out := renderCard(c, 40, time.Now())
	if !strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line missing: %q", out)
	}
}

func TestRenderCardOmitsAutoMergeLineWhenNotArmed(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}
	out := renderCard(c, 40, time.Now())
	if strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line should not appear: %q", out)
	}
}

func assertCardFits(t *testing.T, out string, width int) {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %q is %d columns, want <= %d", ansi.Strip(line), w, width)
		}
	}
}

func TestRenderCardFailingShowsTook(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 check failing",
		Failing: []triage.FailedCheck{{Label: "lint", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:01:20Z"}}}
	out := renderCard(c, 40, time.Now())
	assertCardFits(t, out, 40)
	if got := ansi.Strip(out); !strings.Contains(got, "✗ lint · took 1m 20s") {
		t.Fatalf("took suffix missing: %q", got)
	}
}

func TestRenderCardFailingWithoutStampsHasNoSuffix(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []triage.FailedCheck{
			{Label: "lint"},
			{Label: "e2e", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:00:00Z"},
		}}
	out := renderCard(c, 40, time.Now())
	assertCardFits(t, out, 40)
	got := ansi.Strip(out)
	for _, label := range []string{"lint", "e2e"} {
		if !strings.Contains(got, "✗ "+label) {
			t.Fatalf("label %q missing: %q", label, got)
		}
	}
	if strings.Contains(got, "took") || strings.Contains(got, "·") {
		t.Fatalf("unexpected timing suffix: %q", got)
	}
}

func TestRenderCardFailingTimingDroppedAtNarrowWidth(t *testing.T) {
	const width = 14
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "1 failing",
		Failing: []triage.FailedCheck{{Label: "integration-tests", StartedAt: "2026-01-01T10:00:00Z", CompletedAt: "2026-01-01T10:01:20Z"}}}
	out := renderCard(c, width, time.Now())
	assertCardFits(t, out, width)
	got := ansi.Strip(out)
	if strings.Contains(got, "took") {
		t.Fatalf("timing should be dropped at width %d: %q", width, got)
	}
	if !strings.Contains(got, "✗ integrati") {
		t.Fatalf("truncated label missing: %q", got)
	}
}
