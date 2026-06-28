package ui

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Catppuccin Mocha — the fixed dark palette (matches the lazytmux terminal
// theme). Hardcoded hex so the TUI reads identically regardless of the terminal's
// 16/256 ANSI mapping.
const (
	ctpBase     = "#1e1e2e"
	ctpText     = "#cdd6f4"
	ctpOverlay1 = "#7f849c"
	ctpSurface2 = "#585b70"
	ctpSurface1 = "#45475a"
	ctpRed      = "#f38ba8"
	ctpGreen    = "#a6e3a1"
	ctpYellow   = "#f9e2af"
	ctpPeach    = "#fab387"
	ctpBlue     = "#89b4fa"
	ctpSky      = "#89dceb"
	ctpMauve    = "#cba6f7"
	ctpLavender = "#b4befe"
	ctpTeal     = "#94e2d5"
	ctpPink     = "#f5c2e7"
	ctpSapphire = "#74c7ec"
	ctpFlamingo = "#f2cdcd"
)

// Palette roles, mapped onto Catppuccin Mocha.
var (
	titleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpText))           // primary text — row titles, body
	accentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpBlue))           // PR#, action keys, links
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpOverlay1))       // meta text — age, labels
	sepStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpSurface1))       // divider rules — recede below text
	passStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpGreen))          // green
	failStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpRed))            // red
	pendStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpYellow))         // yellow
	selMarkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpMauve))          // mauve — multi-select ●
	focusBarStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpSky))            // sky — cursor-row left bar
	headerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpSky)).Bold(true) // header + active tab
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ctpOverlay1))
	errBarStyle    = lipgloss.NewStyle().Background(lipgloss.Color(ctpRed)).Foreground(lipgloss.Color(ctpBase)).Bold(true)
	confirmBorder  = lipgloss.Color(ctpYellow)   // confirm modal frame
	borderColor    = lipgloss.Color(ctpSurface2) // pane frames
)

// authorPalette: Mocha hues that stay clear of the state colors (red/green/
// yellow) so an author tint never reads as a CI signal.
var authorPalette = []string{ctpBlue, ctpMauve, ctpTeal, ctpPeach, ctpPink, ctpSapphire, ctpLavender, ctpSky, ctpFlamingo}

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

// Rounded chip end-caps: Powerline half-circles drawn in the chip's own color on
// the pane background, so a label reads as a rounded pill. / are Nerd
// Font glyphs (ple-left/right-half-circle-thick); swap if your font maps them out.
const (
	chipCapLeft  = "\ue0b6"
	chipCapRight = "\ue0b4"
)

// labelChip renders one rounded label pill: GitHub hex as the fill with auto
// black/white text by luminance; empty/invalid colors fall back to a dim chip.
func labelChip(name, hex string) string {
	fg, bg := lipgloss.Color(ctpBase), lipgloss.Color("#"+hex)
	switch {
	case len(hex) != 6:
		fg, bg = lipgloss.Color(ctpText), lipgloss.Color(ctpSurface1)
	case lightText(hex):
		fg = lipgloss.Color(ctpText)
	}
	caps := lipgloss.NewStyle().Foreground(bg)
	body := lipgloss.NewStyle().Foreground(fg).Background(bg)
	return caps.Render(chipCapLeft) + body.Render(name) + caps.Render(chipCapRight)
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
