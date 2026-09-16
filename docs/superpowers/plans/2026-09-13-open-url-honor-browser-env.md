# Honor $BROWSER for URL opens Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** prdash opens URLs through `$BROWSER` when it is set, so lazytmux's `og-open` can route a mirrored session's opens to the controlling host.

**Architecture:** `browserArgv` becomes a pure function of `(goos, browser)`. `openURL` feeds it `os.Getenv("BROWSER")`. Every open path (single `o`, bulk `o`, check-log `o`) already funnels through `openURL`, so no call site changes. Spec: `docs/superpowers/specs/2026-09-13-open-url-on-controller-design.md` §1. The lazytmux half (`og-open`, the daemon subscription) is planned in that repo.

**Tech Stack:** Go, standard library only.

---

### Task 1: `browserArgv` prefers `$BROWSER`

**Files:**
- Modify: `internal/ui/browser.go`
- Test: `internal/ui/browser_test.go`

- [ ] **Step 1: Replace the test with a table test covering BROWSER set/unset**

Overwrite `internal/ui/browser_test.go` with:

```go
package ui

import (
	"reflect"
	"testing"
)

func TestBrowserArgv(t *testing.T) {
	tests := []struct {
		name, goos, browser string
		want                []string
	}{
		{"darwin default", "darwin", "", []string{"open"}},
		{"linux default", "linux", "", []string{"xdg-open"}},
		{"darwin honors BROWSER", "darwin", "og-open", []string{"og-open"}},
		{"linux honors BROWSER", "linux", "/usr/bin/firefox", []string{"/usr/bin/firefox"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserArgv(tt.goos, tt.browser); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("browserArgv(%q, %q) = %v, want %v", tt.goos, tt.browser, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails to compile**

Run: `go test ./internal/ui -run TestBrowserArgv`
Expected: FAIL, `too many arguments in call to browserArgv`

- [ ] **Step 3: Implement**

Overwrite `internal/ui/browser.go` with:

```go
package ui

import (
	"os"
	"os/exec"
	"runtime"
)

// browserArgv is the command that opens a URL. $BROWSER wins when set: in a
// lazytmux mirror it names og-open, which hands the URL to the controlling
// host instead of opening a browser on the remote. Split out (like
// clipboardArgv) so the choice is unit-testable without spawning anything.
func browserArgv(goos, browser string) []string {
	if browser != "" {
		return []string{browser}
	}
	if goos == "darwin" {
		return []string{"open"}
	}
	return []string{"xdg-open"} // linux and the rest
}

// openURL opens url in the default browser. The opener detaches; we reap it in a
// goroutine so the short-lived child doesn't linger as a zombie in this
// long-running TUI.
func openURL(url string) error {
	argv := append(browserArgv(runtime.GOOS, os.Getenv("BROWSER")), url)
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the child; its exit status is irrelevant
	return nil
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `go test ./internal/ui -run TestBrowserArgv -v`
Expected: PASS, all 4 subtests

- [ ] **Step 5: Run the full suite and vet**

Run: `go test ./...`
Expected: PASS

Run: `go vet ./...`
Expected: no output

- [ ] **Step 6: Commit**

```bash
git add internal/ui/browser.go internal/ui/browser_test.go
git commit -m "feat(ui): open URLs through \$BROWSER when set"
```

### Task 2: Document the behavior

**Files:**
- Modify: `README.md`, in the section describing keys/actions. Find it with `rg -n "open" README.md`. If there is no natural spot, add a short "Opening URLs" subsection after the install section.

- [ ] **Step 1: Add the note**

```markdown
### Opening URLs

`o` opens the PR, issue, or check in your browser. When `$BROWSER` is set,
prdash runs `$BROWSER <url>` instead of `open`/`xdg-open`. Inside a lazytmux
mirrored session, `BROWSER=og-open` routes the open to the controlling host.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: note that URL opens honor \$BROWSER"
```

### Manual verification (after the lazytmux half lands)

- [ ] From a mirrored session: `prefix + p`, move to a PR, press `o`. The controller's browser opens the PR.
- [ ] Locally with `BROWSER` unset: `o` opens the browser as before.
- [ ] Locally, run `BROWSER=echo prdash` and press `o`. It shows "Opened" and no browser appears, which confirms `$BROWSER` is used.
