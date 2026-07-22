package gh

import (
	"strings"
	"time"
)

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

// IssuesDisabled reports whether err is gh's "repository has disabled issues"
// failure — a normal state for repos that track work in an external tracker,
// so the caller shows an empty board rather than an error.
func IssuesDisabled(err error) bool {
	return err != nil && strings.Contains(err.Error(), "has disabled issues")
}
