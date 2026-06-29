package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// authPrompt prints label to stderr and reads a line from stdin.
func authPrompt(label string) string {
	fmt.Fprint(os.Stderr, label)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// authPromptSecret prints label to stderr and reads input with echo disabled.
// Falls back to plain readline when stdin is not a terminal (e.g. pipe).
func authPromptSecret(label string) string {
	fmt.Fprint(os.Stderr, label)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return ""
		}
		return string(b)
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// authPromptYesNo defaults to N on empty input.
func authPromptYesNo(label string) bool {
	return authPromptYesNoDefault(label, false)
}

// authPromptYesNoDefault is authPromptYesNo with configurable default.
func authPromptYesNoDefault(label string, defaultYes bool) bool {
	fmt.Fprint(os.Stderr, label)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			return defaultYes
		}
		return strings.EqualFold(text, "y") || strings.EqualFold(text, "yes")
	}
	return defaultYes
}

// promptApply reads a y/n decision from /dev/tty so it survives stdin redirection.
// Returns (decision, ttyAvailable). If /dev/tty cannot be opened (CI/container),
// returns (false, false) — caller treats as non-interactive.
func promptApply() (apply bool, ttyAvailable bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, false
	}
	defer tty.Close()
	fmt.Fprint(tty, "Apply? [y/N]: ")
	scanner := bufio.NewScanner(tty)
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(text, "y") || strings.EqualFold(text, "yes"), true
	}
	return false, true
}
