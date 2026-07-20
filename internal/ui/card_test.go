package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/triage"
)

func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Failing: []string{"lint", "e2e"}, ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40)
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
		Failing: []string{"lint"}, Running: []string{"build"},
		ActionKey: "r", ActionLabel: "rerun checks"}
	out := renderCard(c, 40)
	if !strings.Contains(out, "✗ lint") {
		t.Fatalf("failing glyph/label missing: %q", out)
	}
	if !strings.Contains(out, "● build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
}

func TestRenderCardRunningOnlyHasNoFailGlyph(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksRunning, Headline: "Checks running…",
		Running: []string{"build"}}
	out := renderCard(c, 40)
	if !strings.Contains(out, "● build") {
		t.Fatalf("running glyph/label missing: %q", out)
	}
	if strings.Contains(out, "✗") {
		t.Fatalf("unexpected fail glyph on running-only card: %q", out)
	}
}

func TestRenderCardReadyNoAction(t *testing.T) {
	out := renderCard(triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}, 40)
	if !strings.Contains(out, "Ready to merge") {
		t.Fatalf("headline missing: %q", out)
	}
}

func TestRenderCardShowsAutoMergeLine(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)", AutoMerge: true}
	out := renderCard(c, 40)
	if !strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line missing: %q", out)
	}
}

func TestRenderCardOmitsAutoMergeLineWhenNotArmed(t *testing.T) {
	c := triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}
	out := renderCard(c, 40)
	if strings.Contains(out, "auto-merge armed") {
		t.Fatalf("auto-merge line should not appear: %q", out)
	}
}
