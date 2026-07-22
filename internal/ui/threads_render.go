package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// renderThreadsSummary is the Overview THREADS block body: top-N unresolved
// threads then a "more / resolved hidden" tail. Empty when nothing is unresolved.
func renderThreadsSummary(ts []gh.ReviewThread, n, w int) string {
	top, more := preview.TopUnresolved(ts, n)
	if len(top) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range top {
		loc := fmt.Sprintf("%s:%d", filepath.Base(t.Path), t.Line)
		author := ""
		body := ""
		if len(t.Comments) > 0 {
			author = t.Comments[0].Author
			body = firstLine(t.Comments[0].Body)
		}
		sep := "  "
		// Budget the line to w by truncating the variable-length author (loc is
		// short and fixed-format) before styling, rather than slicing the
		// already-styled line, which would risk cutting an ANSI escape.
		author = truncate(author, max(0, w-lipgloss.Width(loc)-lipgloss.Width(sep)))
		b.WriteString(focusBarStyle.Render(loc) + sep + authorStyle(author).Render(author) + "\n")
		b.WriteString("  " + dimStyle.Render(truncate(body, w-2)) + "\n")
	}
	tail := []string{}
	if more > 0 {
		tail = append(tail, fmt.Sprintf("%d more", more))
	}
	if r := preview.CountResolved(ts); r > 0 {
		tail = append(tail, fmt.Sprintf("%d resolved hidden", r))
	}
	if len(tail) > 0 {
		b.WriteString(dimStyle.Render("▸ " + strings.Join(tail, " · ")))
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderFileThreads renders one file's threads: unresolved with bodies, resolved
// collapsed to a count line unless showResolved.
func renderFileThreads(g preview.FileThreads, w int, showResolved bool) string {
	var b strings.Builder
	resolved := 0
	for _, t := range g.Threads {
		if t.IsResolved && !showResolved {
			resolved++
			continue
		}
		if len(t.Comments) == 0 {
			continue
		}
		dot := failStyle.Render("●") + " " + failStyle.Render("unresolved")
		if t.IsResolved {
			dot = passStyle.Render("✓ resolved")
		}
		head := t.Comments[0]
		indent, label, sep1, sep2 := "    ", focusBarStyle.Render(fmt.Sprintf("L%d", t.Line)), "  ", "   "
		// Budget the header to w by truncating the variable-length author
		// (indent/label/dot are short and fixed-format) before styling, rather
		// than slicing the already-styled line, which would risk cutting an ANSI
		// escape.
		fixed := lipgloss.Width(indent) + lipgloss.Width(label) + lipgloss.Width(sep1) + lipgloss.Width(sep2) + lipgloss.Width(dot)
		author := truncate(head.Author, max(0, w-fixed))
		b.WriteString(indent + label + sep1 + authorStyle(author).Render(author) + sep2 + dot + "\n")
		b.WriteString("      " + dimStyle.Render(truncate(firstLine(head.Body), w-6)) + "\n")
		for _, reply := range t.Comments[1:] {
			b.WriteString("      " + sepStyle.Render("└ ") + authorStyle(reply.Author).Render(reply.Author) + "\n")
			b.WriteString("        " + dimStyle.Render(truncate(firstLine(reply.Body), w-8)) + "\n")
		}
	}
	if resolved > 0 {
		b.WriteString("    " + dimStyle.Render(fmt.Sprintf("▸ %d resolved", resolved)) + "\n")
	}
	return b.String()
}
