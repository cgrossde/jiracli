package jira

import (
	"encoding/json"
	"testing"
)

// makeRawWithTT constructs a minimal IssueRaw with time tracking and status info.
func makeRawWithTT(key, status, statusCat, issueType string, orig, rem, spent int64, spField string, sp *float64) IssueRaw {
	raw := IssueRaw{Key: key}
	raw.Fields.Summary = key + " summary"
	raw.Fields.Status.Name = status
	raw.Fields.Status.StatusCategory.Name = statusCat
	raw.Fields.IssueType.Name = issueType
	if orig != 0 || rem != 0 || spent != 0 {
		raw.Fields.TimeTracking = &struct {
			OriginalEstimateSeconds  int64 `json:"originalEstimateSeconds"`
			RemainingEstimateSeconds int64 `json:"remainingEstimateSeconds"`
			TimeSpentSeconds         int64 `json:"timeSpentSeconds"`
		}{
			OriginalEstimateSeconds:  orig,
			RemainingEstimateSeconds: rem,
			TimeSpentSeconds:         spent,
		}
	}
	if spField != "" && sp != nil {
		spBytes, _ := json.Marshal(*sp)
		raw.RawFields = map[string]json.RawMessage{
			spField: spBytes,
		}
	}
	return raw
}

func makeChild(key, status, statusCat, issueType string) ChildIssueRecord {
	return ChildIssueRecord{
		Key:            key,
		Summary:        key + " summary",
		Status:         status,
		StatusCategory: statusCat,
		IssueType:      issueType,
	}
}

// ── Test: empty children ────────────────────────────────────────────────────

func TestRollUpChildren_Empty(t *testing.T) {
	result := RollUpChildren("ACME-1", nil, nil, "")
	if result.EpicKey != "ACME-1" {
		t.Errorf("EpicKey: got %q, want %q", result.EpicKey, "ACME-1")
	}
	if result.Total.Children != 0 {
		t.Errorf("Total.Children: got %d, want 0", result.Total.Children)
	}
	if result.Total.StoryPoints != 0 {
		t.Errorf("Total.StoryPoints: got %v, want 0", result.Total.StoryPoints)
	}
	if result.Unestimated == nil || len(result.Unestimated) != 0 {
		t.Errorf("Unestimated: got %v, want empty slice", result.Unestimated)
	}
	if len(result.Buckets) != 3 {
		t.Errorf("Buckets: got %d, want 3", len(result.Buckets))
	}
}

// ── Test: 3 children same status bucket ────────────────────────────────────

func TestRollUpChildren_SameBucket(t *testing.T) {
	const spField = "customfield_10001"
	sp1, sp2, sp3 := float64(3), float64(5), float64(2)

	children := []ChildIssueRecord{
		makeChild("A-1", "In Progress", "In Progress", "Story"),
		makeChild("A-2", "In Progress", "In Progress", "Story"),
		makeChild("A-3", "In Progress", "In Progress", "Story"),
	}
	fullData := []IssueRaw{
		makeRawWithTT("A-1", "In Progress", "In Progress", "Story", 3600, 1800, 1800, spField, &sp1),
		makeRawWithTT("A-2", "In Progress", "In Progress", "Story", 7200, 5400, 1800, spField, &sp2),
		makeRawWithTT("A-3", "In Progress", "In Progress", "Story", 3600, 3600, 0, spField, &sp3),
	}

	result := RollUpChildren("EPIC-1", children, fullData, spField)

	if result.Total.Children != 3 {
		t.Errorf("Total.Children: got %d, want 3", result.Total.Children)
	}
	want := int64(3600 + 7200 + 3600)
	if result.Total.OriginalEstimateSecs != want {
		t.Errorf("Total.OriginalEstimateSecs: got %d, want %d", result.Total.OriginalEstimateSecs, want)
	}
	if result.Total.StoryPoints != float64(10) {
		t.Errorf("Total.StoryPoints: got %v, want 10", result.Total.StoryPoints)
	}
	if result.Total.PointedChildren != 3 {
		t.Errorf("Total.PointedChildren: got %d, want 3", result.Total.PointedChildren)
	}
	// All are In Progress → bucket[1]
	if result.Buckets[1].Children != 3 {
		t.Errorf("InProgress bucket.Children: got %d, want 3", result.Buckets[1].Children)
	}
	if result.Buckets[0].Children != 0 || result.Buckets[2].Children != 0 {
		t.Errorf("ToDo/Done buckets should be 0, got %d/%d", result.Buckets[0].Children, result.Buckets[2].Children)
	}
}

// ── Test: mixed status categories ──────────────────────────────────────────

func TestRollUpChildren_MixedStatus(t *testing.T) {
	children := []ChildIssueRecord{
		makeChild("A-1", "Open", "To Do", "Story"),
		makeChild("A-2", "In Progress", "In Progress", "Story"),
		makeChild("A-3", "Done", "Done", "Story"),
	}
	fullData := []IssueRaw{
		makeRawWithTT("A-1", "Open", "To Do", "Story", 7200, 7200, 0, "", nil),
		makeRawWithTT("A-2", "In Progress", "In Progress", "Story", 7200, 3600, 3600, "", nil),
		makeRawWithTT("A-3", "Done", "Done", "Story", 7200, 0, 7200, "", nil),
	}

	result := RollUpChildren("EPIC-1", children, fullData, "")

	if result.Buckets[0].Children != 1 {
		t.Errorf("ToDo Children: got %d, want 1", result.Buckets[0].Children)
	}
	if result.Buckets[1].Children != 1 {
		t.Errorf("InProgress Children: got %d, want 1", result.Buckets[1].Children)
	}
	if result.Buckets[2].Children != 1 {
		t.Errorf("Done Children: got %d, want 1", result.Buckets[2].Children)
	}
	if result.Total.Children != 3 {
		t.Errorf("Total.Children: got %d, want 3", result.Total.Children)
	}
	if result.Total.OriginalEstimateSecs != 21600 {
		t.Errorf("Total.OriginalEstimateSecs: got %d, want 21600", result.Total.OriginalEstimateSecs)
	}
	if result.Total.TimeSpentSecs != 10800 {
		t.Errorf("Total.TimeSpentSecs: got %d, want 10800", result.Total.TimeSpentSecs)
	}
}

// ── Test: Done children counted in Total ────────────────────────────────────

func TestRollUpChildren_DoneCountedInTotal(t *testing.T) {
	children := []ChildIssueRecord{
		makeChild("A-1", "Done", "Done", "Story"),
		makeChild("A-2", "Done", "Done", "Story"),
	}
	fullData := []IssueRaw{
		makeRawWithTT("A-1", "Done", "Done", "Story", 3600, 0, 3600, "", nil),
		makeRawWithTT("A-2", "Done", "Done", "Story", 7200, 0, 7200, "", nil),
	}

	result := RollUpChildren("EPIC-1", children, fullData, "")

	if result.Total.OriginalEstimateSecs != 10800 {
		t.Errorf("Total.OriginalEstimateSecs: got %d, want 10800", result.Total.OriginalEstimateSecs)
	}
	if result.Total.TimeSpentSecs != 10800 {
		t.Errorf("Total.TimeSpentSecs: got %d, want 10800", result.Total.TimeSpentSecs)
	}
	if result.Buckets[2].EstimatedChildren != 2 {
		t.Errorf("Done.EstimatedChildren: got %d, want 2", result.Buckets[2].EstimatedChildren)
	}
}

// ── Test: SP-only child (no time) ──────────────────────────────────────────

func TestRollUpChildren_SPOnlyUnestimated(t *testing.T) {
	const spField = "customfield_10001"
	sp := float64(5)

	children := []ChildIssueRecord{makeChild("A-1", "Open", "To Do", "Story")}
	fullData := []IssueRaw{
		makeRawWithTT("A-1", "Open", "To Do", "Story", 0, 0, 0, spField, &sp),
	}

	result := RollUpChildren("EPIC-1", children, fullData, spField)

	if result.Total.StoryPoints != float64(5) {
		t.Errorf("Total.StoryPoints: got %v, want 5", result.Total.StoryPoints)
	}
	if len(result.Unestimated) != 1 {
		t.Errorf("Unestimated len: got %d, want 1", len(result.Unestimated))
	}
	if result.Unestimated[0].Key != "A-1" {
		t.Errorf("Unestimated[0].Key: got %q, want %q", result.Unestimated[0].Key, "A-1")
	}
}

// ── Test: 8-child mixed fixture ─────────────────────────────────────────────
// 8 children: 2 with time estimates (40h, 56h), 2 with SP only, 4 unestimated.
// Status: 3 ToDo, 3 InProgress, 2 Done.

func TestRollUpChildren_Fixture(t *testing.T) {
	const spField = "customfield_10001"
	sp5 := float64(5)
	sp2 := float64(2)
	sp10 := float64(10)
	sp12 := float64(12)

	children := []ChildIssueRecord{
		makeChild("ACME-201", "In Progress", "In Progress", "Story"),
		makeChild("ACME-202", "In Progress", "In Progress", "Story"),
		makeChild("ACME-203", "In Progress", "In Progress", "Story"),
		makeChild("ACME-204", "Open", "To Do", "Story"),
		makeChild("ACME-205", "Open", "To Do", "Story"),
		makeChild("ACME-206", "Open", "To Do", "Story"),
		makeChild("ACME-207", "Closed", "Done", "Story"),
		makeChild("ACME-208", "Closed", "Done", "Story"),
	}
	fullData := []IssueRaw{
		makeRawWithTT("ACME-201", "In Progress", "In Progress", "Story", 40*3600, 32*3600, 8*3600, spField, &sp5),
		makeRawWithTT("ACME-202", "In Progress", "In Progress", "Story", 56*3600, 48*3600, 8*3600, spField, &sp2),
		makeRawWithTT("ACME-203", "In Progress", "In Progress", "Story", 0, 0, 0, "", nil),
		makeRawWithTT("ACME-204", "Open", "To Do", "Story", 0, 0, 0, "", nil),
		makeRawWithTT("ACME-205", "Open", "To Do", "Story", 0, 0, 0, spField, &sp10),
		makeRawWithTT("ACME-206", "Open", "To Do", "Story", 0, 0, 0, spField, &sp12),
		makeRawWithTT("ACME-207", "Closed", "Done", "Story", 0, 0, 0, "", nil),
		makeRawWithTT("ACME-208", "Closed", "Done", "Story", 0, 0, 0, "", nil),
	}

	result := RollUpChildren("ACME-100", children, fullData, spField)

	if result.EpicKey != "ACME-100" {
		t.Errorf("EpicKey: got %q", result.EpicKey)
	}
	if result.Total.Children != 8 {
		t.Errorf("Total.Children: got %d, want 8", result.Total.Children)
	}
	if result.Total.OriginalEstimateSecs != 96*3600 {
		t.Errorf("Total.OriginalEstimateSecs: got %d, want %d", result.Total.OriginalEstimateSecs, 96*3600)
	}
	if result.Total.RemainingEstimateSecs != 80*3600 {
		t.Errorf("Total.RemainingEstimateSecs: got %d, want %d", result.Total.RemainingEstimateSecs, 80*3600)
	}
	if result.Total.TimeSpentSecs != 16*3600 {
		t.Errorf("Total.TimeSpentSecs: got %d, want %d", result.Total.TimeSpentSecs, 16*3600)
	}
	wantSP := float64(5 + 2 + 10 + 12) // 29
	if result.Total.StoryPoints != wantSP {
		t.Errorf("Total.StoryPoints: got %v, want %v", result.Total.StoryPoints, wantSP)
	}
	if result.Total.PointedChildren != 4 {
		t.Errorf("Total.PointedChildren: got %d, want 4", result.Total.PointedChildren)
	}
	if result.Total.EstimatedChildren != 2 {
		t.Errorf("Total.EstimatedChildren: got %d, want 2", result.Total.EstimatedChildren)
	}
	// 6 unestimated (no original estimate)
	if len(result.Unestimated) != 6 {
		t.Errorf("Unestimated len: got %d, want 6", len(result.Unestimated))
	}

	if result.Buckets[0].Children != 3 {
		t.Errorf("ToDo Children: got %d, want 3", result.Buckets[0].Children)
	}
	if result.Buckets[1].Children != 3 {
		t.Errorf("InProgress Children: got %d, want 3", result.Buckets[1].Children)
	}
	if result.Buckets[2].Children != 2 {
		t.Errorf("Done Children: got %d, want 2", result.Buckets[2].Children)
	}
}
