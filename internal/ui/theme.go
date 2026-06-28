package ui

import (
	"hash/fnv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Palette roles. Concrete colors inherit the terminal's theme (lazytmux
// Catppuccin overlay); these adaptive defaults read well on dark backgrounds.
var (
	titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))           // primary text — row titles, body
	accentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))            // blue — PR#, action keys, links
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))           // meta text — age, labels
	sepStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))           // divider rules — recede below text
	passStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))           // green
	failStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))           // red
	pendStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))           // yellow
	selMarkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))           // mauve — multi-select ●
	focusBarStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))            // cyan — cursor-row left bar
	headerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true) // bright cyan — top header + active tab
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// authorPalette: legible-on-dark hues that stay distinct from the state colors
// (red/green/yellow) so an author tint never reads as a CI signal.
var authorPalette = []string{"75", "114", "176", "80", "215", "139", "179", "211", "73"}

// authorStyle gives each login a stable color so the same person reads the same
// everywhere. Bots are muted — they're noise, not people.
func authorStyle(login string) lipgloss.Style {
	if isBot(login) {
		return dimStyle
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(login))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(authorPalette[h.Sum32()%uint32(len(authorPalette))]))
}

func isBot(login string) bool {
	switch login {
	case "linear-code", "cursor", "github-actions", "factifybot", "claude", "dependabot":
		return true
	}
	return strings.Contains(login, "bot") || strings.Contains(login, "[bot]")
}

// metaLine renders the "@author · state · age" header shared by the conversation
// timeline and the reviews tab. state is "" for plain comments; age is omitted
// for a zero time.
func metaLine(author, state string, at time.Time) string {
	s := authorStyle(author).Bold(true).Render("@" + author)
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
