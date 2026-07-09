package gh

import (
	"encoding/json"
	"strconv"
)

// TimelineItem is the placeholder for the future issue comments/events timeline.
// Defined empty now so IssueDetail's shape is stable when the timeline lands.
type TimelineItem struct{}

type IssueDetail struct {
	Body     string         `json:"body"`
	Timeline []TimelineItem `json:"-"` // populated by a later milestone, not fetched yet
}

func IssueViewArgs(number int) []string {
	return []string{"issue", "view", strconv.Itoa(number), "--json", "body"}
}

func ParseIssueDetail(b []byte) (IssueDetail, error) {
	var d IssueDetail
	err := json.Unmarshal(b, &d)
	return d, err
}
