package ui

import "testing"

func TestTicketID(t *testing.T) {
	for _, tc := range []struct{ branch, want string }{
		// Linear shape: team prefix, digits, description.
		{"eng-7726-same-value-different-evidence", "ENG-7726"},
		{"eng-7452-share-one-archer-conversation", "ENG-7452"},
		{"ops-12-rotate-keys", "OPS-12"},
		// GitHub shape: commit type, slash, issue number, description.
		{"feat/213-id-seed-avatars", "#213"},
		{"fix/208-widget-warm-deeplink", "#208"},
		{"chore/220-bump-expo-54", "#220"},
		// No id at all.
		{"agents/spicedb-rel-migrate-88ee", ""},
		{"cursor/guidance-drift-review", ""},
		{"chore-slim-agent-instructions", ""},
		{"main", ""},
		{"", ""},
		// TRAP 1: looks like a Linear key but has no number. A pattern without
		// \d+ emits a bogus "ENG-EMMETT".
		{"eng-emmett-graph-assurance", ""},
		// TRAP 2: a commit-type prefix with no slash. Without the denylist this
		// parses as "FIX-123" for a branch that references no ticket.
		{"fix-123-typo", ""},
		{"feat-42-add-thing", ""},
		{"chore-9-bump", ""},
		// Team prefix longer than 6 chars is not a Linear key.
		{"platform-12-thing", ""},
	} {
		if got := ticketID(tc.branch); got != tc.want {
			t.Errorf("ticketID(%q) = %q, want %q", tc.branch, got, tc.want)
		}
	}
}
