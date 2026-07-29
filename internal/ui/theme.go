package ui

import (
	"hash/fnv"
	"math"
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
	Select  string // pink — multi-select bar ▌
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

// luminance returns the perceptual luminance (0..255) of a 6-hex color (no '#')
// and whether it parsed. Shared by lightText and chipReadableAsText.
func luminance(hex string) (float64, bool) {
	if len(hex) != 6 {
		return 0, false
	}
	r, e1 := strconv.ParseInt(hex[0:2], 16, 0)
	g, e2 := strconv.ParseInt(hex[2:4], 16, 0)
	b, e3 := strconv.ParseInt(hex[4:6], 16, 0)
	if e1 != nil || e2 != nil || e3 != nil {
		return 0, false
	}
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b), true
}

// lightText reports whether a label background (6-hex, no '#') is dark enough to
// need light text. Unparsable colors default to light text (safe on the dim
// fallback chip).
func lightText(hex string) bool {
	lum, ok := luminance(hex)
	if !ok {
		return true
	}
	return lum < 150
}

// relativeLuminance is the WCAG relative luminance of a 6-hex color (no '#'),
// with sRGB channels linearized. Unlike the perceptual byte-average used for
// text-on-fill decisions, this is the basis for a real contrast ratio.
func relativeLuminance(hex string) (float64, bool) {
	if len(hex) != 6 {
		return 0, false
	}
	var lin [3]float64
	for i := range lin {
		v, err := strconv.ParseInt(hex[i*2:i*2+2], 16, 0)
		if err != nil {
			return 0, false
		}
		c := float64(v) / 255
		if c <= 0.04045 {
			lin[i] = c / 12.92
		} else {
			lin[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*lin[0] + 0.7152*lin[1] + 0.0722*lin[2], true
}

// contrastRatio is the WCAG contrast ratio (1..21) between two relative luminances.
func contrastRatio(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

// chipOutlineMinContrast is the WCAG contrast ratio a label color needs against
// the row backgrounds to read as plain colored text. 3:1 is the AA floor for UI
// components and large text. Below it the chip falls back to a filled pill.
//
// A ratio, not a luminance gap: a saturated red like #FF2200 has low luminance
// (red barely contributes to it) yet reads vividly on a dark ground — judging it
// by luminance distance alone wrongly filled it while #FF5500 stayed outlined.
const chipOutlineMinContrast = 3.0

// chipReadableAsText reports whether a label color contrasts enough with BOTH the
// pane background and the focused-row background to render as an outline chip
// (colored brackets + colored text, no fill) on either.
func chipReadableAsText(hex string) bool {
	lc, ok := relativeLuminance(hex)
	if !ok {
		return false
	}
	base, ok1 := relativeLuminance(strings.TrimPrefix(theme.Base, "#"))
	row, ok2 := relativeLuminance(strings.TrimPrefix(theme.RowBg, "#"))
	if !ok1 || !ok2 {
		return false
	}
	return contrastRatio(lc, base) >= chipOutlineMinContrast &&
		contrastRatio(lc, row) >= chipOutlineMinContrast
}

// Line-2 property glyphs: a person before the author, a branch before the head
// ref. Set to whatever your Nerd Font maps if these render as tofu.
const (
	authorGlyph = "\uF415" // nerd: nf-oct-person
	branchGlyph = "\uF418" // nerd: nf-oct-git_branch
)

// reviewApprovedGlyph marks an approved review. A badge, not a ✓: the CI column
// sits right beside it and already uses the plain check, so a second one read as
// a duplicate instead of a second signal.
const reviewApprovedGlyph = "\uF461" // nerd: nf-oct-verified

// Rounded chip end-caps: Powerline half-circles drawn in the chip's own color on
// the pane background, so a label reads as a rounded pill. Both are Nerd Font
// glyphs (ple-left/right-half-circle-thick); swap if your font maps them out.
const (
	chipCapLeft  = "\ue0b6" // nerd: ple-left-half-circle-thick
	chipCapRight = "\ue0b4" // nerd: ple-right-half-circle-thick
)

// labelChip renders one label. When the label color contrasts enough with the
// row backgrounds it renders as a light bracketed chip — "[name]" with brackets
// and text in the label color, no fill. Low-contrast or invalid colors fall back
// to a filled pill with auto black/white text so they stay legible.
func labelChip(name, hex string) string {
	if chipReadableAsText(hex) {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color("#" + hex))
		return st.Render("[" + name + "]")
	}
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
const autoMergeGlyphRune = "\uF46A" // nerd: nf-oct-sync

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

// warnGlyph is the conflict/behind flag. An Octicon, matching prGlyph/issueGlyph
// and the other row markers — not U+26A0, which many terminals draw as a 2-cell
// emoji (with or without a VS15 selector) while lipgloss measures 1, shifting the
// number column. Keep any replacement single-width; oneCell guards the grid but
// cannot shrink an over-wide glyph.
const warnGlyph = "\uF421" // nerd: nf-oct-alert

// focusBarGlyph and selBarGlyph share the row's single leftmost cell. The bar
// encodes multi-selection, and marks the cursor row only when that row is not
// selected — on the board row, focus also reads via the row background and a
// bold title, selection has nothing else. selBarGlyph is the heavier block on
// purpose: selection is what an action fires against, so it must read by
// weight and not by hue alone.
// Both must stay single-width; see warnGlyph above.
const (
	focusBarGlyph = "▎" // U+258E left one-quarter block
	selBarGlyph   = "▌" // U+258C left half block
)

// rateGlyph marks the API-budget segment in the header.
const rateGlyph = "◔" // nerd: nf-md-gauge
