package gh

import "testing"

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
