package ui

import (
	"os/exec"
	"runtime"
)

// browserArgv is the OS command that opens a URL. Split out (like clipboardArgv)
// so the choice is unit-testable without spawning anything.
func browserArgv(goos string) []string {
	if goos == "darwin" {
		return []string{"open"}
	}
	return []string{"xdg-open"} // linux and the rest
}

// openURL opens url in the default browser. The opener detaches; we reap it in a
// goroutine so the short-lived child doesn't linger as a zombie in this
// long-running TUI.
func openURL(url string) error {
	argv := append(browserArgv(runtime.GOOS), url)
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the child; its exit status is irrelevant
	return nil
}
