package jira

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRelative(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"just now - 0s", now, "just now"},
		{"just now - 30s", now.Add(-30 * time.Second), "just now"},
		{"just now - 59s", now.Add(-59 * time.Second), "just now"},
		{"minutes - 1m", now.Add(-1 * time.Minute), "1m"},
		{"minutes - 5m", now.Add(-5 * time.Minute), "5m"},
		{"minutes - 59m", now.Add(-59 * time.Minute), "59m"},
		{"hours - 1h", now.Add(-1 * time.Hour), "1h"},
		{"hours - 3h", now.Add(-3 * time.Hour), "3h"},
		{"hours - 23h", now.Add(-23 * time.Hour), "23h"},
		{"days - 1d", now.Add(-24 * time.Hour), "1d"},
		{"days - 2d", now.Add(-2 * 24 * time.Hour), "2d"},
		{"days - 6d", now.Add(-6 * 24 * time.Hour), "6d"},
		{"weeks - 1w", now.Add(-7 * 24 * time.Hour), "1w"},
		{"weeks - 3w", now.Add(-21 * 24 * time.Hour), "3w"},
		{"months - 1mo", now.Add(-30 * 24 * time.Hour), "1mo"},
		{"months - 6mo", now.Add(-180 * 24 * time.Hour), "6mo"},
		{"months - 11mo", now.Add(-330 * 24 * time.Hour), "11mo"},
		{"years - 1y", now.Add(-365 * 24 * time.Hour), "1y"},
		{"years - 2y", now.Add(-730 * 24 * time.Hour), "2y"},
		// future time is treated symmetrically
		{"future - 5m", now.Add(5 * time.Minute), "5m"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatRelative(tc.t, now)
			if got != tc.want {
				t.Errorf("FormatRelative(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

func TestWrapAt(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		width  int
		indent int
		want   string
	}{
		{
			name:   "empty string",
			s:      "",
			width:  80,
			indent: 2,
			want:   "",
		},
		{
			name:   "single word fits",
			s:      "hello",
			width:  80,
			indent: 2,
			want:   "hello",
		},
		{
			name:   "all fits on one line",
			s:      "hello world",
			width:  80,
			indent: 2,
			want:   "hello world",
		},
		{
			name:   "wraps at width",
			s:      "one two three four five",
			width:  10,
			indent: 0,
			want:   "one two\nthree four\nfive",
		},
		{
			name:   "continuation lines indented",
			s:      "one two three four five",
			width:  10,
			indent: 2,
			want:   "one two\n  three\n  four\n  five",
		},
		{
			name:   "word longer than width kept whole",
			s:      "superlongwordthatexceedswidth next",
			width:  10,
			indent: 2,
			want:   "superlongwordthatexceedswidth\n  next",
		},
		{
			name:   "multiline wrap with indent 4",
			s:      "The quick brown fox jumps over the lazy dog",
			width:  20,
			indent: 4,
			want:   "The quick brown fox\n    jumps over the\n    lazy dog",
		},
		{
			name:   "extra whitespace collapsed",
			s:      "  lots   of   spaces  ",
			width:  80,
			indent: 2,
			want:   "lots of spaces",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WrapAt(tc.s, tc.width, tc.indent)
			if got != tc.want {
				// Show a diff-friendly view
				t.Errorf("WrapAt(%q, %d, %d):\ngot:\n%s\nwant:\n%s",
					tc.s, tc.width, tc.indent,
					strings.ReplaceAll(got, "\n", "↵\n"),
					strings.ReplaceAll(tc.want, "\n", "↵\n"),
				)
			}
		})
	}
}

func TestAbbreviateChange(t *testing.T) {
	longBody := strings.Repeat("x", 500) // 500 runes
	longComment := strings.Repeat("y", 200) // 200 runes — above 120 truncation limit

	tests := []struct {
		name         string
		field        string
		from         string
		to           string
		statusMarker string
		want         string
	}{
		// description — always hidden
		{
			name:  "description updated",
			field: "description", from: "old text", to: "new text",
			want: "description: updated",
		},
		{
			name:  "description short update still hidden",
			field: "description", from: "a", to: "b",
			want: "description: updated",
		},
		{
			name:  "description set (from empty)",
			field: "description", from: "", to: longBody,
			want: "description: set",
		},
		{
			name:  "description cleared (to empty)",
			field: "description", from: longBody, to: "",
			want: "description: cleared",
		},
		// comment — truncated at 120
		{
			name:  "comment short — shown in full",
			field: "Comment", from: "short old", to: "short new",
			want: "Comment: short old → short new",
		},
		{
			name:  "comment long — truncated at 120",
			field: "Comment", from: longComment, to: longComment,
			want: "Comment: " + strings.Repeat("y", 120) + "… → " + strings.Repeat("y", 120) + "…",
		},
		// environment truncated
		{
			name:  "environment long — truncated at 120",
			field: "environment", from: longComment, to: "prod",
			want: "environment: " + strings.Repeat("y", 120) + "… → prod",
		},
		// summary truncated
		{
			name:  "summary long — truncated at 120",
			field: "summary", from: longComment, to: longComment,
			want: "summary: " + strings.Repeat("y", 120) + "… → " + strings.Repeat("y", 120) + "…",
		},
		// status — shown in full with regression marker
		{
			name:         "status with regression marker",
			field:        "status", from: "In Progress", to: "To Do", statusMarker: " ↩",
			want:         "status: In Progress → To Do ↩",
		},
		// assignee — shown in full even if long
		{
			name:  "assignee full even if long",
			field: "assignee", from: longBody, to: longBody,
			want:  "assignee: " + longBody + " → " + longBody,
		},
		// normal short field
		{
			name:  "fixVersion shown in full",
			field: "Fix Version", from: "2026.05", to: "2026.06",
			want:  "Fix Version: 2026.05 → 2026.06",
		},
		// from-empty normal field
		{
			name:  "label set (from empty)",
			field: "labels", from: "", to: "mobile",
			want:  "labels: → mobile",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AbbreviateChange(tc.field, tc.from, tc.to, tc.statusMarker)
			if got != tc.want {
				t.Errorf("\ngot:  %q\nwant: %q", got, tc.want)
			}
		})
	}
}
