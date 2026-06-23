package ui

import (
	"sort"

	"github.com/sahilm/fuzzy"

	"github.com/noamsto/prdash/internal/action"
)

// filterActions fuzzy-matches "key label" haystacks; empty query → all (sorted by key).
func filterActions(acts map[string]action.Action, query string) []action.Action {
	list := make([]action.Action, 0, len(acts))
	for _, a := range acts {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	if query == "" {
		return list
	}
	hay := make([]string, len(list))
	for i, a := range list {
		hay[i] = a.Key + " " + a.Label
	}
	matches := fuzzy.Find(query, hay)
	out := make([]action.Action, 0, len(matches))
	for _, mch := range matches {
		out = append(out, list[mch.Index])
	}
	return out
}
