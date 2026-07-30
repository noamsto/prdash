package gh

import "time"

type ThreadComment struct {
	Author    string
	Body      string
	DiffHunk  string
	CreatedAt time.Time
}

type ReviewThread struct {
	Path       string
	Line       int
	IsResolved bool
	Comments   []ThreadComment
}

// threadsFields selects the review threads the preview renders. It is part of
// detailFields, not a query of its own — see PRDetail.ReviewThreads.
//
// The 20 is load-bearing, and it is the OUTER page size that matters: `comments`
// resolves once per thread, so a detail batch pays (PRs × threads) parent nodes
// for it. At first:100 a five-PR window measured 5 points; at 20 it measures 1,
// the same as not fetching threads at all. Keep threadsPageSize × prefetchWindow
// at or under 100. `comments(first:100)` is free by contrast — a leaf
// connection's own page size never moves the cost.
const threadsFields = "reviewThreads(first:20){nodes{isResolved path line originalLine" +
	" comments(first:100){nodes{author{login} body diffHunk createdAt}}}}"

// qlThreads is the JSON shape threadsFields returns.
type qlThreads struct {
	Nodes []struct {
		IsResolved   bool   `json:"isResolved"`
		Path         string `json:"path"`
		Line         *int   `json:"line"`
		OriginalLine *int   `json:"originalLine"`
		Comments     struct {
			Nodes []struct {
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				Body      string    `json:"body"`
				DiffHunk  string    `json:"diffHunk"`
				CreatedAt time.Time `json:"createdAt"`
			} `json:"nodes"`
		} `json:"comments"`
	} `json:"nodes"`
}

// mapThreads flattens the connection into []ReviewThread. GitHub reports line as
// null on a thread whose diff moved, so originalLine is the fallback.
func mapThreads(q qlThreads) []ReviewThread {
	out := make([]ReviewThread, 0, len(q.Nodes))
	for _, n := range q.Nodes {
		line := 0
		if n.Line != nil {
			line = *n.Line
		} else if n.OriginalLine != nil {
			line = *n.OriginalLine
		}
		cs := make([]ThreadComment, 0, len(n.Comments.Nodes))
		for _, c := range n.Comments.Nodes {
			cs = append(cs, ThreadComment{Author: c.Author.Login, Body: c.Body, DiffHunk: c.DiffHunk, CreatedAt: c.CreatedAt})
		}
		out = append(out, ReviewThread{Path: n.Path, Line: line, IsResolved: n.IsResolved, Comments: cs})
	}
	return out
}
