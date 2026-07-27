package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/noamsto/prdash/internal/gh"
	"github.com/noamsto/prdash/internal/preview"
)

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
			body = preview.PlainTitle(t.Comments[0].Body)
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
		b.WriteString(renderDiffHunk(head.DiffHunk, w))
		b.WriteString(renderCommentBody(head.Body, w, "      "))
		for _, reply := range t.Comments[1:] {
			b.WriteString("      " + sepStyle.Render("└ ") + authorStyle(reply.Author).Render(reply.Author) + "\n")
			b.WriteString(renderCommentBody(reply.Body, w, "        "))
		}
	}
	if resolved > 0 {
		b.WriteString("    " + dimStyle.Render(fmt.Sprintf("▸ %d resolved", resolved)) + "\n")
	}
	return b.String()
}

// hunkTailLines bounds how much of a diffHunk we paint. GitHub returns the full
// leading context, but only the lines nearest the comment locate it, and an
// unbounded hunk would push the body it belongs to off screen.
const hunkTailLines = 6

// renderDiffHunk paints a comment's diffHunk as a gutter-prefixed block. The @@
// header is dropped — the L<line> label above already says where we are — and
// only the last hunkTailLines lines are kept. Returns "" for an empty hunk so
// callers draw no gutter at all.
func renderDiffHunk(hunk string, w int) string {
	if hunk == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(hunk, "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "@@") {
		lines = lines[1:]
	}
	if len(lines) > hunkTailLines {
		lines = lines[len(lines)-hunkTailLines:]
	}
	var b strings.Builder
	for _, ln := range lines {
		st := dimStyle
		switch {
		case strings.HasPrefix(ln, "+"):
			st = passStyle
		case strings.HasPrefix(ln, "-"):
			st = failStyle
		}
		b.WriteString("      " + sepStyle.Render("│ ") + st.Render(truncate(ln, w-8)) + "\n")
	}
	return b.String()
}

// indentBlock prefixes every non-blank line with pad. Prefixing is safe on
// already-styled text (unlike slicing, which can cut an ANSI escape), so this
// nests glamour output under its thread without re-wrapping it.
func indentBlock(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			lines[i] = pad + ln
		}
	}
	return strings.Join(lines, "\n")
}

// renderCommentBody renders a comment body as nested markdown, falling back to
// the distilled one-liner if glamour fails — the same precedent as
// renderDiscussionItem, which falls back to raw markdown rather than nothing.
func renderCommentBody(body string, w int, pad string) string {
	out, err := preview.Render(body, w-lipgloss.Width(pad))
	if err != nil {
		return pad + dimStyle.Render(truncate(preview.PlainTitle(body), w-lipgloss.Width(pad))) + "\n"
	}
	return indentBlock(out, pad) + "\n"
}
