package jira

import "testing"

func TestDefaultOpenFilter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input yields just the filter",
			input: "",
			want:  `statusCategory != "Done"`,
		},
		{
			name:  "whitespace-only input yields just the filter",
			input: "   ",
			want:  `statusCategory != "Done"`,
		},
		{
			name:  "plain JQL is wrapped",
			input: `project = ACME`,
			want:  `(project = ACME) AND statusCategory != "Done"`,
		},
		{
			name:  "JQL with status = is unchanged",
			input: `project = ACME AND status = "In Progress"`,
			want:  `project = ACME AND status = "In Progress"`,
		},
		{
			name:  "JQL with status != is unchanged",
			input: `project = ACME AND status != Done`,
			want:  `project = ACME AND status != Done`,
		},
		{
			name:  "JQL with status in is unchanged",
			input: `project = ACME AND status in ("Open", "In Progress")`,
			want:  `project = ACME AND status in ("Open", "In Progress")`,
		},
		{
			name:  "JQL with statusCategory != is unchanged",
			input: `project = ACME AND statusCategory != "Done"`,
			want:  `project = ACME AND statusCategory != "Done"`,
		},
		{
			name:  "JQL with statusCategory = is unchanged",
			input: `statusCategory = "To Do"`,
			want:  `statusCategory = "To Do"`,
		},
		{
			name:  "JQL with resolution is EMPTY is unchanged",
			input: `project = ACME AND resolution is EMPTY`,
			want:  `project = ACME AND resolution is EMPTY`,
		},
		{
			name:  "JQL with resolution = is unchanged",
			input: `resolution = Unresolved`,
			want:  `resolution = Unresolved`,
		},
		{
			name:  "JQL with resolution in is unchanged",
			input: `project = X AND resolution in (Unresolved, Fixed)`,
			want:  `project = X AND resolution in (Unresolved, Fixed)`,
		},
		{
			name:  "resolution not in is unchanged",
			input: `resolution not in (Unresolved)`,
			want:  `resolution not in (Unresolved)`,
		},
		{
			name:  "case-insensitive STATUS is unchanged",
			input: `STATUS = "In Progress"`,
			want:  `STATUS = "In Progress"`,
		},
		{
			name:  "case-insensitive STATUSCATEGORY is unchanged",
			input: `STATUSCATEGORY != "Done"`,
			want:  `STATUSCATEGORY != "Done"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultOpenFilter(tt.input)
			if got != tt.want {
				t.Errorf("DefaultOpenFilter(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}
