package gh

import "testing"

func TestParseIssueDetail(t *testing.T) {
	d, err := ParseIssueDetail([]byte(`{"body":"## Hello\nworld"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Body != "## Hello\nworld" {
		t.Errorf("body = %q", d.Body)
	}
	if d.Timeline != nil {
		t.Errorf("Timeline should be empty in v1, got %v", d.Timeline)
	}
}

func TestIssueViewArgs(t *testing.T) {
	got := IssueViewArgs(42)
	want := []string{"issue", "view", "42", "--json", "body"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
