package ui

import (
	"strings"
	"testing"

	"github.com/noamsto/prdash/internal/triage"
)

func TestRenderCardShowsHeadlineAndAction(t *testing.T) {
	c := triage.Card{Kind: triage.KindChecksFailing, Headline: "2 checks failing",
		Lines: []string{"lint", "e2e"}, ActionKey: "r", ActionLabel: "rerun failed"}
	out := renderCard(c, 40)
	if !strings.Contains(out, "2 checks failing") {
		t.Fatalf("headline missing: %q", out)
	}
	if !strings.Contains(out, "lint") || !strings.Contains(out, "e2e") {
		t.Fatalf("failing checks missing: %q", out)
	}
	if !strings.Contains(out, "r") || !strings.Contains(out, "rerun failed") {
		t.Fatalf("suggested action missing: %q", out)
	}
}

func TestRenderCardReadyNoAction(t *testing.T) {
	out := renderCard(triage.Card{Kind: triage.KindReady, Headline: "Ready to merge",
		ActionKey: "m", ActionLabel: "merge (squash)"}, 40)
	if !strings.Contains(out, "Ready to merge") {
		t.Fatalf("headline missing: %q", out)
	}
}
