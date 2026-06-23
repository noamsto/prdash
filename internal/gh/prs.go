package gh

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

var prFields = []string{
	"number", "title", "author", "statusCheckRollup", "reviewDecision",
	"labels", "assignees", "headRefName", "baseRefName", "url", "updatedAt",
}

type Check struct {
	State      string `json:"state"`
	Conclusion string `json:"conclusion"`
}

type Label struct {
	Name string `json:"name"`
}

type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	ReviewDecision    string  `json:"reviewDecision"`
	StatusCheckRollup []Check `json:"statusCheckRollup"`
	Labels            []Label `json:"labels"`
	Assignees         []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	HeadRefName string    `json:"headRefName"`
	BaseRefName string    `json:"baseRefName"`
	URL         string    `json:"url"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func PRListArgs(filter string, limit int) []string {
	return []string{
		"pr", "list", "--search", filter,
		"-L", strconv.Itoa(limit), "--json", strings.Join(prFields, ","),
	}
}

func FetchPRs(r Runner, dir, filter string, limit int) ([]PR, error) {
	out, err := r.Run(dir, PRListArgs(filter, limit)...)
	if err != nil {
		return nil, err
	}
	return ParsePRs(out)
}

func ParsePRs(b []byte) ([]PR, error) {
	var prs []PR
	if err := json.Unmarshal(b, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

func (p PR) CIState() string {
	if len(p.StatusCheckRollup) == 0 {
		return "none"
	}
	pending, failed := false, false
	for _, c := range p.StatusCheckRollup {
		s := c.State
		if s == "" {
			s = c.Conclusion
		}
		switch s {
		case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED":
			failed = true
		case "PENDING", "QUEUED", "IN_PROGRESS", "":
			pending = true
		}
	}
	switch {
	case failed:
		return "fail"
	case pending:
		return "pending"
	default:
		return "pass"
	}
}
