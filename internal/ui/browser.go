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
	return spawnDetached(append(browserArgv(runtime.GOOS, os.Getenv("BROWSER")), url))
}
