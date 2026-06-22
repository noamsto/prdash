package gh

import "os/exec"

// Runner runs `gh` with args in dir and returns stdout. The single OS seam,
// so the rest of the package is unit-testable with a fake.
type Runner interface {
	Run(dir string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	return cmd.Output()
}
