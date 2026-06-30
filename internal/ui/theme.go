package ui

import (
	"hash/fnv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Theme is the owned palette. Roles are concrete Catppuccin hex — prdash does
// NOT inherit the terminal theme, so mauve is mauve everywhere. Adding a flavor
// (Latte/Frappé/Macchiato) or a dark/light toggle later is a second constructor.
type Theme struct {
	Accent  string // mauve — #, keys, links, headline, header/active tab
	Header  string // mauve — top header + active tab
	Focus   string // sky — cursor-row bar
	Select  string // pink — multi-select ●
	Text    string // row titles, body
	Meta    string // age, labels, dim hints
	Rule    string // dividers, borders
	RowBg   string // cursor-row background
	Pass    string // green
	Fail    string // red
	Pending string // yellow
	Draft   string // peach — the [draft] tag; kept out of the author rotation
	Section string // sapphire — section/group divider labels
	Author  []string
}

// Mocha is the Catppuccin Mocha flavor.
func Mocha() Theme {
	return Theme{
		Accent: "#cba6f7", Header: "#cba6f7", Focus: "#89dceb", Select: "#f5c2e7",
		Text: "#cdd6f4", Meta: "#a6adc8", Rule: "#585b70", RowBg: "#313244",
		Pass: "#a6e3a1", Fail: "#f38ba8", Pending: "#f9e2af", Draft: "#fab387",
		Section: "#74c7ec",
		// Distinct author hues — deliberately excludes mauve (accent), sky (focus),
		// pink (select), peach (draft tag), sapphire (section labels), and the
		// green/red/yellow state colors.
		Author: []string{
			"#b4befe", "#94e2d5", "#eba0ac",
			"#f5e0dc", "#f2cdcd", "#89b4fa",
		},
	}
}

// theme is the active palette. A future toggle reassigns this.
var theme = Mocha()

var (
	titleStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	accentStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	dimStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sepStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	passStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pass))
	failStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Fail))
	pendStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pending))
	selMarkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Select))
	focusBarStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	headerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Header)).Bold(true)
	statusBarStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sectionLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Section)).Bold(true)
	draftTagStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Draft))
)

// authorStyle gives each login a stable color so the same person reads the same
// everywhere. Bots are muted — they're noise, not people.
func authorStyle(login string) lipgloss.Style {
	if isBot(login) {
		return dimStyle
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(login))
	return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Author[h.Sum32()%uint32(len(theme.Author))]))
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
