package preview

import (
	"sort"

	"github.com/noamsto/prdash/internal/gh"
)

func Unresolved(ts []gh.ReviewThread) []gh.ReviewThread {
	out := make([]gh.ReviewThread, 0, len(ts))
	for _, t := range ts {
		if !t.IsResolved {
			out = append(out, t)
		}
	}
	return out
}

func CountResolved(ts []gh.ReviewThread) int {
	n := 0
	for _, t := range ts {
		if t.IsResolved {
			n++
		}
	}
	return n
}

// TopUnresolved returns the first n unresolved threads plus the count of
// unresolved threads beyond n.
func TopUnresolved(ts []gh.ReviewThread, n int) (top []gh.ReviewThread, more int) {
	u := Unresolved(ts)
	if len(u) <= n {
		return u, 0
	}
	return u[:n], len(u) - n
}

type FileThreads struct {
	Path    string
	Threads []gh.ReviewThread
}

// GroupByFile groups threads by path in first-seen order; within a file,
// unresolved threads sort before resolved.
func GroupByFile(ts []gh.ReviewThread) []FileThreads {
	order := []string{}
	byPath := map[string][]gh.ReviewThread{}
	for _, t := range ts {
		if _, seen := byPath[t.Path]; !seen {
			order = append(order, t.Path)
		}
		byPath[t.Path] = append(byPath[t.Path], t)
	}
	out := make([]FileThreads, 0, len(order))
	for _, p := range order {
		g := byPath[p]
		sort.SliceStable(g, func(i, j int) bool { return !g[i].IsResolved && g[j].IsResolved })
		out = append(out, FileThreads{Path: p, Threads: g})
	}
	return out
}
