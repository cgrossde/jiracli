package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupported is returned when the OS has no known URL-open command.
var ErrUnsupported = errors.New("cannot auto-open browser on this OS")

const browserOverrideEnv = "JIRACLI_BROWSER"

// Open launches the OS-default URL handler for the given URL.
// macOS uses "open", Linux uses "xdg-open", and Windows uses rundll32.
// Set JIRACLI_BROWSER to an executable, command name, or known browser alias
// (for example, "microsoft-edge") to launch a specific browser instead.
// Non-blocking: the child process is detached (Start, not Run).
func Open(rawURL string) error {
	cmd, args, err := browserCommand(runtime.GOOS, os.Getenv(browserOverrideEnv), rawURL)
	if err != nil {
		return err
	}
	return exec.Command(cmd, args...).Start()
}

func browserCommand(goos, override, rawURL string) (string, []string, error) {
	if override = strings.TrimSpace(override); override != "" {
		if goos == "darwin" {
			if app, ok := macOSBrowserApp(override); ok {
				return "open", []string{"-a", app, rawURL}, nil
			}
		}
		if alias, ok := browserAlias(goos, override); ok {
			return alias, []string{rawURL}, nil
		}
		if goos == "darwin" && isMacOSAppName(override) {
			return "open", []string{"-a", override, rawURL}, nil
		}
		return override, []string{rawURL}, nil
	}

	switch goos {
	case "darwin":
		return "open", []string{rawURL}, nil
	case "linux":
		return "xdg-open", []string{rawURL}, nil
	case "windows":
		return "rundll32.exe", []string{"url.dll,FileProtocolHandler", rawURL}, nil
	default:
		return "", nil, ErrUnsupported
	}
}

func browserAlias(goos, value string) (string, bool) {
	switch goos {
	case "windows":
		switch strings.ToLower(value) {
		case "edge", "microsoft-edge", "msedge":
			return "msedge.exe", true
		case "chrome", "google-chrome":
			return "chrome.exe", true
		case "chromium":
			return "chromium.exe", true
		case "brave", "brave-browser":
			return "brave.exe", true
		case "vivaldi":
			return "vivaldi.exe", true
		case "opera":
			return "opera.exe", true
		}
	case "linux":
		switch strings.ToLower(value) {
		case "edge", "microsoft-edge":
			return "microsoft-edge", true
		case "chrome", "google-chrome":
			return "google-chrome", true
		case "brave", "brave-browser":
			return "brave-browser", true
		}
	}
	return "", false
}

func macOSBrowserApp(value string) (string, bool) {
	switch strings.ToLower(value) {
	case "edge", "microsoft-edge", "msedge":
		return "Microsoft Edge", true
	case "chrome", "google-chrome":
		return "Google Chrome", true
	case "chromium":
		return "Chromium", true
	case "brave", "brave-browser":
		return "Brave Browser", true
	case "vivaldi":
		return "Vivaldi", true
	case "opera":
		return "Opera", true
	}
	if isMacOSAppName(value) {
		return value, true
	}
	return "", false
}

func isMacOSAppName(value string) bool {
	return strings.HasSuffix(strings.ToLower(value), ".app") ||
		(!strings.ContainsAny(value, `/\`) && strings.Contains(value, " "))
}
