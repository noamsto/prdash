package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/noamsto/prdash/internal/preview"
)

func TestRenderPreviewBodyShowsOlderMarker(t *testing.T) {
	items := make([]preview.Item, 5)
	for i := range items {
		items[i] = preview.Item{Author: "a", Body: "msg", At: time.Unix(int64(i), 0), Kind: preview.KindComment}
	}
	out := renderTimeline(items, 3, 80, false) // n=3, not expanded
	if !strings.Contains(out, "earlier") {
		t.Fatalf("expected older marker: %q", out)
	}
}
