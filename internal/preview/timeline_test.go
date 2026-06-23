package preview

import (
	"testing"
	"time"

	"github.com/noamsto/prdash/internal/gh"
)

func at(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t }

func TestTimelineSortedAndFolded(t *testing.T) {
	d := gh.PRDetail{
		Comments: []gh.Comment{
			{Body: "c1", CreatedAt: at("2026-06-01T10:00:00Z")},
			{Body: "c3", CreatedAt: at("2026-06-03T10:00:00Z")},
		},
		Reviews: []gh.Review{
			{Body: "r2", State: "APPROVED", SubmittedAt: at("2026-06-02T10:00:00Z")},
		},
	}
	items := Timeline(d)
	if len(items) != 3 || items[0].Body != "c1" || items[2].Body != "c3" {
		t.Fatalf("order wrong: %+v", items)
	}

	older, latest := Fold(items, 2)
	if older != 1 || len(latest) != 2 || latest[0].Body != "r2" {
		t.Fatalf("fold wrong: older=%d latest=%+v", older, latest)
	}
}
