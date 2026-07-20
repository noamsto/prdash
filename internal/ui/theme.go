package ui

import (
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// Theme is the owned palette. Roles are concrete Catppuccin hex — prdash does
// NOT inherit the terminal theme, so mauve is mauve everywhere. Adding a flavor
// (Latte/Frappé/Macchiato) or a dark/light toggle later is a second constructor.
type Theme struct {
	Accent  string // teal — #, keys, links, PR-board accent (title/segment/active tab)
	Issue   string // peach coral — Issues-board accent; shares Draft's hex but drafts are PR-only, so they never co-occur
	Header  string // mauve — top header + repo wordmark, the app identity
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
	Base    string // base — dark text on filled status badges
	Author  []string
}

// Mocha is the Catppuccin Mocha flavor.
func Mocha() Theme {
	return Theme{
		Accent: "#94e2d5", Issue: "#fab387", Header: "#cba6f7", Focus: "#89dceb", Select: "#f5c2e7",
		Text: "#cdd6f4", Meta: "#a6adc8", Rule: "#585b70", RowBg: "#313244",
		Pass: "#a6e3a1", Fail: "#f38ba8", Pending: "#f9e2af", Draft: "#fab387",
		Section: "#74c7ec", Base: "#1e1e2e",
		// Distinct author hues — deliberately excludes teal (accent), mauve (header),
		// sky (focus), pink (select), peach (draft/issue accent), sapphire (section
		// labels), and the green/red/yellow state colors.
		Author: []string{
			"#b4befe", "#eba0ac", "#f5e0dc",
			"#f2cdcd", "#89b4fa",
		},
	}
}

// Latte is the Catppuccin Latte flavor — light mode. Accents are the WCAG-AA
// adjusted values from nix-config palette.nix, so prdash matches the desktop.
func Latte() Theme {
	return Theme{
		Accent: "#179299", Issue: "#fe640b", Header: "#8839ef", Focus: "#0480b3", Select: "#b84a9e",
		Text: "#4c4f69", Meta: "#6c6f85", Rule: "#acb0be", RowBg: "#ccd0da",
		Pass: "#358023", Fail: "#d20f39", Pending: "#996b00", Draft: "#c24b00",
		Section: "#1a7d8f", Base: "#eff1f5",
		Author: []string{
			"#5a6ad4", "#c0364a", "#a85847",
			"#b54545", "#1e66f5",
		},
	}
}

// themeFor maps a mode string ("light"/"dark") to its palette; unknown → Mocha.
func themeFor(mode string) Theme {
	if mode == "light" {
		return Latte()
	}
	return Mocha()
}

// theme is the active palette; applyTheme reassigns it and every derived style.
var theme Theme

var (
	titleStyle        lipgloss.Style
	accentStyle       lipgloss.Style
	issueAccentStyle  lipgloss.Style
	dimStyle          lipgloss.Style
	sepStyle          lipgloss.Style
	passStyle         lipgloss.Style
	failStyle         lipgloss.Style
	pendStyle         lipgloss.Style
	selMarkStyle      lipgloss.Style
	focusBarStyle     lipgloss.Style
	headerStyle       lipgloss.Style
	mergedStyle       lipgloss.Style // mauve — the merged-PR status mark
	statusBarStyle    lipgloss.Style
	sectionLabelStyle lipgloss.Style
	draftTagStyle     lipgloss.Style
	refreshStyle      lipgloss.Style // ambient revalidation; brighter than dim, unfilled
	badgeBase         lipgloss.Style // dark base text on a bright role-color fill
	runBadgeStyle     lipgloss.Style
	passBadgeStyle    lipgloss.Style
	failBadgeStyle    lipgloss.Style
	tabActiveStyle    lipgloss.Style
	tabInactiveStyle  lipgloss.Style
)

// applyTheme swaps the active palette and rebuilds every derived style var. Safe
// without a lock: only called from init(), InitTheme (before the program runs),
// and the single-goroutine Update loop.
func applyTheme(t Theme) {
	theme = t
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text))
	accentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent))
	issueAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Issue))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Rule))
	passStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pass))
	failStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Fail))
	pendStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Pending))
	selMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Select))
	focusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Header)).Bold(true)
	mergedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Header))
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta))
	sectionLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Section)).Bold(true)
	draftTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Draft))
	refreshStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Focus))
	badgeBase = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Base)).Bold(true).Padding(0, 1)
	runBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Accent))
	passBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Pass))
	failBadgeStyle = badgeBase.Background(lipgloss.Color(theme.Fail))

	// Expanded-view tab bar, notched into the box's top border: the active tab
	// reuses the filled accent badge; the rest are dim names, same padding so the
	// tabs keep an even width.
	tabActiveStyle = badgeBase.Background(lipgloss.Color(theme.Accent))
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Meta)).Padding(0, 1)
}

func init() { applyTheme(Mocha()) }

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
// the pane background, so a label reads as a rounded pill. Both are Nerd Font
// glyphs (ple-left/right-half-circle-thick); swap if your font maps them out.
const (
	chipCapLeft  = "\ue0b6" // nerd: ple-left-half-circle-thick
	chipCapRight = "\ue0b4" // nerd: ple-right-half-circle-thick
)

// labelChip renders one rounded label pill: GitHub hex as the fill with auto
// black/white text by luminance; empty/invalid colors fall back to a dim chip.
func labelChip(name, hex string) string {
	fg, bg := lipgloss.Color(theme.Base), lipgloss.Color("#"+hex)
	switch {
	case len(hex) != 6:
		fg, bg = lipgloss.Color(theme.Text), lipgloss.Color(theme.RowBg)
	case lightText(hex):
		fg = lipgloss.Color(theme.Text)
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

// autoMergeGlyphRune marks a PR with GitHub auto-merge armed — it will land on
// its own once checks and reviews clear. Distinct from mergedGlyph (a terminal
// state); this one appears only on still-open PRs.
const autoMergeGlyphRune = "" // nerd: nf-fa-refresh

// autoMergeGlyph is the dense-row/triage-card auto-merge marker. Blank when
// disabled so it never crowds the row — mirrors ciGlyph/reviewDot's "unknown"
// convention but with true silence instead of a dim placeholder, since an
// un-armed PR has nothing to say here.
func autoMergeGlyph(enabled bool) string {
	if !enabled {
		return ""
	}
	return mergedStyle.Render(autoMergeGlyphRune)
}

// mergedGlyph is the status mark for a merged PR — mauve, matching GitHub's
// purple and the lazytmux status line, and distinct from the CI pass/fail marks.
const mergedGlyph = "󰘭" // nerd: nf-md-source-merge (U+F062D)

func mergedMark() string { return mergedStyle.Render(mergedGlyph) }

// closedGlyph marks a PR closed without merging — a dim ✗, distinct from the red
// CI-fail ✗ by color: the checks no longer matter, the PR just didn't land.
const closedGlyph = "✗"

func closedMark() string { return dimStyle.Render(closedGlyph) }
