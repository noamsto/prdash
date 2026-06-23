package issue

import "testing"

func TestBranchDefaultType(t *testing.T) {
	got := Branch(213, "Seed avatars by id", nil)
	if got != "feat/213-seed-avatars-by-id" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchLabelOverride(t *testing.T) {
	got := Branch(8, "Crash on launch!", []string{"bug", "fix"})
	if got != "fix/8-crash-on-launch" {
		t.Fatalf("got %q", got)
	}
}

func TestBranchSlugTrims(t *testing.T) {
	long := "This is a very long issue title that should be truncated nicely here"
	got := Branch(1, long, nil)
	if len(got) > 4+1+1+1+40 { // "feat/" + "1" + "-" + ≤40
		t.Fatalf("slug too long: %q", got)
	}
}
