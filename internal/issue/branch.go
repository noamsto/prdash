package issue

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	typeOrder = []string{"feat", "fix", "chore", "docs", "refactor"}
)

// Branch derives "{type}/{number}-{slug}" per Noam's convention. type defaults
// to feat, overridden by the first matching commit-type label; slug ≤40 chars.
func Branch(number int, title string, labels []string) string {
	typ := "feat"
	for _, want := range typeOrder {
		for _, l := range labels {
			if strings.EqualFold(l, want) {
				typ = want
			}
		}
	}
	slug := nonSlug.ReplaceAllString(strings.ToLower(title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	return fmt.Sprintf("%s/%d-%s", typ, number, slug)
}
