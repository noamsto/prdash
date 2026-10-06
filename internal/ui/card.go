package ui

import (
	"strings"
	"time"

	"github.com/noamsto/prdash/internal/triage"
)

// cardGlyph picks a leading glyph + style for the card's kind.
func cardGlyph(k triage.Kind) string {
	switch k {
	case triage.KindReady:
		return passStyle.Render("✓")
	case triage.KindChecksFailing, triage.KindConflict, triage.KindChangesRequested:
		return failStyle.Render("✗")
	case triage.KindChecksRunning, triage.KindPending:
		return pendStyle.Render(ciRunningGlyph)
	default:
		return dimStyle.Render("•")
	}
}

// renderCard renders the triage card: glyph + headline, any detail lines, and
// the suggested action. Empty headline (fallback) renders nothing.
func renderCard(c triage.Card, width int, now time.Time) string {
	if c.Headline == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(cardGlyph(c.Kind) + " " + headerStyle.Render(c.Headline) + "\n")
	for _, l := range c.Failing {
		b.WriteString("  " + failStyle.Render("✗ "+truncate(l, width-4)) + "\n")
	}
	for _, l := range c.Running {
		label, timing := timedLabel(l.Label, elapsedSince(l.StartedAt, now), width-4)
		b.WriteString("  " + pendStyle.Render(ciRunningGlyph+" "+label) + timing + "\n")
	}
	if c.ActionKey != "" {
		b.WriteString(dimStyle.Render(c.ActionLabel+" → ") + accentStyle.Render(c.ActionKey) + "\n")
	}
	if c.AutoMerge {
		b.WriteString("  " + autoMergeGlyph(true) + " " + dimStyle.Render("auto-merge armed") + "\n")
	}
	return b.String()
}
