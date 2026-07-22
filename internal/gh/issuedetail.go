package gh

// TimelineItem is the placeholder for the future issue comments/events timeline.
// Defined empty now so IssueDetail's shape is stable when the timeline lands.
type TimelineItem struct{}

type IssueDetail struct {
	Body     string         `json:"body"`
	Timeline []TimelineItem `json:"-"` // populated by a later milestone, not fetched yet
}
