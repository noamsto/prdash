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

// openURL opens url in the default browser. Fire-and-forget: the opener detaches.
func openURL(url string) error {
	argv := append(browserArgv(runtime.GOOS), url)
	return exec.Command(argv[0], argv[1:]...).Start()
}
