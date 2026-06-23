package ui

import (
	"testing"

	"github.com/noamsto/prdash/internal/action"
)

func TestFilterActions(t *testing.T) {
	acts := action.DefaultPRActions()
	got := filterActions(acts, "merge")
	if len(got) == 0 || got[0].Key != "m" {
		t.Fatalf("merge query = %+v", got)
	}
}
