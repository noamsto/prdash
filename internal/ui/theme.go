package ui

import "github.com/charmbracelet/lipgloss"

// Palette roles. Concrete colors inherit the terminal's theme (lazytmux
// Catppuccin overlay); these adaptive defaults read well on dark backgrounds.
var (
	accentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))  // blue
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241")) // gray
	mutedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	passStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))  // green
	failStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")) // red
	pendStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // yellow
	selMarkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // mauve
	cursorRowStyle = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	headerStyle    = accentStyle.Bold(true)
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

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
