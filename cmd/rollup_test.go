package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
)

// ── renderRollup unit tests (no I/O) ───────────────────────────────────────

func makeTestEpic(key string) jira.IssueRaw {
	raw := jira.IssueRaw{Key: key}
	raw.Fields.Summary = "Test Epic Summary"
	raw.Fields.Status.Name = "In Progress"
	raw.Fields.Status.StatusCategory.Name = "In Progress"
	raw.Fields.IssueType.Name = "Epic"
	return raw
}

func TestRenderRollup_EmptyEpic(t *testing.T) {
	epicRaw := makeTestEpic("ACME-1")
	r := jira.RolledUpEstimates{
		EpicKey:       "ACME-1",
		Total:         jira.RollupBucket{Category: "Total", Children: 0},
		Buckets:       []jira.RollupBucket{{Category: "To Do"}, {Category: "In Progress"}, {Category: "Done"}},
		Unestimated:   []jira.ChildIssueRecord{},
		TotalChildren: 0,
	}
	out := renderRollup(epicRaw, r, "", false, 0)
	if !strings.Contains(out, "ACME-1") {
		t.Errorf("expected epic key in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Breakdown by status:") {
		t.Errorf("expected Breakdown by status: in output, got:\n%s", out)
	}
}

func TestRenderRollup_WithEstimates(t *testing.T) {
	epicRaw := makeTestEpic("ACME-100")
	sp := float64(22)
	_ = sp
	r := jira.RolledUpEstimates{
		EpicKey: "ACME-100",
		Total: jira.RollupBucket{
			Category:              "Total",
			Children:              8,
			OriginalEstimateSecs:  96 * 3600,
			RemainingEstimateSecs: 80 * 3600,
			TimeSpentSecs:         16 * 3600,
			StoryPoints:           22,
			PointedChildren:       5,
			EstimatedChildren:     3,
		},
		Buckets: []jira.RollupBucket{
			{Category: "To Do", Children: 3},
			{Category: "In Progress", Children: 3, OriginalEstimateSecs: 96 * 3600, RemainingEstimateSecs: 80 * 3600, TimeSpentSecs: 16 * 3600},
			{Category: "Done", Children: 2},
		},
		Unestimated:   []jira.ChildIssueRecord{{Key: "ACME-204"}, {Key: "ACME-205"}},
		TotalChildren: 8,
	}
	out := renderRollup(epicRaw, r, "", false, 8)

	// Header
	if !strings.Contains(out, "ACME-100") {
		t.Errorf("missing epic key in output")
	}
	// Estimates line
	if !strings.Contains(out, "Estimates:") {
		t.Errorf("missing Estimates: line")
	}
	if !strings.Contains(out, "Planned 96h") {
		t.Errorf("missing Planned 96h in output, got:\n%s", out)
	}
	// Story Points line
	if !strings.Contains(out, "Story Points:") {
		t.Errorf("missing Story Points: line")
	}
	// Status breakdown
	if !strings.Contains(out, "Breakdown by status:") {
		t.Errorf("missing Breakdown by status: header")
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("missing Total row")
	}
	// Unestimated block
	if !strings.Contains(out, "Unestimated children") {
		t.Errorf("missing Unestimated children block")
	}
	if !strings.Contains(out, "ACME-204") {
		t.Errorf("missing ACME-204 in unestimated list")
	}
}

func TestShowRollup_InvalidRef(t *testing.T) {
	// A ref that doesn't look like an issue key — ShowRollup should return an error
	// before hitting any I/O.
	_, err := ShowRollup(context.Background(), ShowRollupFlags{}, "not-a-valid-key!@#")
	if err == nil {
		t.Fatal("expected error for invalid ref, got nil")
	}
	if !strings.Contains(err.Error(), "show rollup requires a plain issue key") {
		t.Errorf("expected corrective error message, got: %v", err)
	}
}

// ── JSON shape test (via RollUpChildren → JSON) ─────────────────────────────

func TestRollupJSONShape(t *testing.T) {
	r := jira.RollUpChildren("EPIC-1", nil, nil, "")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	requiredKeys := []string{"epicKey", "total", "buckets", "unestimated", "truncated", "totalChildren"}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in JSON output: %s", k, string(data))
		}
	}
}
