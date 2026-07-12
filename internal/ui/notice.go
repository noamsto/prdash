package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// noticeModel is a full-screen message that stays put until the user presses a
// key — so a startup failure is readable instead of flashing past (e.g. when
// prdash is launched in a tmux popup that would otherwise close instantly).
type noticeModel struct {
	title, body string
	w, h        int
}

func (m noticeModel) Init() tea.Cmd { return nil }

func (m noticeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m noticeModel) View() tea.View {
	block := lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render(m.title), "",
		titleStyle.Render(m.body), "",
		dimStyle.Render("press any key to quit"))
	content := block
	if m.w > 0 && m.h > 0 {
		content = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, block)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// RunNotice paints title + body full-screen in the active palette and blocks
// until a keypress. Used for fatal startup conditions that aren't crashes.
func RunNotice(title, body string) error {
	applyTheme(themeFor(detectTheme()))
	_, err := tea.NewProgram(noticeModel{title: title, body: body}).Run()
	return err
}
