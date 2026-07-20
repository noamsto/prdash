package ui

import (
	"strings"

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
		return pendStyle.Render("●")
	default:
		return dimStyle.Render("•")
	}
}

// renderCard renders the triage card: glyph + headline, any detail lines, and
// the suggested action. Empty headline (fallback) renders nothing.
func renderCard(c triage.Card, width int) string {
	if c.Headline == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(cardGlyph(c.Kind) + " " + headerStyle.Render(c.Headline) + "\n")
	for _, l := range c.Failing {
		b.WriteString("  " + failStyle.Render("✗ "+truncate(l, width-4)) + "\n")
	}
	for _, l := range c.Running {
		b.WriteString("  " + pendStyle.Render("● "+truncate(l, width-4)) + "\n")
	}
	if c.ActionKey != "" {
		b.WriteString(dimStyle.Render(c.ActionLabel+" → ") + accentStyle.Render(c.ActionKey) + "\n")
	}
	if c.AutoMerge {
		b.WriteString("  " + autoMergeGlyph(true) + " " + dimStyle.Render("auto-merge armed") + "\n")
	}
	return b.String()
}
