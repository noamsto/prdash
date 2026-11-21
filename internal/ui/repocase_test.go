package ui

import (
	"slices"
	"testing"

	"github.com/noamsto/prdash/internal/gh"
)

// The origin URL's casing (m.repo) can differ from GitHub's nameWithOwner on
// the rows; both must still name the launch repo.
func TestLaunchRepoCaseMismatch(t *testing.T) {
	m := NewModel("/repo", "is:open", nil)
	m.SetRepo("Owner/Repo")
	m.width, m.height = 100, 40
	review, open, _ := repoGroupFixture()
	cross := []gh.PR{repoPR("a/b", 5, "zed", "2026-01-01")}
	m.paintSections(review, nil, open, cross, "me", true, false)

	want := []string{"Owner/Repo#01", "Owner/Repo#02", "Owner/Repo#03", "a/b#05"}
	if got := boardLayout(m); !slices.Equal(got, want) {
		t.Fatalf("layout = %v, want %v", got, want)
	}
	if dir, err := m.checkoutDir("owner/repo"); err != nil || dir != m.dir {
		t.Fatalf("checkoutDir(owner/repo) = %q, %v, want %q", dir, err, m.dir)
	}
}

func TestSetPRsNormalizesLaunchRepoCase(t *testing.T) {
	m := NewModel("/repo", "is:open author:@me", nil)
	m.SetRepo("Owner/Repo")
	m.width, m.height = 100, 40
	m.setPRs([]gh.PR{repoPR("owner/repo", 1, "me", "2026-01-01")})

	if got := boardLayout(m); !slices.Equal(got, []string{"Owner/Repo#01"}) {
		t.Fatalf("layout = %v, want [Owner/Repo#01]", got)
	}
}
