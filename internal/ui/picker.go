package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
)

type picker struct {
	title   string
	cands   []gh.User
	checked map[string]bool
	cursor  int
	filter  textinput.Model
}

func newPicker(title string, cands []gh.User, checked map[string]bool) picker {
	if checked == nil {
		checked = map[string]bool{}
	}
	ti := textinput.New()
	ti.Prompt = "› "
	ti.Focus()
	return picker{title: title, cands: cands, checked: checked, filter: ti}
}

// visible returns the candidates matching the fuzzy filter, in order.
func (p picker) visible() []gh.User {
	q := p.filter.Value()
	if q == "" {
		return p.cands
	}
	hay := make([]string, len(p.cands))
	for i, u := range p.cands {
		hay[i] = u.Login + " " + u.Name
	}
	idx := matchIdx(hay, q)
	out := make([]gh.User, 0, len(idx))
	for _, i := range idx {
		out = append(out, p.cands[i])
	}
	return out
}

// toggleCursor flips the checked state of the candidate under the cursor in the
// currently-visible (filtered) list.
func (p *picker) toggleCursor() {
	vis := p.visible()
	if p.cursor < 0 || p.cursor >= len(vis) {
		return
	}
	login := vis[p.cursor].Login
	if p.checked[login] {
		delete(p.checked, login)
	} else {
		p.checked[login] = true
	}
}

// selected returns the checked logins (order unspecified).
func (p picker) selected() []string {
	out := make([]string, 0, len(p.checked))
	for login, on := range p.checked {
		if on {
			out = append(out, login)
		}
	}
	return out
}

// pickerRows caps how many candidates the floating picker lists at once; the
// filter narrows the set to reach the rest.
const pickerRows = 12

func (m Model) pickerView() string {
	p := m.pick
	var b strings.Builder
	b.WriteString(p.filter.View() + "\n\n")
	switch {
	case p.cands == nil:
		b.WriteString(dimStyle.Render("Loading…"))
	case len(p.cands) == 0:
		b.WriteString(dimStyle.Render("No assignable users."))
	default:
		vis := p.visible()
		start := 0 // scroll the window so the cursor stays visible
		if p.cursor >= pickerRows {
			start = p.cursor - pickerRows + 1
		}
		end := min(start+pickerRows, len(vis))
		for i, u := range vis[start:end] {
			mark := "  "
			if p.checked[u.Login] {
				mark = selMarkStyle.Render("● ")
			}
			cur := "  "
			if start+i == p.cursor {
				cur = accentStyle.Render("▸ ")
			}
			label := "@" + u.Login
			if u.Name != "" {
				label += dimStyle.Render("  " + u.Name)
			}
			b.WriteString(cur + mark + label + "\n")
		}
	}
	body := strings.TrimRight(b.String(), "\n")
	h := lipgloss.Height(body) + 2
	if maxH := m.height - 2; h > maxH {
		h = maxH
	}
	return titledBox(body, 54, h, p.title)
}
