package preview

import (
	"regexp"
	"strings"
)

// Inline markdown patterns used to distill a body down to plain text.
var (
	mdImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	htmlTag = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	mdEmph  = regexp.MustCompile("[*`]+")
	wsRun   = regexp.MustCompile(`\s+`)
)

// PlainTitle distills a comment body to a single line of plain text, for
// summary rows that have one line to spend. Those rows never reach glamour, so
// the style config that cleans up rendered bodies does nothing for them.
//
// It returns the first line that is non-empty *after* distillation — a bot
// comment whose first line is only a severity badge distills to "" and must fall
// through rather than yielding a blank row. Returns "" when no line survives.
func PlainTitle(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		s := mdImage.ReplaceAllString(ln, "")
		s = mdLink.ReplaceAllString(s, "$1")
		s = htmlTag.ReplaceAllString(s, "")
		s = strings.TrimLeft(s, "#> \t")
		s = mdEmph.ReplaceAllString(s, "")
		s = strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
		if s != "" {
			return s
		}
	}
	return ""
}
