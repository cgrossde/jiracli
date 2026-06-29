package jira

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color helpers — no external dependency.
// Badge format: bold white text on a colored background, padded with spaces.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiFgW   = "\x1b[97m" // bright white foreground

	// Background colors (256-color) — muted to avoid overwhelming dark terminals.
	ansiBgToDo       = "\x1b[48;5;238m" // dark grey        — "To Do / Open / Ready"
	ansiBgInProgress = "\x1b[48;5;24m"  // steel blue       — "In Progress / Pending …"
	ansiBgDone       = "\x1b[48;5;22m"  // dark green       — "Done"

	ansiBgBug   = "\x1b[48;5;88m" // dark red
	ansiBgVuln  = "\x1b[48;5;88m" // dark red (same as bug)
	ansiBgStory = "\x1b[48;5;28m" // dark green
	ansiBgEpic  = "\x1b[48;5;55m" // dark purple
	ansiBgTask  = "\x1b[48;5;24m" // steel blue (same as in-progress)

	// Foreground-only for priority (no badge).
	ansiFgOrange = "\x1b[38;5;208m"
	ansiFgRed    = "\x1b[31m"
	ansiFgGrey   = "\x1b[38;5;246m"
)

// ColorsEnabled reports whether ANSI colour sequences should be emitted.
// Colour is disabled when:
//   - the NO_COLOR env var is set (https://no-color.org), or
//   - stdout is not a terminal (pipe, redirect, CI, etc.).
func ColorsEnabled() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StripAnsi removes ANSI escape sequences and returns the visible string.
// Uses a state machine — no alloc for strings with no escapes.
func StripAnsi(s string) string {
	out := make([]byte, 0, len(s))
	inEsc := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if inEsc {
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				inEsc = false
			}
			continue
		}
		if b == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i++ // skip '['
			continue
		}
		out = append(out, b)
	}
	return string(out)
}

// badge wraps text in a bold-white-on-bg pill: " text ".
func badge(bg, text string) string {
	return bg + ansiFgW + ansiBold + " " + text + " " + ansiReset
}

// ColorIssueType returns a colored type badge (or plain "[Type]") for an issue type name.
func ColorIssueType(name string, colorEnabled bool) string {
	if name == "" {
		return ""
	}
	if !colorEnabled {
		return "[" + name + "]"
	}
	letter := string([]rune(name)[0])
	switch strings.ToLower(name) {
	case "bug":
		return badge(ansiBgBug, letter)
	case "vulnerability":
		return badge(ansiBgVuln, letter)
	case "story":
		return badge(ansiBgStory, letter)
	case "epic":
		return badge(ansiBgEpic, letter)
	case "task", "sub-task", "subtask":
		return badge(ansiBgTask, letter)
	}
	return letter
}

// ColorStatusName returns a colored status badge (or plain name) for a status display name.
// name is the human-readable status ("In Progress", "Done", "To Do", etc.).
func ColorStatusName(name string, colorEnabled bool) string {
	var key string
	switch strings.ToLower(name) {
	case "done":
		key = "done"
	case "in progress":
		key = "indeterminate"
	default:
		key = "new"
	}
	if !colorEnabled {
		return name
	}
	switch key {
	case "indeterminate":
		return badge(ansiBgInProgress, name)
	case "done":
		return badge(ansiBgDone, name)
	default:
		return badge(ansiBgToDo, name)
	}
}

// Bold returns s wrapped in ANSI bold when on is true; s unchanged otherwise.
func Bold(s string, on bool) string {
	if !on {
		return s
	}
	return ansiBold + s + ansiReset
}

// BoldFgW returns s wrapped in ANSI bold+white when on is true.
func BoldFgW(s string, on bool) string {
	if !on {
		return s
	}
	return ansiBold + ansiFgW + s + ansiReset
}

// Dim returns s in dim/grey color when on is true; s unchanged otherwise.
func Dim(s string, on bool) string {
	if !on {
		return s
	}
	return ansiFgGrey + s + ansiReset
}
