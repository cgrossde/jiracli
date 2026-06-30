package jira

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// FormatRelative returns a compact relative time string: "just now", "5m", "3h", "2d", "3w", "6mo", "2y".
// now is injectable for tests.
func FormatRelative(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(7*24)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(math.Round(d.Hours()/(30*24))))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(365*24)))
	}
}

// WrapAt word-wraps s at width columns with indent spaces of leading indent on continuation lines.
// First line is NOT indented (caller handles that).
func WrapAt(s string, width, indent int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) <= width {
			line += " " + w
		} else {
			lines = append(lines, line)
			line = strings.Repeat(" ", indent) + w
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

// StatusCategoryRank returns a rank for ordering status categories.
// "To Do" < "In Progress" < "Done"
func StatusCategoryRank(category string) int {
	switch strings.ToLower(category) {
	case "to do", "todo":
		return 0
	case "in progress":
		return 1
	case "done":
		return 2
	default:
		return -1
	}
}

// TruncateString truncates s to maxRunes runes, appending "…" if truncated.
func TruncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

// CommonRunePrefix returns the length in runes of the longest common prefix
// shared by all strings in strs. Empty strings are ignored; returns 0 when
// strs is empty or contains only empty strings.
func CommonRunePrefix(strs []string) int {
	var base []rune
	for _, s := range strs {
		if s == "" {
			continue
		}
		r := []rune(s)
		if base == nil {
			base = r
			continue
		}
		n := len(r)
		if len(base) < n {
			n = len(base)
		}
		i := 0
		for i < n && base[i] == r[i] {
			i++
		}
		base = base[:i]
	}
	return len(base)
}

// TruncateMidPrefix truncates s to width runes using middle-elision.
// The first prefixKeep runes are always shown, then "…", then as much of the
// tail of s as fits in the remaining width. This surfaces the unique suffix
// of strings that all share a long common prefix.
//
// Falls back to TruncateString (right-truncate) when:
//   - s fits in width already
//   - prefixKeep ≥ width-1 (no room for any tail)
//   - the string is too short to have a meaningful tail
func TruncateMidPrefix(s string, width, prefixKeep int) string {
	r := []rune(s)
	if len(r) <= width {
		return s // fits, no elision needed
	}
	// tailWidth = width - prefixKeep - 1 (for the "…")
	tailWidth := width - prefixKeep - 1
	if tailWidth <= 0 {
		// No room for a tail; fall back to right-truncate.
		return TruncateString(s, width)
	}
	tail := string(r[len(r)-tailWidth:])
	return string(r[:prefixKeep]) + "…" + tail
}

// FormatBytes renders a byte count as a human-readable string: "142 KB", "9 KB", "1.2 MB".
func FormatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", n/1024)
	case n < 1024*1024*1024:
		mb := float64(n) / (1024 * 1024)
		if mb < 10 {
			return fmt.Sprintf("%.1f MB", mb)
		}
		return fmt.Sprintf("%d MB", int(mb))
	default:
		gb := float64(n) / (1024 * 1024 * 1024)
		return fmt.Sprintf("%.1f GB", gb)
	}
}

// ColWidth returns the display width of s (counts runes, not bytes).
func ColWidth(s string) int {
	return len([]rune(s))
}

// PadRight pads s with spaces to at least width runes.
func PadRight(s string, width int) string {
	n := ColWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// IsASCIILetter reports whether r is an ASCII letter.
func IsASCIILetter(r rune) bool {
	return unicode.IsLetter(r) && r < 128
}

// wikiLinkRe matches [text|url] and bare [url] forms.
var wikiLinkRe = regexp.MustCompile(`\[([^\]|]+)\|([^\]]+)\]|\[([^\]]+)\]`)

// RenderWikiMarkup converts Jira wiki markup to clean plain text suitable for
// terminal display. Handles the subset of wiki syntax commonly seen in DC descriptions:
//   - * / ** / *** bullet lists → "• " / "  ◦ " / "    ▪ " prefixes
//   - # / ## numbered lists → "1. " / "  a. "
//   - [text|url] / [url] links → "text (url)" / "url"
//   - {noformat}...{noformat} / {code}...{code} → preserved verbatim block
//   - \r\n and \xa0 normalised
//   - Consecutive blank lines collapsed to one
func RenderWikiMarkup(s string) string {
	// Normalise line endings and non-breaking spaces.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\u00a0", " ")

	lines := strings.Split(s, "\n")
	var out []string
	inBlock := false
	numberedCounters := map[int]int{} // depth → current count

	for _, line := range lines {
		// Handle {noformat} / {code} blocks verbatim.
		trimmed := strings.TrimSpace(line)
		if !inBlock && (strings.HasPrefix(trimmed, "{noformat") || strings.HasPrefix(trimmed, "{code")) {
			inBlock = true
			out = append(out, "")
			continue
		}
		if inBlock {
			if strings.HasPrefix(trimmed, "{noformat}") || strings.HasPrefix(trimmed, "{code}") {
				inBlock = false
				out = append(out, "")
			} else {
				out = append(out, "  "+trimmed)
			}
			continue
		}

		// Bullet lists: leading asterisks determine depth.
		if bulletDepth, t := countLeading(line, '*'); bulletDepth > 0 {
			body := strings.TrimSpace(t[bulletDepth:])
			body = renderInline(body)
			switch bulletDepth {
			case 1:
				out = append(out, "• "+body)
			case 2:
				out = append(out, "  ◦ "+body)
			default:
				out = append(out, strings.Repeat("  ", bulletDepth-1)+"▪ "+body)
			}
			numberedCounters = map[int]int{}
			continue
		}

		// Numbered lists: leading hashes determine depth.
		if numDepth, t := countLeading(line, '#'); numDepth > 0 {
			numberedCounters[numDepth]++
			for d := numDepth + 1; d <= 10; d++ {
				delete(numberedCounters, d)
			}
			body := strings.TrimSpace(t[numDepth:])
			body = renderInline(body)
			n := numberedCounters[numDepth]
			prefix := strings.Repeat("  ", numDepth-1)
			if numDepth == 1 {
				out = append(out, fmt.Sprintf("%s%d. %s", prefix, n, body))
			} else {
				letter := string(rune('a' + (n-1)%26))
				out = append(out, fmt.Sprintf("%s%s. %s", prefix, letter, body))
			}
			continue
		}

		numberedCounters = map[int]int{}
		out = append(out, renderInline(strings.TrimRight(strings.TrimLeft(line, " \t"), " \t")))
	}

	// Collapse runs of more than one blank line to a single blank.
	result := collapseBlankLines(out)
	return strings.TrimSpace(result)
}

// countLeading counts how many consecutive occurrences of ch appear at the
// start of the trimmed line followed by a space (wiki list syntax).
// Returns (depth, trimmedLine) so the caller can extract the body correctly.
func countLeading(line string, ch byte) (int, string) {
	t := strings.TrimLeft(line, " \t")
	i := 0
	for i < len(t) && t[i] == ch {
		i++
	}
	if i > 0 && i < len(t) && t[i] == ' ' {
		return i, t
	}
	return 0, t
}

// renderInline processes inline wiki markup within a single line:
// [text|url], [url], *bold*, _italic_ (stripped to plain text).
func renderInline(s string) string {
	// [text|url] → "text (url)"
	// [url] → url
	s = wikiLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikiLinkRe.FindStringSubmatch(m)
		if sub[1] != "" && sub[2] != "" {
			// [text|url] — only show URL if text and URL differ meaningfully.
			text := strings.TrimSpace(sub[1])
			url := strings.TrimSpace(sub[2])
			if text == url || strings.HasPrefix(text, "http") {
				return url
			}
			return text + " (" + url + ")"
		}
		// bare [url]
		return strings.TrimSpace(sub[3])
	})
	// Strip bold/italic markers (*text*, _text_, +text+).
	s = stripPaired(s, '*')
	s = stripPaired(s, '_')
	s = stripPaired(s, '+')
	return s
}

// stripPaired removes paired wiki inline markers like *bold* and _italic_.
// Only strips when the marker appears at a word boundary.
func stripPaired(s string, marker rune) string {
	m := string(marker)
	// Simple heuristic: replace " *text* " patterns.
	// We use a regex-free approach to avoid over-stripping e.g. "3*4".
	var b strings.Builder
	i := 0
	r := []rune(s)
	for i < len(r) {
		if r[i] == marker {
			// Look for closing marker.
			j := i + 1
			for j < len(r) && r[j] != marker {
				j++
			}
			if j < len(r) && j > i+1 {
				// Only strip if preceded/followed by space or start/end.
				before := i == 0 || r[i-1] == ' '
				after := j+1 >= len(r) || r[j+1] == ' ' || r[j+1] == ',' || r[j+1] == '.'
				if before && after {
					b.WriteString(string(r[i+1 : j]))
					i = j + 1
					continue
				}
			}
			b.WriteString(m)
			i++
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	_ = m // suppress unused warning
	return b.String()
}

// collapseBlankLines joins lines and collapses runs of >1 blank line.
func collapseBlankLines(lines []string) string {
	var b strings.Builder
	blanks := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blanks++
			if blanks <= 1 {
				b.WriteByte('\n')
			}
		} else {
			blanks = 0
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// activityTruncateLen is the max rune length for truncated field values in the
// activity timeline. Fields like comment, environment, summary are shown up to
// this length. Description is always suppressed entirely.
const activityTruncateLen = 120

// alwaysHideField lists fields whose body is never shown — only "updated" is emitted.
var alwaysHideField = map[string]bool{
	"description": true,
}

// truncateField lists fields truncated to activityTruncateLen rather than shown in full.
var truncateField = map[string]bool{
	"Comment":     true,
	"comment":     true,
	"environment": true,
	"summary":     true,
}

// AbbreviateChange renders a single field change for the activity timeline.
//
// Rules:
//   - description: always "description: updated" (body never shown)
//   - Comment/comment: Jira omits bodies in changelog; render as "added a comment"
//     when both sides are empty, otherwise truncated to 120 chars per side
//   - environment, summary: truncated to 120 chars per side
//   - everything else: shown in full
//
// statusMarker is appended verbatim (used for the " ↩" regression marker).
func AbbreviateChange(field, from, to string, statusMarker string) string {
	// Always-hide fields: just say "updated" / "set" / "cleared".
	if alwaysHideField[field] {
		switch {
		case from == "" && to != "":
			return field + ": set"
		case from != "" && to == "":
			return field + ": cleared"
		default:
			return field + ": updated"
		}
	}

	// Comment fields: Jira changelog never includes comment bodies, so from/to
	// are always empty. Render a human-readable summary instead of "(none) → (none)".
	if strings.EqualFold(field, "comment") {
		if from == "" && to == "" {
			return "Comment: added"
		}
		// Bodies present (hypothetical future case) — fall through to truncated render.
	}

	// Truncated fields: clip each side to activityTruncateLen.
	if truncateField[field] {
		from = TruncateString(from, activityTruncateLen)
		to = TruncateString(to, activityTruncateLen)
	}

	// Normal render.
	fromDisplay := from
	if fromDisplay == "" {
		fromDisplay = "(none)"
	}
	toDisplay := to
	if toDisplay == "" {
		toDisplay = "(none)"
	}
	if from == "" && to != "" {
		return fmt.Sprintf("%s: → %s%s", field, toDisplay, statusMarker)
	}
	return fmt.Sprintf("%s: %s → %s%s", field, fromDisplay, toDisplay, statusMarker)
}

// FormatSeconds converts a Jira time-in-seconds value to a human-readable string.
// Jira stores time in seconds; typical granularity is 1h = 3600s.
// Returns "" when secs <= 0.
func FormatSeconds(secs int64) string {
	if secs <= 0 {
		return ""
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// ProgressPercent returns floor(100*spent/planned). Returns 0 when planned <= 0.
func ProgressPercent(spent, planned int64) int {
	if planned <= 0 {
		return 0
	}
	return int(math.Floor(float64(spent) / float64(planned) * 100))
}

// FormatProgressBar renders a width-cell unicode bar showing spent/planned ratio.
// Over-budget coloring: green ≤99%, orange 100-119%, red ≥120%.
// Returns ANSI-colored string when colorEnabled is true, plain otherwise.
// Planned must be > 0; caller guards.
func FormatProgressBar(spent, planned int64, width int, colorEnabled bool) string {
	if width <= 0 {
		width = 24
	}
	pct := ProgressPercent(spent, planned)

	// Cap filled cells at width even when over-budget.
	filled := int(math.Round(float64(pct) / 100.0 * float64(width)))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	var color string
	if colorEnabled {
		switch {
		case pct >= 120:
			color = "\x1b[31m" // red
		case pct >= 100:
			color = "\x1b[38;5;208m" // orange
		default:
			color = "\x1b[97m" // bright white
		}
	}

	suffix := fmt.Sprintf("· %d%% spent", pct)
	if colorEnabled {
		return color + "[" + bar + "]" + "\x1b[0m" + " " + suffix
	}
	return "[" + bar + "] " + suffix
}
