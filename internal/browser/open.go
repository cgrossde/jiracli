package browser

import (
	"errors"
	"os/exec"
	"runtime"
)

// ErrUnsupported is returned when the OS has no known URL-open command.
var ErrUnsupported = errors.New("cannot auto-open browser on this OS")

// Open launches the OS-default URL handler for the given URL.
// macOS uses "open", Linux uses "xdg-open".
// Non-blocking: the child process is detached (Start, not Run).
func Open(rawURL string) error {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		return ErrUnsupported
	}
	return exec.Command(cmd, rawURL).Start()
}
