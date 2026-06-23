package ui

import (
	"fmt"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/gh"
)

// haystack builds the all-fields searchable string for one PR.
func haystack(p gh.PR) string {
	parts := []string{
		fmt.Sprintf("#%d", p.Number), p.Title, p.Author.Login,
		p.HeadRefName, p.BaseRefName, p.ReviewDecision, p.CIState(),
	}
	for _, a := range p.Assignees {
		parts = append(parts, a.Login)
	}
	for _, l := range p.Labels {
		parts = append(parts, l.Name)
	}
	return strings.Join(parts, " ")
}

// filterPRs fuzzy-matches query across all fields, ranked by score. Empty
// query returns the input unchanged.
func filterPRs(prs []gh.PR, query string) []gh.PR {
	if strings.TrimSpace(query) == "" {
		return prs
	}
	hay := make([]string, len(prs))
	for i, p := range prs {
		hay[i] = haystack(p)
	}
	matches := fuzzy.Find(query, hay)
	out := make([]gh.PR, 0, len(matches))
	for _, m := range matches {
		out = append(out, prs[m.Index])
	}
	return out
}

// matchIdx returns the indices of hay that fuzzy-match query, ranked by score.
// Empty query returns all indices in order.
func matchIdx(hay []string, query string) []int {
	if strings.TrimSpace(query) == "" {
		return allIdx(len(hay))
	}
	matches := fuzzy.Find(query, hay)
	idx := make([]int, 0, len(matches))
	for _, m := range matches {
		idx = append(idx, m.Index)
	}
	return idx
}
