package ui

import (
	"hash/fnv"
	"strconv"
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

// lightText reports whether a label background (6-hex, no '#') is dark enough to
// need light text. Uses perceptual luminance; unparsable colors default to
// light text (safe on the dim fallback chip).
func lightText(hex string) bool {
	if len(hex) != 6 {
		return true
	}
	r, e1 := strconv.ParseInt(hex[0:2], 16, 0)
	g, e2 := strconv.ParseInt(hex[2:4], 16, 0)
	b, e3 := strconv.ParseInt(hex[4:6], 16, 0)
	if e1 != nil || e2 != nil || e3 != nil {
		return true
	}
	lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
	return lum < 150
}

// chipStyle renders a label pill: the label's background color with auto-picked
// black/white text. Empty/invalid colors fall back to a neutral dim chip.
func chipStyle(hex string) lipgloss.Style {
	if len(hex) != 6 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238"))
	}
	fg := lipgloss.Color("16") // near-black
	if lightText(hex) {
		fg = lipgloss.Color("231") // near-white
	}
	return lipgloss.NewStyle().Foreground(fg).Background(lipgloss.Color("#" + hex))
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
