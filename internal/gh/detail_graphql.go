package gh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// detailFields is the GraphQL selection matching what `gh pr view --json
// comments,reviews,latestReviews,mergeStateStatus,mergeable,isDraft,reviewRequests,files`
// fetches. mergeStateStatus needs the merge-info preview media type (set on the
// request), else GitHub rejects the field.
const detailFields = "comments(last:100){nodes{author{login __typename}body createdAt}}" +
	"reviews(first:100){nodes{author{login __typename}body state submittedAt}}" +
	"latestReviews(first:20){nodes{author{login __typename}body state submittedAt}}" +
	"mergeStateStatus mergeable isDraft " +
	"reviewRequests(first:100){nodes{requestedReviewer{__typename ... on User{login} ... on Bot{login} ... on Team{name}}}}" +
	"files(first:100){nodes{path additions deletions}}"

// FetchDetails fetches per-PR detail for every number in one aliased GraphQL
// request, replacing the N `gh pr view` subprocesses the prefetch window used to
// spawn. It returns the mapped details plus, per number, the JSON bytes to cache
// (marshalled PRDetail, so the hydrate path round-trips).
func (s GraphSource) FetchDetails(numbers []int) (map[int]PRDetail, map[int][]byte, error) {
	if len(numbers) == 0 {
		return map[int]PRDetail{}, map[int][]byte{}, nil
	}
	owner, name, ok := strings.Cut(s.repo, "/")
	if !ok {
		return nil, nil, fmt.Errorf("bad repo %q", s.repo)
	}
	reqBody, err := json.Marshal(map[string]any{
		"query":     buildDetailQuery(numbers),
		"variables": map[string]string{"owner": owner, "name": name},
	})
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequest(http.MethodPost, githubGraphQLURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.merge-info-preview+json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("graphql detail: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return parseDetails(body, numbers)
}

// buildDetailQuery aliases one pullRequest(number:) selection per number under a
// single repository query. Numbers are ints, so inlining them is injection-safe.
func buildDetailQuery(numbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner:String!,$name:String!){repository(owner:$owner,name:$name){")
	for _, n := range numbers {
		fmt.Fprintf(&b, "pr%d:pullRequest(number:%d){%s}", n, n, detailFields)
	}
	b.WriteString("}}")
	return b.String()
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
}

func parseDetails(body []byte, numbers []int) (map[int]PRDetail, map[int][]byte, error) {
	var resp struct {
		Data struct {
			Repository map[string]qlDetail `json:"repository"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("parse detail: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, nil, fmt.Errorf("graphql detail: %s", resp.Errors[0].Message)
	}
	details := make(map[int]PRDetail, len(numbers))
	raws := make(map[int][]byte, len(numbers))
	for _, n := range numbers {
		ql, ok := resp.Data.Repository[fmt.Sprintf("pr%d", n)]
		if !ok {
			continue
		}
		d := mapDetail(ql)
		details[n] = d
		if raw, err := json.Marshal(d); err == nil {
			raws[n] = raw
		}
	}
	return details, raws, nil
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
