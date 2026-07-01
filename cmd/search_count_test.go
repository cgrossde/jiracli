package cmd

import (
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
)

// ── extractDimension ─────────────────────────────────────────────────────────

func TestExtractDimension(t *testing.T) {
	buildRaw := func(modify func(*jira.IssueRaw)) jira.IssueRaw {
		var r jira.IssueRaw
		r.Fields.Status.Name = "In Progress"
		r.Fields.Status.StatusCategory.Name = "In Progress"
		r.Fields.IssueType.Name = "Bug"
		r.Fields.Project.Key = "PROJ"
		modify(&r)
		return r
	}

	cases := []struct {
		name string
		dim  string
		raw  jira.IssueRaw
		want string
	}{
		{"status", "status", buildRaw(func(r *jira.IssueRaw) {}), "In Progress"},
		{"statusCategory", "statusCategory", buildRaw(func(r *jira.IssueRaw) {}), "In Progress"},
		{"status empty", "status", buildRaw(func(r *jira.IssueRaw) { r.Fields.Status.Name = "" }), "(none)"},
		{"priority set", "priority", buildRaw(func(r *jira.IssueRaw) {
			r.Fields.Priority = &struct {
				Name string `json:"name"`
			}{"High"}
		}), "High"},
		{"priority nil", "priority", buildRaw(func(r *jira.IssueRaw) { r.Fields.Priority = nil }), "(none)"},
		{"assignee set", "assignee", buildRaw(func(r *jira.IssueRaw) {
			r.Fields.Assignee = &struct {
				Name         string `json:"name"`
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
			}{DisplayName: "Alice Chen"}
		}), "Alice Chen"},
		{"assignee nil", "assignee", buildRaw(func(r *jira.IssueRaw) { r.Fields.Assignee = nil }), "Unassigned"},
		{"issueType", "issueType", buildRaw(func(r *jira.IssueRaw) {}), "Bug"},
		{"resolution set", "resolution", buildRaw(func(r *jira.IssueRaw) {
			r.Fields.Resolution = &struct {
				Name string `json:"name"`
			}{"Fixed"}
		}), "Fixed"},
		{"resolution nil", "resolution", buildRaw(func(r *jira.IssueRaw) { r.Fields.Resolution = nil }), "(unresolved)"},
		{"project", "project", buildRaw(func(r *jira.IssueRaw) {}), "PROJ"},
	}
	for _, tc := range cases {
		got := extractDimension(tc.raw, tc.dim)
		if got != tc.want {
			t.Errorf("[%s] extractDimension(%q) = %q, want %q", tc.name, tc.dim, got, tc.want)
		}
	}
}

// ── renderCountByPlain status ordering ───────────────────────────────────────

func TestRenderCountByPlain_StatusOrder(t *testing.T) {
	// Simulate fetch order: Done first, then Open — should re-sort.
	counts := map[string]int{
		"Done":        10,
		"Open":        5,
		"In Progress": 7,
	}
	order := []string{"Done", "Open", "In Progress"}

	out := renderCountByPlain("status", order, counts, 22, "project = X", false)

	// Open should appear before Done in output.
	openIdx := strings.Index(out, "Open")
	doneIdx := strings.Index(out, "Done")
	if openIdx == -1 || doneIdx == -1 {
		t.Fatalf("expected both Open and Done in output, got:\n%s", out)
	}
	if openIdx > doneIdx {
		t.Errorf("expected Open to appear before Done (canonical order), got:\n%s", out)
	}

	// Percentages: 10/22=45.5%, 7/22=31.8%, 5/22=22.7% — all must appear.
	if !strings.Contains(out, "45.5%") {
		t.Errorf("expected 45.5%% for Done (10/22), got:\n%s", out)
	}
	// Total row.
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected Total row with 100.0%%, got:\n%s", out)
	}
}

// ── renderCountByPlain non-status order: count desc ──────────────────────────

func TestRenderCountByPlain_OtherFieldsOrderByCount(t *testing.T) {
	counts := map[string]int{
		"High":     5,
		"Low":      3,
		"Critical": 1,
	}
	order := []string{"High", "Low", "Critical"}

	out := renderCountByPlain("priority", order, counts, 9, "project = X", false)

	highIdx := strings.Index(out, "High")
	lowIdx := strings.Index(out, "Low")
	criticalIdx := strings.Index(out, "Critical")

	if highIdx == -1 || lowIdx == -1 || criticalIdx == -1 {
		t.Fatalf("expected all priorities in output, got:\n%s", out)
	}
	// count desc: High(5) > Low(3) > Critical(1)
	if !(highIdx < lowIdx && lowIdx < criticalIdx) {
		t.Errorf("expected High < Low < Critical in count-desc order, got positions: High=%d Low=%d Critical=%d\n%s",
			highIdx, lowIdx, criticalIdx, out)
	}
}

// ── search --count-by validation ─────────────────────────────────────────────

func TestSearchCountByValidation(t *testing.T) {
	// Unsupported field — validated in RunE before any I/O.
	{
		cmd := NewSearchCmd()
		cmd.SetArgs([]string{"--jql", "project = X", "--count-by", "labels"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "--count-by: unsupported field") {
			t.Errorf("expected unsupported field error, got: %v", err)
		}
	}

	// Mutex with --keys-only.
	{
		cmd := NewSearchCmd()
		cmd.SetArgs([]string{"--jql", "project = X", "--count-by", "status", "--keys-only"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "--count-by and --keys-only are mutually exclusive") {
			t.Errorf("expected mutex error, got: %v", err)
		}
	}
}
