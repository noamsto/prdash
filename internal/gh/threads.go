package gh

import (
	"encoding/json"
	"strconv"
	"time"
)

type ThreadComment struct {
	Author    string
	Body      string
	CreatedAt time.Time
}

type ReviewThread struct {
	Path       string
	Line       int
	IsResolved bool
	Comments   []ThreadComment
}

const reviewThreadsQuery = `query($owner:String!,$repo:String!,$num:Int!){repository(owner:$owner,name:$repo){pullRequest(number:$num){reviewThreads(first:100){nodes{isResolved path line originalLine comments(first:100){nodes{author{login} body createdAt}}}}}}}`

func ReviewThreadsArgs(owner, repo string, number int) []string {
	return []string{
		"api", "graphql",
		"-f", "query=" + reviewThreadsQuery,
		"-F", "owner=" + owner,
		"-F", "repo=" + repo,
		"-F", "num=" + strconv.Itoa(number),
	}
}

func ParseReviewThreads(b []byte) ([]ReviewThread, error) {
	var env struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
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
									CreatedAt time.Time `json:"createdAt"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, err
	}
	nodes := env.Data.Repository.PullRequest.ReviewThreads.Nodes
	out := make([]ReviewThread, 0, len(nodes))
	for _, n := range nodes {
		line := 0
		if n.Line != nil {
			line = *n.Line
		} else if n.OriginalLine != nil {
			line = *n.OriginalLine
		}
		cs := make([]ThreadComment, 0, len(n.Comments.Nodes))
		for _, c := range n.Comments.Nodes {
			cs = append(cs, ThreadComment{Author: c.Author.Login, Body: c.Body, CreatedAt: c.CreatedAt})
		}
		out = append(out, ReviewThread{Path: n.Path, Line: line, IsResolved: n.IsResolved, Comments: cs})
	}
	return out, nil
}
