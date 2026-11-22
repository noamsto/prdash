package gh

import (
	"encoding/json"
	"time"
)

// detailFields is everything the preview needs for one PR, review threads
// included: they used to be a second request per row visited, and folding them in
// costs nothing (see PRDetail.ReviewThreads). mergeStateStatus needs the
// merge-info preview media type (set on the request), else GitHub rejects it.
const detailFields = "comments(last:100){nodes{author{login __typename}body createdAt}}" +
	"reviews(first:100){nodes{author{login __typename}body state submittedAt}}" +
	"latestReviews(first:20){nodes{author{login __typename}body state submittedAt}}" +
	"mergeStateStatus mergeable isDraft " +
	"reviewRequests(first:100){nodes{requestedReviewer{__typename ... on User{login} ... on Bot{login} ... on Team{name}}}}" +
	"files(first:100){nodes{path additions deletions}}" + threadsFields

// detailSpec's 20-PR cap keeps a chunk near 50k of GitHub's 500k nodes: each PR
// costs ~2.4k (threadsFields' 20 threads × 100 comments dominates).
var detailSpec = batchSpec{
	label: "detail", accept: "application/vnd.github.merge-info-preview+json",
	aliasPrefix: "pr", field: "pullRequest",
	selection: detailFields, maxPRs: 20, strict: true,
}

// FetchDetails fetches per-PR detail, batching every repo into one aliased
// GraphQL request per chunk. It returns the mapped details plus, per ref, the
// JSON bytes to cache (marshalled PRDetail, so the hydrate path round-trips). A
// failing repo leaves the others' results in the maps and joins its error into
// err.
func (s GraphSource) FetchDetails(refs []PRRef) (map[PRRef]PRDetail, map[PRRef][]byte, error) {
	details := make(map[PRRef]PRDetail, len(refs))
	raws := make(map[PRRef][]byte, len(refs))
	err := s.fetchAliased(detailSpec, refs, func(ref PRRef, raw json.RawMessage) error {
		var ql qlDetail
		if err := json.Unmarshal(raw, &ql); err != nil {
			return err
		}
		d := mapDetail(ql)
		details[ref] = d
		if b, err := json.Marshal(d); err == nil {
			raws[ref] = b
		}
		return nil
	})
	return details, raws, err
}

type qlActor struct {
	Login    string `json:"login"`
	Typename string `json:"__typename"`
}

type qlComment struct {
	Author    qlActor   `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

type qlReview struct {
	Author      qlActor   `json:"author"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type qlDetail struct {
	Comments         struct{ Nodes []qlComment } `json:"comments"`
	Reviews          struct{ Nodes []qlReview }  `json:"reviews"`
	LatestReviews    struct{ Nodes []qlReview }  `json:"latestReviews"`
	MergeStateStatus string                      `json:"mergeStateStatus"`
	Mergeable        string                      `json:"mergeable"`
	IsDraft          bool                        `json:"isDraft"`
	ReviewRequests   struct {
		Nodes []struct {
			RequestedReviewer struct {
				Login string `json:"login"`
				Name  string `json:"name"` // Team has no login; gh shows its slug
			} `json:"requestedReviewer"`
		}
	} `json:"reviewRequests"`
	Files struct {
		Nodes []struct {
			Path      string `json:"path"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		}
	} `json:"files"`
	ReviewThreads qlThreads `json:"reviewThreads"`
}

func mapDetail(q qlDetail) PRDetail {
	d := PRDetail{
		MergeStateStatus: q.MergeStateStatus,
		Mergeable:        q.Mergeable,
		IsDraft:          q.IsDraft,
	}
	for _, c := range q.Comments.Nodes {
		var cm Comment
		cm.Author.Login = actorLogin(c.Author)
		cm.Body = c.Body
		cm.CreatedAt = c.CreatedAt
		d.Comments = append(d.Comments, cm)
	}
	d.Reviews = mapReviews(q.Reviews.Nodes)
	d.LatestReviews = mapReviews(q.LatestReviews.Nodes)
	for _, r := range q.ReviewRequests.Nodes {
		login := r.RequestedReviewer.Login
		if login == "" {
			login = r.RequestedReviewer.Name
		}
		if login == "" {
			continue // requestedReviewer null/unresolvable (e.g. a team the token can't see); gh drops these
		}
		d.ReviewRequests = append(d.ReviewRequests, ReviewRequest{Login: login})
	}
	for _, f := range q.Files.Nodes {
		d.Files = append(d.Files, DiffFile{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions})
	}
	d.ReviewThreads = mapThreads(q.ReviewThreads)
	sanitize(&d)
	return d
}

func mapReviews(nodes []qlReview) []Review {
	out := make([]Review, 0, len(nodes))
	for _, r := range nodes {
		var rv Review
		rv.Author.Login = actorLogin(r.Author)
		rv.Body = r.Body
		rv.State = r.State
		rv.SubmittedAt = r.SubmittedAt
		out = append(out, rv)
	}
	return out
}

// actorLogin mirrors gh's app/ prefix on GitHub App (Bot) actors.
func actorLogin(a qlActor) string {
	if a.Typename == "Bot" && a.Login != "" {
		return "app/" + a.Login
	}
	return a.Login
}
