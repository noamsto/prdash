package preview

import (
	"sort"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

type Kind int

const (
	KindComment Kind = iota
	KindReview
)

type Item struct {
	Author string
	Body   string
	At     time.Time
	Kind   Kind
	State  string // review state, when KindReview
}

// Timeline merges conversation comments and review summaries, sorted oldest→newest.
func Timeline(d gh.PRDetail) []Item {
	items := make([]Item, 0, len(d.Comments)+len(d.Reviews))
	for _, c := range d.Comments {
		items = append(items, Item{Author: c.Author.Login, Body: c.Body, At: c.CreatedAt, Kind: KindComment})
	}
	for _, r := range d.Reviews {
		if r.Body == "" && r.State == "" {
			continue
		}
		items = append(items, Item{Author: r.Author.Login, Body: r.Body, At: r.SubmittedAt, Kind: KindReview, State: r.State})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.Before(items[j].At) })
	return items
}

// Fold returns (olderCount, latestN). The latest n items are shown expanded; the
// rest collapse behind a "▸ {older} earlier" row.
func Fold(items []Item, n int) (int, []Item) {
	if len(items) <= n {
		return 0, items
	}
	return len(items) - n, items[len(items)-n:]
}
