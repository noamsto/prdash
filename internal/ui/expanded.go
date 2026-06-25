package ui

import (
	"fmt"
	"strings"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

var expandedTabs = []string{"Conversation", "Reviews", "Checks", "Diff"}

// jumpTabIndex maps a triage card's JumpTab to a tab index (default Conversation).
func jumpTabIndex(jump string) int {
	switch jump {
	case "reviews":
		return 1
	case "checks":
		return 2
	case "diff":
		return 3
	default:
		return 0
	}
}

func tabStrip(active int) string {
	parts := make([]string, len(expandedTabs))
	for i, t := range expandedTabs {
		if i == active {
			parts[i] = accentStyle.Render(t)
		} else {
			parts[i] = dimStyle.Render(t)
		}
	}
	return "  " + strings.Join(parts, "   ")
}

func renderReviews(d gh.PRDetail, w int) string {
	if len(d.LatestReviews) == 0 {
		return dimStyle.Render("  No reviews yet.")
	}
	var b strings.Builder
	for _, r := range d.LatestReviews {
		hdr := "@" + r.Author.Login
		if r.State != "" {
			hdr += " · " + r.State
		}
		b.WriteString(accentStyle.Render(hdr) + "\n")
		if r.Body != "" {
			body, err := preview.Render(r.Body, w)
			if err != nil {
				body = r.Body
			}
			b.WriteString(body)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderChecks(pr gh.PR, w int) string {
	if len(pr.StatusCheckRollup) == 0 {
		return dimStyle.Render("  No checks.")
	}
	var b strings.Builder
	for _, c := range pr.StatusCheckRollup {
		b.WriteString("  " + ciGlyph(c.Result()) + " " + truncate(c.Label(), w-4) + "\n")
	}
	return b.String()
}

func renderDiffstat(d gh.PRDetail, w int) string {
	s := d.Diffstat()
	if s.Files == 0 {
		return dimStyle.Render("  No file changes.")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %d files  %s  %s\n\n", s.Files,
		passStyle.Render(fmt.Sprintf("+%d", s.Additions)), failStyle.Render(fmt.Sprintf("-%d", s.Deletions))))
	for _, f := range d.Files {
		b.WriteString(fmt.Sprintf("  %s  %s %s\n", truncate(f.Path, w-16),
			passStyle.Render(fmt.Sprintf("+%d", f.Additions)), failStyle.Render(fmt.Sprintf("-%d", f.Deletions))))
	}
	return b.String()
}
