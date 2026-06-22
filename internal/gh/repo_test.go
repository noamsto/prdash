package gh

import "testing"

func TestParseRepoFromView(t *testing.T) {
	got, err := parseRepo([]byte(`{"nameWithOwner":"noamsto/prdash"}`))
	if err != nil || got != "noamsto/prdash" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestParseRepoEmpty(t *testing.T) {
	if _, err := parseRepo([]byte(`{}`)); err == nil {
		t.Fatal("expected error on empty repo")
	}
}
