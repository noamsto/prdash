package gh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// execTimeout bounds the read-only gh/git subprocesses this package shells
// out to (gh auth token, git remote get-url, and the local read-only git
// probes), so a stalled subprocess (locked keyring, stale index lock, slow
// filesystem) fails instead of hanging forever. A var, not a const, so tests
// can shorten it.
var execTimeout = 3 * time.Second

// mutatingGitTimeout bounds the two git calls that mutate the filesystem/refs
// (worktree remove, branch -D). They get more slack than execTimeout because
// killing them mid-mutation can leave a half-removed worktree or a locked
// ref — the bound exists only to stop a stale-lock hang, not to be tight.
var mutatingGitTimeout = 10 * time.Second

// waitGrace bounds how long Wait keeps waiting for a subprocess's output pipe
// to close after the process itself is already gone. cmd.WaitDelay's own
// timer starts at the context deadline (or process exit), not after the kill
// signal fires, so this is additive on top of whichever timeout the caller's
// context carries — keep it small and fixed rather than reusing the full
// timeout, or the effective worst case doubles.
const waitGrace = 500 * time.Millisecond

// newBoundedCmd builds cmd for name/arg under ctx, so it is killed if ctx's
// deadline passes, and sets WaitDelay so Wait() returns within waitGrace of
// that even if a grandchild process inherits the output pipe and outlives the
// kill (e.g. a POSIX sh script that forks instead of exec'ing its payload) —
// otherwise Output()/Run()/CombinedOutput() can block on that pipe long after
// the kill.
func newBoundedCmd(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.WaitDelay = waitGrace
	return cmd
}

// Token returns a GitHub token for api.github.com. It prefers GH_TOKEN /
// GITHUB_TOKEN from the environment — set either (e.g. via your secrets manager)
// for a fully gh-free setup — and otherwise falls back to `gh auth token`, which
// resolves gh's stored token portably on macOS and Linux (keyring or file)
// without prdash having to know where gh keeps it. Times out rather than
// hanging if `gh auth token` stalls.
func Token() (string, error) {
	for _, env := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if t := strings.TrimSpace(os.Getenv(env)); t != "" {
			return t, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	out, err := newBoundedCmd(ctx, "gh", "auth", "token").Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("`gh auth token` timed out after %s (gh may be stuck talking to your keyring/secret service): %w", execTimeout, ctx.Err())
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("no GH_TOKEN/GITHUB_TOKEN set and `gh auth token` failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("no GH_TOKEN/GITHUB_TOKEN set and `gh auth token` failed: %w", err)
	}
	t := strings.TrimSpace(string(out))
	if t == "" {
		return "", errors.New("no GH_TOKEN/GITHUB_TOKEN set and `gh auth token` returned empty")
	}
	return t, nil
}
