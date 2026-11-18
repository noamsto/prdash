package gh

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutFor(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	clone := func(t *testing.T, owner, name, origin string) string {
		t.Helper()
		dir := filepath.Join(root, owner, name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		git(t, dir, "init", "-b", "main")
		git(t, dir, "remote", "add", "origin", origin)
		return dir
	}

	t.Run("match", func(t *testing.T) {
		want := clone(t, "owner", "name", "https://github.com/Owner/Name.git")
		got, err := CheckoutFor(root, "owner/name")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})

	t.Run("missing", func(t *testing.T) {
		wantPath := filepath.Join(root, "x", "y")
		_, err := CheckoutFor(root, "x/y")
		var ce *CheckoutError
		if !errors.As(err, &ce) {
			t.Fatalf("err = %v, want *CheckoutError", err)
		}
		if ce.Path != wantPath || ce.Origin != "" || ce.Repo != "x/y" {
			t.Fatalf("err = %+v", ce)
		}
		if !strings.Contains(err.Error(), wantPath) {
			t.Fatalf("error %q does not mention %q", err, wantPath)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		path := clone(t, "want", "repo", "https://github.com/other/thing.git")
		_, err := CheckoutFor(root, "want/repo")
		var ce *CheckoutError
		if !errors.As(err, &ce) {
			t.Fatalf("err = %v, want *CheckoutError", err)
		}
		if ce.Path != path || ce.Origin != "other/thing" || ce.Repo != "want/repo" {
			t.Fatalf("err = %+v", ce)
		}
	})

	t.Run("not github", func(t *testing.T) {
		path := clone(t, "lab", "proj", "https://gitlab.com/a/b.git")
		_, err := CheckoutFor(root, "lab/proj")
		var ce *CheckoutError
		if !errors.As(err, &ce) {
			t.Fatalf("err = %v, want *CheckoutError", err)
		}
		if ce.Path != path || ce.Origin != "" || ce.Repo != "lab/proj" {
			t.Fatalf("err = %+v", ce)
		}
	})
}

func TestDefaultReposRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := DefaultReposRoot(), filepath.Join(home, "git"); got != want {
		t.Fatalf("DefaultReposRoot() = %q, want %q", got, want)
	}
}
