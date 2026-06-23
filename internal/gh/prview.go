package gh

import (
	"encoding/json"
	"strconv"
	"time"
)

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

func PRViewArgs(number int) []string {
	return []string{"pr", "view", strconv.Itoa(number), "--json",
		"comments,reviews,latestReviews,mergeStateStatus,mergeable,isDraft,reviewRequests,files"}
}

func FetchPRDetail(r Runner, dir string, number int) (PRDetail, error) {
	out, err := r.Run(dir, PRViewArgs(number)...)
	if err != nil {
		return PRDetail{}, err
	}
	return ParsePRDetail(out)
}

func ParsePRDetail(b []byte) (PRDetail, error) {
	var d PRDetail
	err := json.Unmarshal(b, &d)
	return d, err
}
