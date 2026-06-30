package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
	"github.com/noamsto/prdash/internal/triage"
)

type prDetailMsg struct {
	number int
	detail gh.PRDetail
}

// fetchDetailCmd lazily loads the selected PR's comments/reviews.
func (m Model) fetchDetailCmd(number int) tea.Cmd {
	r, dir := m.runner, m.dir
	return func() tea.Msg {
		d, err := gh.FetchPRDetail(r, dir, number)
		if err != nil {
			return fetchFailedMsg{err}
		}
		return prDetailMsg{number: number, detail: d}
	}
}

// detailCmdForCursor fetches the cursor PR's detail if it isn't cached yet.
func (m *Model) detailCmdForCursor() tea.Cmd {
	if m.runner == nil || m.section.Kind() != "pr" {
		return nil
	}
	v, ok := m.cursorVars()
	if !ok {
		return nil
	}
	if _, cached := m.detail[v.Number]; cached {
		return nil
	}
	return m.fetchDetailCmd(v.Number)
}

// prefetchWindow bounds how many uncached PR details we fan out per settle.
const prefetchWindow = 5

// prefetchNumbers returns up to window uncached PR numbers from cursor downward.
func prefetchNumbers(ps *PRSection, cursor int, detail map[int]gh.PRDetail, window int) []int {
	var out []int
	for i := cursor; i < ps.Len() && len(out) < window; i++ {
		num := ps.prAt(i).Number
		if _, cached := detail[num]; cached {
			continue
		}
		out = append(out, num)
	}
	return out
}

// prefetchCmd warms detail for a bounded window of visible PRs so the ! column
// and the side card fill in without a fetch per keystroke.
func (m Model) prefetchCmd() tea.Cmd {
	ps, ok := m.section.(*PRSection)
	if !ok || m.runner == nil {
		return nil
	}
	nums := prefetchNumbers(ps, m.cursor, m.detail, prefetchWindow)
	if len(nums) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(nums))
	for _, n := range nums {
		cmds = append(cmds, m.fetchDetailCmd(n))
	}
	return tea.Batch(cmds...)
}

// renderTimeline renders the latest n items expanded, older collapsed.
func renderTimeline(items []preview.Item, n, width int, expanded bool) string {
	older, latest := preview.Fold(items, n)
	if expanded {
		older, latest = 0, items
	}
	var b strings.Builder
	if older > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("▸ %d earlier comments", older)) + "\n\n")
	}
	sep := sepStyle.Render(strings.Repeat("─", width))
	for i, it := range latest {
		if i > 0 {
			b.WriteString(sep + "\n\n")
		}
		body, err := preview.Render(it.Body, width)
		if err != nil {
			body = it.Body // render failed; show the raw markdown rather than nothing
		}
		b.WriteString(metaLine(it.Author, it.State, it.At) + "\n" + body + "\n")
	}
	return b.String()
}

func (m Model) previewWidth() int {
	l := computeLayout(m.width, m.height)
	if !l.ShowSide {
		return 40
	}
	if m.previewMax {
		return m.width - 2 // interior of the full-width box
	}
	return l.SideWidth - 2
}

// identityHeader is the side card's top block: number + title, then a dim
// author · branch · age line. The branch anchors the copy/worktree actions.
func identityHeader(pr gh.PR) string {
	line1 := accentStyle.Render(fmt.Sprintf("#%d", pr.Number)) + " " + headerStyle.Render(pr.Title)
	line2 := authorStyle(pr.Author.Login).Render(pr.Author.Login) +
		dimStyle.Render(" · "+pr.HeadRefName+" · "+ageString(pr.UpdatedAt))
	return line1 + "\n" + line2
}

// sectionRule is a dim labeled divider inside the preview: "label ─────".
func sectionRule(label string, w int) string {
	rest := w - lipgloss.Width(label) - 1
	if rest < 0 {
		rest = 0
	}
	return dimStyle.Render(label) + " " + sepStyle.Render(strings.Repeat("─", rest))
}

// previewPane renders the triage card (if available) followed by the timeline,
// or a loading/empty hint.
func (m Model) previewPane() string {
	v, ok := m.cursorVars()
	if !ok {
		return ""
	}
	d, cached := m.detail[v.Number]
	if !cached {
		return "Loading preview…"
	}
	w := m.previewWidth()
	// A section's label sits directly on its body; the blank line between blocks
	// (the join below) is what separates sections, so they breathe.
	section := func(label, body string) string {
		return sectionRule(label, w) + "\n" + body
	}
	var blocks []string
	if ps, ok := m.section.(*PRSection); ok {
		pr := ps.prAt(m.cursor)
		blocks = append(blocks, identityHeader(pr))
		tc := triage.Compute(pr, d)
		if card := renderCard(tc, w); card != "" {
			blocks = append(blocks, section("blocker", strings.TrimRight(card, "\n")))
		}
		// The checks section is redundant when the blocker card is already about
		// CI; show it only when the blocker is something else (review/conflict)
		// that would otherwise mask failing checks.
		if tc.Kind != triage.KindChecksFailing && tc.Kind != triage.KindChecksRunning {
			if ci := ciLine(pr); ci != "" {
				blocks = append(blocks, section("checks", ci))
			}
		}
	}
	blocks = append(blocks, section("review", reviewersLine(d.ReviewRequests)))
	blocks = append(blocks, section("latest", renderTimeline(preview.Timeline(d), m.previewN, w, m.previewExpanded)))
	return strings.Join(blocks, "\n\n")
}

// previewTitle is the side pane's border title.
func (m Model) previewTitle() string {
	if v, ok := m.cursorVars(); ok && v.Number > 0 {
		return fmt.Sprintf("#%d", v.Number)
	}
	return "Preview"
}

// ciLine surfaces the check rollup in the quick view independent of the triage
// card, which keys off mergeStateStatus and can mask failing CI behind a
// review/conflict headline.
func ciLine(pr gh.PR) string {
	switch pr.CIState() {
	case "fail":
		var names []string
		for _, c := range pr.StatusCheckRollup {
			if c.Result() == "fail" {
				names = append(names, c.Label())
			}
		}
		s := failStyle.Render("  ✗ checks failing")
		if len(names) > 0 {
			s += dimStyle.Render(": " + strings.Join(names, ", "))
		}
		return s
	case "pending":
		return pendStyle.Render("  ● checks running")
	default: // pass / none — the row glyph carries it; keep the quick view calm
		return ""
	}
}

// reviewersLine summarises requested reviewers for the quick window. Team
// requests have no login and are skipped.
func reviewersLine(reqs []gh.ReviewRequest) string {
	var logins []string
	for _, r := range reqs {
		if r.Login != "" {
			logins = append(logins, r.Login)
		}
	}
	if len(logins) == 0 {
		return pendStyle.Render("  ⚠ no reviewers")
	}
	return dimStyle.Render("  reviewers: " + strings.Join(logins, ", "))
}

// flagGlyph is the board's ! column: a conflict (red) or behind-base (yellow)
// marker. It is detail-derived — blank unless the PR's detail is cached, so the
// board never guesses a blocker from the unreliable bulk list.
func flagGlyph(d gh.PRDetail, cached bool) string {
	if !cached {
		return ""
	}
	switch {
	case d.MergeStateStatus == "DIRTY" || d.Mergeable == "CONFLICTING":
		return failStyle.Render("⚠")
	case d.MergeStateStatus == "BEHIND":
		return pendStyle.Render("⚠")
	default:
		return ""
	}
}

// renderMain lays the bordered list and (when wide) the bordered side preview.
func (m Model) renderMain() string {
	l := computeLayout(m.width, m.height)
	if m.previewMax && l.ShowSide {
		return titledBox(m.previewPane(), m.width, l.ContentHeight, m.previewTitle())
	}
	list := titledBox(m.vp.View(), l.ListWidth, l.ContentHeight, m.listTitle())
	if !l.ShowSide {
		return list
	}
	side := titledBox(m.previewPane(), l.SideWidth, l.ContentHeight, m.previewTitle())
	side = lipgloss.NewStyle().MarginLeft(l.Gap).Render(side)
	return lipgloss.JoinHorizontal(lipgloss.Top, list, side)
}
