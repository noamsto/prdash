package gh

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var issueFields = []string{"number", "title", "author", "labels", "assignees", "url", "updatedAt"}

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels    []Label `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func IssueListArgs(filter string, limit int) []string {
	return []string{
		"issue", "list", "--search", filter,
		"-L", strconv.Itoa(limit), "--json", strings.Join(issueFields, ","),
	}
}

func FetchIssues(r Runner, dir, filter string, limit int) ([]Issue, error) {
	out, err := r.Run(dir, IssueListArgs(filter, limit)...)
	if err != nil {
		return nil, err
	}
	return ParseIssues(out)
}

func ParseIssues(b []byte) ([]Issue, error) {
	var is []Issue
	if err := json.Unmarshal(b, &is); err != nil {
		return nil, err
	}
	return is, nil
}
