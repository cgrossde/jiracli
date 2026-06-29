package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cgrossde/jiracli/internal/jira"
)

// defaultSearchFieldsCopy returns a fresh copy for slice-equality comparisons.
func defaultSearchFieldsCopy() []string {
	out := make([]string, len(defaultSearchFields))
	copy(out, defaultSearchFields)
	return out
}

// ── Test 1: resolveSearchFields add/drop semantics ──────────────────────────

func TestResolveSearchFields_AddDrop(t *testing.T) {
	defaults := defaultSearchFieldsCopy()
	defaultsStr := strings.Join(defaults, ",")
	_ = defaultsStr

	type tc struct {
		spec string
		want func([]string) bool // predicate over result
		desc string
	}

	tests := []tc{
		{
			spec: "",
			want: func(got []string) bool { return sliceEqual(got, defaults) },
			desc: "empty spec → defaults verbatim",
		},
		{
			spec: "description",
			want: func(got []string) bool {
				return sliceContains(got, "description") && sliceHasPrefix(got, defaults)
			},
			desc: "bare name adds to defaults",
		},
		{
			spec: "+description",
			want: func(got []string) bool {
				return sliceContains(got, "description") && sliceHasPrefix(got, defaults)
			},
			desc: "+ prefix is alias for bare (backward compat)",
		},
		{
			spec: "description,reporter,fixVersions",
			want: func(got []string) bool {
				return sliceContains(got, "description") &&
					sliceContains(got, "reporter") &&
					sliceContains(got, "fixVersions") &&
					sliceHasPrefix(got, defaults) &&
					!hasDuplicate(got)
			},
			desc: "multi-field add — all three appended, no duplicates",
		},
		{
			spec: "description,-labels",
			want: func(got []string) bool {
				return sliceContains(got, "description") && !sliceContains(got, "labels")
			},
			desc: "add description, drop labels",
		},
		{
			spec: "-labels,-components",
			want: func(got []string) bool {
				return !sliceContains(got, "labels") && !sliceContains(got, "components")
			},
			desc: "drop two defaults",
		},
		{
			spec: "summary",
			want: func(got []string) bool {
				return sliceEqual(got, defaults) // summary already present — idempotent
			},
			desc: "add already-present field is a no-op",
		},
		{
			spec: "description,-description",
			want: func(got []string) bool {
				return sliceEqual(got, defaults) // added then dropped = net zero
			},
			desc: "add then drop cancels out (left-to-right)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := resolveSearchFields(tt.spec)
			if !tt.want(got) {
				t.Errorf("resolveSearchFields(%q) = %v; predicate failed", tt.spec, got)
			}
		})
	}
}

// ── Test 2: resolveFieldList add/drop semantics ──────────────────────────────

func TestResolveFieldList_AddDrop(t *testing.T) {
	defaults := jira.DefaultIssueFields

	tests := []struct {
		spec string
		want func(string) bool
		desc string
	}{
		{
			spec: "",
			want: func(got string) bool { return got == defaults },
			desc: "empty spec → DefaultIssueFields unchanged",
		},
		{
			spec: "description",
			want: func(got string) bool {
				// description is already in DefaultIssueFields — idempotent, no duplicate
				parts := strings.Split(got, ",")
				count := 0
				for _, p := range parts {
					if strings.TrimSpace(p) == "description" {
						count++
					}
				}
				return count == 1
			},
			desc: "description already in defaults — no duplicate",
		},
		{
			spec: "customfield_10031",
			want: func(got string) bool {
				parts := strings.Split(got, ",")
				return fieldListContains(parts, "customfield_10031") &&
					strings.HasSuffix(strings.TrimRight(got, ","), "customfield_10031")
			},
			desc: "custom field appended at end",
		},
		{
			spec: "-priority,-assignee",
			want: func(got string) bool {
				parts := strings.Split(got, ",")
				return !fieldListContains(parts, "priority") && !fieldListContains(parts, "assignee")
			},
			desc: "drop two defaults",
		},
		{
			spec: "+timetracking",
			want: func(got string) bool {
				parts := strings.Split(got, ",")
				return fieldListContains(parts, "timetracking")
			},
			desc: "+ prefix alias appends field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := resolveFieldList(defaults, tt.spec)
			if !tt.want(got) {
				t.Errorf("resolveFieldList(%q) = %q; predicate failed", tt.spec, got)
			}
		})
	}
}

// ── Test 3: descPreview ──────────────────────────────────────────────────────

func TestDescPreview(t *testing.T) {
	tests := []struct {
		input string
		want  string
		desc  string
	}{
		{"", "", "empty string → empty"},
		{"   \n  ", "", "only whitespace → empty"},
		{"first line\nsecond line", "first line second line", "newlines become spaces"},
		{"* bullet one\n* bullet two", "bullet one bullet two", "list bullets stripped"},
		{"# heading\n## sub", "heading sub", "hash headings stripped"},
		{"[Click here|https://x.test]", "Click here", "wiki link with text → text only"},
		{"[https://x.test]", "https://x.test", "bare wiki link → url"},
		{"{code}foo{code}", "foo", "code block markers removed"},
		{"{code:go}func main() { print(1) }{code}", "func main() { print(1) }", "code:lang block markers removed"},
		{"a\u00a0b", "a b", "non-breaking space normalised"},
		{
			"{panel:borderStyle=dashed|borderColor=ccc|bgColor=red}{color:white}*This is a Critical Risk vulnerability",
			"This is a Critical Risk vulnerability",
			"panel+color macros and bare bold marker stripped",
		},
		{"*bold text*", "bold text", "symmetric bold markers stripped"},
		{"_italic text_", "italic text", "symmetric italic markers stripped"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := descPreview(tt.input)
			if got != tt.want {
				t.Errorf("descPreview(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}

	// 200-rune input → exactly 100+1 rune result (100 runes + "…" appended).
	t.Run("200-rune input truncated to 100 runes then ellipsis", func(t *testing.T) {
		input := strings.Repeat("a", 200)
		got := descPreview(input)
		runes := []rune(got)
		if len(runes) != descPreviewLen+1 {
			t.Errorf("expected %d runes (incl. '…'), got %d: %q", descPreviewLen+1, len(runes), got)
		}
		if runes[len(runes)-1] != '…' {
			t.Errorf("expected last rune '…', got %q", string(runes[len(runes)-1]))
		}
	})
}

// ── Test 4: renderSearchPlain description line ───────────────────────────────

func TestRenderSearchPlain_DescriptionLine(t *testing.T) {
	withDesc := jira.IssueRaw{}
	withDesc.Key = "WEB-1"
	withDesc.Fields.Summary = "Issue with description"
	withDesc.Fields.Description = "This is the description text that should appear in the preview."
	withDesc.Fields.Status.Name = "Open"
	withDesc.Fields.Status.StatusCategory.Key = "new"
	withDesc.Fields.Status.StatusCategory.Name = "To Do"
	withDesc.Fields.IssueType.Name = "Bug"
	withDesc.Fields.Updated = time.Now().Add(-48 * time.Hour).Format("2006-01-02T15:04:05.000-0700")

	noDesc := jira.IssueRaw{}
	noDesc.Key = "WEB-2"
	noDesc.Fields.Summary = "Issue without description"
	noDesc.Fields.Status.Name = "Open"
	noDesc.Fields.Status.StatusCategory.Key = "new"
	noDesc.Fields.Status.StatusCategory.Name = "To Do"
	noDesc.Fields.IssueType.Name = "Story"
	noDesc.Fields.Updated = time.Now().Add(-5 * 24 * time.Hour).Format("2006-01-02T15:04:05.000-0700")

	resp := jira.SearchResponse{
		Issues: []jira.IssueRaw{withDesc, noDesc},
		Total:  2,
	}

	// Case 1: no description in fields — no preview lines.
	t.Run("no description field — no preview", func(t *testing.T) {
		fields := defaultSearchFieldsCopy()
		out, err := renderSearchPlain(resp, "key in (WEB-1, WEB-2)", "key in (WEB-1, WEB-2)", 1, 50, SearchFlags{}, fields)
		if err != nil {
			t.Fatal(err)
		}
		// Count lines with 4-space indent containing description text.
		if strings.Contains(out, "This is the description") {
			t.Error("description text should not appear when description not in fields")
		}
	})

	// Case 2: description in fields — preview on issue 1, not issue 2 (empty desc).
	t.Run("description in fields — preview on issue with description only", func(t *testing.T) {
		fields := append(defaultSearchFieldsCopy(), "description")
		out, err := renderSearchPlain(resp, "key in (WEB-1, WEB-2)", "key in (WEB-1, WEB-2)", 1, 50, SearchFlags{}, fields)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "This is the description text") {
			t.Error("description preview should appear for WEB-1")
		}
		// WEB-2 has no description — only one preview.
		count := strings.Count(out, "This is")
		if count != 1 {
			t.Errorf("expected exactly 1 description preview, got %d", count)
		}
	})

	// Case 3: description,reporter,fixVersions added — plain text only shows description.
	t.Run("multi-add: only description renders in plain output", func(t *testing.T) {
		fields := append(defaultSearchFieldsCopy(), "description", "reporter", "fixVersions")
		out, err := renderSearchPlain(resp, "key in (WEB-1, WEB-2)", "key in (WEB-1, WEB-2)", 1, 50, SearchFlags{}, fields)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "reporter:") || strings.Contains(out, "Reporter:") {
			t.Error("reporter should not be rendered in plain-text output")
		}
		if strings.Contains(out, "fixVersions") {
			t.Error("fixVersions should not be rendered in plain-text output")
		}
		if !strings.Contains(out, "This is the description text") {
			t.Error("description preview should still appear")
		}
	})
}

// ── Test 5: --fields and --fields-only mutex ─────────────────────────────────

func TestSearch_FieldsAndFieldsOnlyMutex(t *testing.T) {
	flags := SearchFlags{
		Fields:     "description",
		FieldsOnly: "key,summary",
	}
	_, err := Search(context.Background(), flags, "project = TEST")
	if err == nil {
		t.Fatal("expected error for --fields + --fields-only, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// sliceHasPrefix returns true when all elements of prefix appear in ss in the
// same order (not necessarily contiguous), maintaining their relative order.
func sliceHasPrefix(ss, prefix []string) bool {
	pi := 0
	for _, v := range ss {
		if pi < len(prefix) && v == prefix[pi] {
			pi++
		}
	}
	return pi == len(prefix)
}

func hasDuplicate(ss []string) bool {
	seen := make(map[string]bool, len(ss))
	for _, v := range ss {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

func fieldListContains(parts []string, field string) bool {
	for _, p := range parts {
		if strings.TrimSpace(p) == field {
			return true
		}
	}
	return false
}
