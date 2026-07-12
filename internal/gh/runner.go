package gh

import (
	"errors"
	"os/exec"
	"strings"
)

// Runner runs `gh` with args in dir and returns stdout. The single OS seam,
// so the rest of the package is unit-testable with a fake.
type Runner interface {
	Run(dir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	// cmd.Output surfaces a bare "exit status 1"; gh's real diagnostic lives on
	// stderr, so fold it into the error message.
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return out, errors.New(msg)
		}
	}
	return out, err
}
