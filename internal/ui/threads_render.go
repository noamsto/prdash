package ui

import (
	"fmt"
	"path/filepath"
	"strings"

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
		b.WriteString(focusBarStyle.Render(loc) + "  " + authorStyle(author).Render(author) + "\n")
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
