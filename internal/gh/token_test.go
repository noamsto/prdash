package gh

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenPrefersGHToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok-gh")
	t.Setenv("GITHUB_TOKEN", "tok-github")
	got, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	if got != "tok-gh" {
		t.Errorf("Token() = %q, want tok-gh (GH_TOKEN wins)", got)
	}
}

func TestTokenFallsBackToGithubToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "tok-github")
	got, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	if got != "tok-github" {
		t.Errorf("Token() = %q, want tok-github", got)
	}
}

func TestTokenTimesOutOnStalledGh(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	old := execTimeout
	execTimeout = 20 * time.Millisecond
	t.Cleanup(func() { execTimeout = old })
	fakeBinOnPath(t, "gh", "exec sleep 30\n")

	_, err := Token()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Token() = %v, want a deadline-exceeded error", err)
	}
}

func TestTokenReportsCleanFailureFromResponsiveGh(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	fakeBinOnPath(t, "gh", "echo not logged in >&2\nexit 1\n")

	_, err := Token()
	if err == nil {
		t.Fatal("expected an error from an unauthenticated gh")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Token() = %v, want a plain failure, not a timeout", err)
	}
}
