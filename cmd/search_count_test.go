package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
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

	out := renderCountByPlain("status", order, counts, 22, 22, "project = X", "")

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

	out := renderCountByPlain("priority", order, counts, 9, 9, "project = X", "")

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

// ── count-by safety cap (broad-query guardrail) ──────────────────────────────

// fakeSearchServer serves /rest/api/2/search with a fixed total, returning
// synthetic issues (all in project "X") for whatever page is requested.
func fakeSearchServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jira.SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode search request: %v", err)
		}
		remaining := total - req.StartAt
		if remaining < 0 {
			remaining = 0
		}
		n := req.MaxResults
		if n > remaining {
			n = remaining
		}
		issues := make([]jira.IssueRaw, n)
		for i := range issues {
			issues[i].Key = fmt.Sprintf("X-%d", req.StartAt+i+1)
			issues[i].Fields.Project.Key = "X"
		}
		resp := jira.SearchResponse{Total: total, StartAt: req.StartAt, MaxResults: req.MaxResults, Issues: issues}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func fakeSearchClient(serverURL string) *jira.Client {
	return jira.New(keychain.Entry{Profile: "test", URL: serverURL, PAT: "test-pat"})
}

func TestCountBy_UnderCap_NoTruncation(t *testing.T) {
	srv := fakeSearchServer(t, 42)
	defer srv.Close()
	client := fakeSearchClient(srv.URL)

	out, err := countByFromRawSearch(context.Background(), client, "project = X", "project", false, countByDefaultCap, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "total: 42 issues") {
		t.Errorf("expected full count of 42, got:\n%s", out)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("did not expect a truncation warning, got:\n%s", out)
	}
}

func TestCountBy_OverDefaultCap_Aborts(t *testing.T) {
	srv := fakeSearchServer(t, countByDefaultCap+1)
	defer srv.Close()
	client := fakeSearchClient(srv.URL)

	_, err := countByFromRawSearch(context.Background(), client, "project = X", "project", false, countByDefaultCap, true)
	if err == nil {
		t.Fatal("expected an error aborting the aggregation, got nil")
	}
	if !strings.Contains(err.Error(), "count-by aborted") ||
		!strings.Contains(err.Error(), "--all") ||
		!strings.Contains(err.Error(), "--limit") {
		t.Errorf("expected corrective error mentioning --all and --limit, got: %v", err)
	}
}

func TestCountBy_ExplicitLimit_CapsAndReportsPartial(t *testing.T) {
	srv := fakeSearchServer(t, 1000)
	defer srv.Close()
	client := fakeSearchClient(srv.URL)

	// Simulates an explicit --limit 100 (abortIfOverCap=false since the user
	// opted into a cap themselves).
	out, err := countByFromRawSearch(context.Background(), client, "project = X", "project", false, 100, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "100 of 1000 matched issues counted") {
		t.Errorf("expected partial-count header, got:\n%s", out)
	}
	if !strings.Contains(out, "⚠") || !strings.Contains(out, "--limit 100") || !strings.Contains(out, "--all") {
		t.Errorf("expected a truncation warning mentioning --limit and --all, got:\n%s", out)
	}
	// The Total row should reflect exactly the capped 100, not 1000.
	totalLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Total") {
			totalLine = line
		}
	}
	if !strings.Contains(totalLine, "100") || strings.Contains(totalLine, "1000") {
		t.Errorf("expected Total row reflecting capped count 100, got line: %q\nfull output:\n%s", totalLine, out)
	}
}

func TestCountBy_All_BypassesCapEvenWhenHuge(t *testing.T) {
	srv := fakeSearchServer(t, countByDefaultCap+50)
	defer srv.Close()
	client := fakeSearchClient(srv.URL)

	// capN=0 simulates --all.
	out, err := countByFromRawSearch(context.Background(), client, "project = X", "project", false, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := fmt.Sprintf("total: %d issues", countByDefaultCap+50)
	if !strings.Contains(out, want) {
		t.Errorf("expected uncapped full count, got:\n%s", out)
	}
}

// TestRunCountBy_FlagWiring exercises runCountBy's flag interpretation
// (All / LimitSet / default cap) directly against SearchFlags values, the
// same way NewSearchCmd's RunE would construct them after parsing.
func TestRunCountBy_FlagWiring(t *testing.T) {
	cases := []struct {
		name       string
		flags      SearchFlags
		total      int
		expectErr  bool
		expectPart string
	}{
		{"default cap under limit", SearchFlags{CountBy: "project"}, 10, false, "total: 10 issues"},
		{"default cap exceeded aborts", SearchFlags{CountBy: "project"}, countByDefaultCap + 1, true, "count-by aborted"},
		{"--all bypasses cap", SearchFlags{CountBy: "project", All: true}, countByDefaultCap + 1, false, fmt.Sprintf("total: %d issues", countByDefaultCap+1)},
		{"explicit --limit caps with note", SearchFlags{CountBy: "project", Limit: 5, LimitSet: true}, 50, false, "5 of 50 matched issues counted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeSearchServer(t, tc.total)
			defer srv.Close()
			client := fakeSearchClient(srv.URL)

			out, err := runCountBy(context.Background(), client, "project = X", tc.flags)
			if tc.expectErr {
				if err == nil || !strings.Contains(err.Error(), tc.expectPart) {
					t.Fatalf("expected error containing %q, got: %v", tc.expectPart, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, tc.expectPart) {
				t.Errorf("expected output containing %q, got:\n%s", tc.expectPart, out)
			}
		})
	}
}
