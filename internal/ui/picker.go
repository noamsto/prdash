package ui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"

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

func (m Model) pickerView() string {
	p := m.pick
	var b strings.Builder
	b.WriteString(headerStyle.Render("  "+p.title) + "\n")
	b.WriteString("  " + p.filter.View() + "\n\n")
	if p.cands == nil {
		b.WriteString(dimStyle.Render("  Loading…"))
		return b.String()
	}
	for i, u := range p.visible() {
		mark := "  "
		if p.checked[u.Login] {
			mark = selMarkStyle.Render("● ")
		}
		cur := "  "
		if i == p.cursor {
			cur = "> "
		}
		label := "@" + u.Login
		if u.Name != "" {
			label += dimStyle.Render("  " + u.Name)
		}
		b.WriteString(cur + mark + label + "\n")
	}
	if len(p.cands) == 0 {
		b.WriteString(dimStyle.Render("  No assignable users."))
	}
	return b.String()
}
