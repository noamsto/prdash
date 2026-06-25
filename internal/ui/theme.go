package ui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Palette roles. Concrete colors inherit the terminal's theme (lazytmux
// Catppuccin overlay); these adaptive defaults read well on dark backgrounds.
var (
	accentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))  // blue
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241")) // gray
	passStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))  // green
	failStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // red
	pendStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // yellow
	selMarkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // mauve
	cursorRowStyle = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	headerStyle    = accentStyle.Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// metaLine renders the "@author · state · age" header shared by the conversation
// timeline and the reviews tab. state is "" for plain comments; age is omitted
// for a zero time.
func metaLine(author, state string, at time.Time) string {
	s := accentStyle.Render("@" + author)
	if state != "" {
		s += dimStyle.Render(" · ") + reviewStateLabel(state)
	}
	if age := ageString(at); age != "" {
		s += dimStyle.Render(" · " + age)
	}
	return s
}

// reviewStateLabel renders a GitHub review state as a colored, lowercased label.
// Sentiment colors only the decisive states; neutral ones stay dim to keep the
// conversation calm.
func reviewStateLabel(state string) string {
	switch state {
	case "APPROVED":
		return passStyle.Render("approved")
	case "CHANGES_REQUESTED":
		return failStyle.Render("changes requested")
	case "COMMENTED":
		return dimStyle.Render("commented")
	case "DISMISSED":
		return dimStyle.Render("dismissed")
	default:
		return dimStyle.Render(state)
	}
}

// ciGlyph maps a CIState() value to a colored single-rune glyph.
func ciGlyph(state string) string {
	switch state {
	case "pass":
		return passStyle.Render("✓")
	case "fail":
		return failStyle.Render("✗")
	case "pending":
		return pendStyle.Render("●")
	default: // "none" and anything unexpected
		return dimStyle.Render("·")
	}
}
