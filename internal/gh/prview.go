package gh

import "time"

type Comment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type Review struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type ReviewRequest struct {
	Login string `json:"login"`
}

type DiffFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type Diffstat struct {
	Files, Additions, Deletions int
}

type PRDetail struct {
	Comments         []Comment       `json:"comments"`
	Reviews          []Review        `json:"reviews"`
	LatestReviews    []Review        `json:"latestReviews"`
	MergeStateStatus string          `json:"mergeStateStatus"`
	Mergeable        string          `json:"mergeable"`
	IsDraft          bool            `json:"isDraft"`
	ReviewRequests   []ReviewRequest `json:"reviewRequests"`
	Files            []DiffFile      `json:"files"`
	// ReviewThreads rides the same query rather than costing a second request per
	// PR. It is one more connection on a document that already resolves several,
	// so the batch's cost does not move — and the prefetch window now warms
	// threads too, which used to be a fetch per row visited.
	ReviewThreads []ReviewThread `json:"reviewThreads"`
}

// Diffstat aggregates the per-file changes into totals for the card/Diff tab.
func (d PRDetail) Diffstat() Diffstat {
	s := Diffstat{Files: len(d.Files)}
	for _, f := range d.Files {
		s.Additions += f.Additions
		s.Deletions += f.Deletions
	}
	return s
}
