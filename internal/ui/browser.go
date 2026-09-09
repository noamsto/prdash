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

// spawnDetached starts argv and returns without waiting. The child is reaped in
// a goroutine so a short-lived opener doesn't linger as a zombie in this
// long-running TUI, and so the UI never blocks on process startup.
func spawnDetached(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// openURL opens url in the default browser.
func openURL(url string) error {
	return spawnDetached(append(browserArgv(runtime.GOOS), url))
}
