package cmd

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
)

// ── renderRollupTree unit tests (no I/O) ────────────────────────────────────

func makeTestSubject(key, issueType string) jira.IssueRaw {
	raw := jira.IssueRaw{Key: key}
	raw.Fields.Summary = "Test Summary for " + key
	raw.Fields.Status.Name = "In Progress"
	raw.Fields.Status.StatusCategory.Name = "In Progress"
	raw.Fields.IssueType.Name = issueType
	return raw
}

func makeTree(subjectKey string, l1Nodes []jira.RollupNode) jira.RollupTree {
	l1Row := jira.AggregateNodes(l1Nodes, "Level 1", false)
	l1Row.TotalCount = len(l1Nodes)
	return jira.RollupTree{
		SubjectKey:       subjectKey,
		SubjectIssueType: "Epic",
		SubjectSummary:   "Test Epic",
		SubjectRow:       jira.RollupRow{Label: "Epic " + subjectKey + " (own)", TotalCount: 1},
		Rows:             []jira.RollupRow{l1Row},
		MaxFetchedDepth:  1,
	}
}

func TestRenderRollupTree_Empty(t *testing.T) {
	raw := makeTestSubject("ACME-1", "Epic")
	tree := makeTree("ACME-1", nil)
	out := renderRollupTree(raw, tree, false, 1, false, "")
	if !strings.Contains(out, "ACME-1") {
		t.Errorf("expected key in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("expected Total row in output, got:\n%s", out)
	}
}

func TestRenderRollupTree_WithEstimates(t *testing.T) {
	raw := makeTestSubject("ACME-100", "Epic")
	// Own TT on epic
	raw.Fields.TimeTracking = &struct {
		OriginalEstimateSeconds  int64 `json:"originalEstimateSeconds"`
		RemainingEstimateSeconds int64 `json:"remainingEstimateSeconds"`
		TimeSpentSeconds         int64 `json:"timeSpentSeconds"`
	}{
		OriginalEstimateSeconds:  240 * 3600,
		RemainingEstimateSeconds: 58 * 3600,
		TimeSpentSeconds:         240 * 3600,
	}

	sp5 := float64(5)
	l1Nodes := []jira.RollupNode{
		{Key: "ACME-101", Summary: "Story 1", Status: "In Progress", IssueType: "Story",
			OriginalEstimateSecs: 40 * 3600, RemainingEstimateSecs: 32 * 3600, TimeSpentSecs: 8 * 3600, StoryPoints: &sp5},
		{Key: "ACME-102", Summary: "Story 2", Status: "In Progress", IssueType: "Story",
			OriginalEstimateSecs: 56 * 3600, RemainingEstimateSecs: 48 * 3600, TimeSpentSecs: 8 * 3600},
	}
	tree := makeTree("ACME-100", l1Nodes)
	tree.SubjectRow = jira.SubjectRowFromRaw(raw, "")

	out := renderRollupTree(raw, tree, false, 1, false, "")

	if !strings.Contains(out, "ACME-100") {
		t.Errorf("missing key in output")
	}
	if !strings.Contains(out, "Level 1") {
		t.Errorf("missing Level 1 row")
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("missing Total row")
	}
	// Own row shows 30d (240h / 8h per workday)
	if !strings.Contains(out, "30d") {
		t.Errorf("expected 30d (own TT, was 240h) in output, got:\n%s", out)
	}
	// L1 row shows 12d planned (96h / 8h per workday)
	if !strings.Contains(out, "12d") {
		t.Errorf("expected 12d (children planned, was 96h) in output, got:\n%s", out)
	}
}

func TestRenderRollupTree_WithList(t *testing.T) {
	raw := makeTestSubject("EPIC-1", "Epic")
	sp := float64(3)
	l1Nodes := []jira.RollupNode{
		{Key: "STORY-1", Summary: "First story", Status: "Open", IssueType: "Story",
			OriginalEstimateSecs: 7200, StoryPoints: &sp},
		{Key: "STORY-2", Summary: "Second story", Status: "Done", IssueType: "Story"},
	}
	tree := makeTree("EPIC-1", l1Nodes)
	tree.Nodes = l1Nodes // --list enabled

	out := renderRollupTree(raw, tree, false, 1, false, "")

	if !strings.Contains(out, "Children:") {
		t.Errorf("expected Children: section with --list nodes, got:\n%s", out)
	}
	if !strings.Contains(out, "STORY-1") {
		t.Errorf("expected STORY-1 in list, got:\n%s", out)
	}
	if !strings.Contains(out, "STORY-2") {
		t.Errorf("expected STORY-2 in list, got:\n%s", out)
	}
}

func TestShowRollup_InvalidRef(t *testing.T) {
	_, err := ShowRollup(context.Background(), ShowRollupFlags{}, "not-a-valid-key!@#")
	if err == nil {
		t.Fatal("expected error for invalid ref, got nil")
	}
	if !strings.Contains(err.Error(), "show rollup requires a plain issue key") {
		t.Errorf("expected corrective error message, got: %v", err)
	}
}

// ── JSON shape test ─────────────────────────────────────────────────────────

func TestRollupTreeJSONShape(t *testing.T) {
	sp := float64(5)
	nodes := []jira.RollupNode{{Key: "X-1", StoryPoints: &sp}}
	tree := makeTree("EPIC-1", nodes)
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	for _, k := range []string{"subjectKey", "subject", "rows", "hasDeeperLevel", "maxFetchedDepth"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in JSON: %s", k, string(data))
		}
	}
}

// ── ChildJQL routing ────────────────────────────────────────────────────────

func TestChildJQL_Epic(t *testing.T) {
	jql := jira.ChildJQL("Epic", "ACME-1", "customfield_10001", "customfield_10002")
	if !strings.Contains(jql, "Epic Link") {
		t.Errorf("Epic should use Epic Link JQL, got: %s", jql)
	}
}

func TestChildJQL_Initiative(t *testing.T) {
	jql := jira.ChildJQL("Initiative", "PROJ-1", "customfield_10001", "customfield_10002")
	if !strings.Contains(jql, "10002") {
		t.Errorf("Initiative should use ParentLink cf[ID] JQL, got: %s", jql)
	}
}

func TestChildJQL_Story(t *testing.T) {
	jql := jira.ChildJQL("Story", "ACME-1", "customfield_10001", "customfield_10002")
	if !strings.Contains(jql, "parent") {
		t.Errorf("Story should use parent = KEY JQL, got: %s", jql)
	}
}

// ── typeCountLabel ──────────────────────────────────────────────────────────

func TestTypeCountLabel_Empty(t *testing.T) {
	got := typeCountLabel(nil, 0)
	if got != "0 children" {
		t.Errorf("got %q", got)
	}
}

func TestTypeCountLabel_SingleType(t *testing.T) {
	got := typeCountLabel(map[string]int{"Story": 8}, 8)
	if got != "8 Storys" {
		t.Errorf("got %q", got)
	}
}

func TestTypeCountLabel_Mixed(t *testing.T) {
	got := typeCountLabel(map[string]int{"Epic": 3, "Bug": 2}, 5)
	// Epics(3) > Bugs(2), so Epics first.
	if !strings.Contains(got, "3 Epics") || !strings.Contains(got, "2 Bugs") {
		t.Errorf("got %q, want '3 Epics, 2 Bugs'", got)
	}
}

func TestTypeCountLabel_Singular(t *testing.T) {
	got := typeCountLabel(map[string]int{"Epic": 1}, 1)
	if got != "1 Epic" {
		t.Errorf("got %q", got)
	}
}

// ── Total = own + all levels ─────────────────────────────────────────────────

func TestRenderRollupTree_TotalAllLevels(t *testing.T) {
	// Own = 240h. L1 = 96h. L2 = 0h.
	// Bug: old code used own+deepest = 240+0 = 240h.
	// Fix: new code sums own+L1+L2 = 336h.
	raw := makeTestSubject("EPIC-1", "Epic")
	raw.Fields.TimeTracking = &struct {
		OriginalEstimateSeconds  int64 `json:"originalEstimateSeconds"`
		RemainingEstimateSeconds int64 `json:"remainingEstimateSeconds"`
		TimeSpentSeconds         int64 `json:"timeSpentSeconds"`
	}{OriginalEstimateSeconds: 240 * 3600, RemainingEstimateSeconds: 58 * 3600, TimeSpentSeconds: 182 * 3600}

	l1Row := jira.RollupRow{Label: "Level 1 — 8 Storys", OriginalEstimateSecs: 96 * 3600, TotalCount: 8}
	l2Row := jira.RollupRow{Label: "Level 2 — 13 Sub-tasks", OriginalEstimateSecs: 0, TotalCount: 13}
	subjectRow := jira.SubjectRowFromRaw(raw, "")
	tree := jira.RollupTree{
		SubjectKey:       "EPIC-1",
		SubjectIssueType: "Epic",
		SubjectSummary:   "Test Epic",
		SubjectRow:       subjectRow,
		Rows:             []jira.RollupRow{l1Row, l2Row},
		MaxFetchedDepth:  2,
	}

	out := renderRollupTree(raw, tree, false, 2, false, "")
	// Total planned must be 42d (336h / 8h per workday: own 240 + L1 96 + L2 0).
	if !strings.Contains(out, "42d") {
		t.Errorf("expected Total to be 42d (was 336h, own+L1+L2), got:\n%s", out)
	}
	if !strings.Contains(out, "Total (all levels)") {
		t.Errorf("expected 'Total (all levels)' label at depth 2, got:\n%s", out)
	}
}

// ── HasChildren indicator in --list ─────────────────────────────────────────

func TestRenderRollupTree_HasChildrenIndicator(t *testing.T) {
	raw := makeTestSubject("PROJ-1", "Initiative")
	nodes := []jira.RollupNode{
		{Key: "EPIC-1", Summary: "Epic with children", IssueType: "Epic", HasChildren: true},
		{Key: "EPIC-2", Summary: "Epic without children", IssueType: "Epic", HasChildren: false},
	}
	l1Row := jira.AggregateNodes(nodes, "Level 1 — 2 Epics", false)
	l1Row.TotalCount = 2
	tree := jira.RollupTree{
		SubjectKey:       "PROJ-1",
		SubjectIssueType: "Initiative",
		SubjectSummary:   "Test Initiative",
		SubjectRow:       jira.RollupRow{Label: "Initiative PROJ-1 (own)", TotalCount: 1},
		Rows:             []jira.RollupRow{l1Row},
		Nodes:            nodes,
		MaxFetchedDepth:  1,
	}

	out := renderRollupTree(raw, tree, true, 1, false, "")
	if !strings.Contains(out, "▸") {
		t.Errorf("expected ▸ indicator for node with children, got:\n%s", out)
	}
}

// ── nodeGroupKey ─────────────────────────────────────────────────────────────

func TestNodeGroupKey(t *testing.T) {
	cases := []struct {
		groupBy string
		node    jira.RollupNode
		want    string
	}{
		{"assignee", jira.RollupNode{Assignee: "Alice"}, "Alice"},
		{"assignee", jira.RollupNode{Assignee: ""}, "Unassigned"},
		{"status", jira.RollupNode{Status: "In Progress"}, "In Progress"},
		{"status", jira.RollupNode{Status: ""}, "(no status)"},
		{"statusCategory", jira.RollupNode{StatusCategory: "Done"}, "Done"},
		{"statusCategory", jira.RollupNode{StatusCategory: ""}, "(no category)"},
		{"", jira.RollupNode{Status: "Open"}, ""},
	}
	for _, tc := range cases {
		got := nodeGroupKey(tc.node, tc.groupBy)
		if got != tc.want {
			t.Errorf("nodeGroupKey(%q, node{status=%q,cat=%q,assignee=%q}) = %q, want %q",
				tc.groupBy, tc.node.Status, tc.node.StatusCategory, tc.node.Assignee, got, tc.want)
		}
	}
}

// ── statusRank / statusRowLess ───────────────────────────────────────────────

func TestStatusRowLess_Category(t *testing.T) {
	rows := []jira.RollupRow{
		{Label: "Done", TotalCount: 5},
		{Label: "To Do", TotalCount: 3},
		{Label: "(no category)", TotalCount: 1},
		{Label: "In Progress", TotalCount: 2},
	}
	sort.SliceStable(rows, statusRowLess(rows, true))
	want := []string{"To Do", "In Progress", "Done", "(no category)"}
	for i, w := range want {
		if rows[i].Label != w {
			t.Errorf("sorted[%d] = %q, want %q", i, rows[i].Label, w)
		}
	}
}

func TestStatusRowLess_Names(t *testing.T) {
	rows := []jira.RollupRow{
		{Label: "Closed"},
		{Label: "Open"},
		{Label: "In Progress"},
		{Label: "Pending Review"},
		{Label: "Blocked"},
	}
	sort.SliceStable(rows, statusRowLess(rows, false))
	// Blocked=0, Open=1, In Progress=2, Pending Review=2 (alpha after), Closed=3
	if rows[0].Label != "Blocked" {
		t.Errorf("first = %q, want Blocked", rows[0].Label)
	}
	if rows[len(rows)-1].Label != "Closed" {
		t.Errorf("last = %q, want Closed", rows[len(rows)-1].Label)
	}
}

// ── renderRollupJQL group-by-status ─────────────────────────────────────────

func TestRenderRollupJQL_GroupByStatus(t *testing.T) {
	openRow := jira.RollupRow{
		Label:                "Open",
		OriginalEstimateSecs: 40 * 3600,
		TimeSpentSecs:        5 * 3600,
		TotalCount:           3,
	}
	doneRow := jira.RollupRow{
		Label:                "Closed",
		OriginalEstimateSecs: 80 * 3600,
		TimeSpentSecs:        80 * 3600,
		TotalCount:           5,
	}
	totalRow := jira.RollupRow{
		Label:                "Total",
		OriginalEstimateSecs: 120 * 3600,
		TimeSpentSecs:        85 * 3600,
		TotalCount:           8,
	}
	tree := jira.RollupTree{
		SubjectKey:     "(jql)",
		SubjectSummary: "project = PROJ",
		Rows:           []jira.RollupRow{openRow, doneRow, totalRow},
		GroupBy:        "status",
	}

	out := renderRollupJQL(tree)

	if !strings.Contains(out, "Status") {
		t.Errorf("expected 'Status' column header, got:\n%s", out)
	}
	if !strings.Contains(out, "Count") {
		t.Errorf("expected 'Count' column, got:\n%s", out)
	}
	if !strings.Contains(out, "Open") || !strings.Contains(out, "Closed") {
		t.Errorf("expected status rows, got:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("expected Total row, got:\n%s", out)
	}
}

// ── renderRollupHierarchyGrouped ─────────────────────────────────────────────

func TestRenderRollupHierarchyGrouped_Empty(t *testing.T) {
	raw := makeTestSubject("INIT-1", "Initiative")
	levels := []groupedLevel{
		{label: "Level 1 — 0 children", rows: nil, total: 0},
	}
	out := renderRollupHierarchyGrouped(raw, levels, "status", "")
	if !strings.Contains(out, "INIT-1") {
		t.Errorf("expected key in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Level 1") {
		t.Errorf("expected level label in output, got:\n%s", out)
	}
}

func TestRenderRollupHierarchyGrouped_WithCount(t *testing.T) {
	raw := makeTestSubject("INIT-1", "Initiative")
	rows := []jira.RollupRow{
		{Label: "In Progress", OriginalEstimateSecs: 80 * 3600, TimeSpentSecs: 20 * 3600, TotalCount: 3},
		{Label: "Open", OriginalEstimateSecs: 40 * 3600, TimeSpentSecs: 0, TotalCount: 2},
		{Label: "Closed", OriginalEstimateSecs: 60 * 3600, TimeSpentSecs: 60 * 3600, TotalCount: 5},
	}
	levels := []groupedLevel{
		{label: "Level 1 — 10 Epics", rows: rows, total: 10},
	}

	out := renderRollupHierarchyGrouped(raw, levels, "status", "")

	if !strings.Contains(out, "Level 1") {
		t.Errorf("expected level label above table, got:\n%s", out)
	}
	if !strings.Contains(out, "Status") {
		t.Errorf("expected 'Status' header, got:\n%s", out)
	}
	if !strings.Contains(out, "Count") {
		t.Errorf("expected 'Count' column, got:\n%s", out)
	}
	if !strings.Contains(out, "Total") {
		t.Errorf("expected Total row, got:\n%s", out)
	}
	if !strings.Contains(out, "10") {
		t.Errorf("expected total count 10, got:\n%s", out)
	}
}

func TestRenderRollupHierarchyGrouped_TwoLevels(t *testing.T) {
	raw := makeTestSubject("INIT-1", "Initiative")
	l1Rows := []jira.RollupRow{
		{Label: "In Progress", TotalCount: 3},
		{Label: "Closed", TotalCount: 2},
	}
	l2Rows := []jira.RollupRow{
		{Label: "Open", TotalCount: 10},
		{Label: "Closed", TotalCount: 8},
	}
	levels := []groupedLevel{
		{label: "Level 1 — 5 Epics", rows: l1Rows, total: 5},
		{label: "Level 2 — 18 Stories", rows: l2Rows, total: 18},
	}

	out := renderRollupHierarchyGrouped(raw, levels, "status", "")

	if !strings.Contains(out, "Level 1") {
		t.Errorf("expected Level 1 label, got:\n%s", out)
	}
	if !strings.Contains(out, "Level 2") {
		t.Errorf("expected Level 2 label, got:\n%s", out)
	}
	// Both levels have Total rows — count at least 2 occurrences.
	if strings.Count(out, "Total") < 2 {
		t.Errorf("expected at least 2 Total rows (one per level), got:\n%s", out)
	}
	// Label appears before its table header.
	l1Idx := strings.Index(out, "Level 1")
	statusIdx := strings.Index(out, "Status")
	if l1Idx > statusIdx {
		t.Errorf("expected level label before table header, Level1@%d > Status@%d", l1Idx, statusIdx)
	}
}

// ── ShowRollup --group-by validation ─────────────────────────────────────────

func TestShowRollup_GroupByValidation(t *testing.T) {
	// Unsupported group-by value.
	_, err := ShowRollup(context.Background(), ShowRollupFlags{
		JQL:     "project = X",
		GroupBy: "foo",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "--group-by: supported values") {
		t.Errorf("expected group-by validation error, got: %v", err)
	}

	// assignee requires --jql or --sprint, not <KEY>.
	_, err = ShowRollup(context.Background(), ShowRollupFlags{
		GroupBy: "assignee",
	}, "PROJ-1")
	if err == nil || !strings.Contains(err.Error(), "--group-by=assignee requires --jql or --sprint") {
		t.Errorf("expected assignee+key validation error, got: %v", err)
	}
}
