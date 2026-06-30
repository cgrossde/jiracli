package jira

import "encoding/json"

// RollupRow holds aggregated estimates for one conceptual level in the hierarchy.
type RollupRow struct {
	Label                 string         `json:"label"`
	OriginalEstimateSecs  int64          `json:"originalEstimateSeconds"`
	RemainingEstimateSecs int64          `json:"remainingEstimateSeconds"`
	TimeSpentSecs         int64          `json:"timeSpentSeconds"`
	StoryPoints           float64        `json:"storyPoints"`
	PointedCount          int            `json:"pointedCount"`   // items with SP set
	TotalCount            int            `json:"totalCount"`     // items in this level
	Truncated             bool           `json:"truncated,omitempty"`
	IssueTypeCounts       map[string]int `json:"issueTypeCounts,omitempty"` // e.g. {"Epic":3,"Bug":2}
}

// RollupNode is one child entry, used in --list output.
type RollupNode struct {
	Key                   string   `json:"key"`
	Summary               string   `json:"summary"`
	Status                string   `json:"status"`
	StatusCategory        string   `json:"statusCategory"`
	IssueType             string   `json:"issueType"`
	Assignee              string   `json:"assignee,omitempty"` // display name; empty when unassigned
	OriginalEstimateSecs  int64    `json:"originalEstimateSeconds"`
	RemainingEstimateSecs int64    `json:"remainingEstimateSeconds"`
	TimeSpentSecs         int64    `json:"timeSpentSeconds"`
	StoryPoints           *float64 `json:"storyPoints,omitempty"`
	ChildrenTotal         int      `json:"childrenTotal"` // server-reported count of next level
	HasChildren           bool     `json:"hasChildren"`   // true when ChildrenTotal > 0 (convenience for renderers)
}

// RollupTree is the complete result of a ShowRollup call.
type RollupTree struct {
	SubjectKey      string       `json:"subjectKey"`
	SubjectIssueType string      `json:"subjectIssueType"`
	SubjectSummary  string       `json:"subjectSummary"`
	SubjectRow      RollupRow    `json:"subject"`      // subject's own TT/SP
	Rows            []RollupRow  `json:"rows"`         // L1 aggregate, optionally L2 aggregate
	Nodes           []RollupNode `json:"nodes"`        // L1 children for --list; empty unless requested
	HasDeeperLevel  bool         `json:"hasDeeperLevel"` // any L1 child has children
	MaxFetchedDepth int          `json:"maxFetchedDepth"`
}

// ChildJQL returns the JQL to fetch direct children of key given the subject's issue type and
// configured hierarchy field IDs.
// - Epic / story-level types use "Epic Link" = key
// - Portfolio-level types (Initiative, Feature, Program, Theme) use ParentLink = key
// - Unknown → falls back to "parent = key" (subtasks via the typed parent field)
func ChildJQL(issueType, key, epicLinkField, parentLinkField string) string {
	lower := issueTypeLower(issueType)
	switch {
	case lower == "epic":
		if epicLinkField != "" {
			return `"Epic Link" = ` + key
		}
		return `"Epic Link" = ` + key
	case isPortfolioLevel(lower):
		if parentLinkField != "" {
			return `cf[` + fieldIDNumber(parentLinkField) + `] = ` + key
		}
		// fallback — rarely reached
		return `parent = ` + key
	default:
		return `parent = ` + key
	}
}

// isPortfolioLevel returns true for issue types above Epic in the hierarchy.
func isPortfolioLevel(lower string) bool {
	for _, term := range []string{"initiative", "feature", "program", "theme", "portfolio"} {
		if lower == term {
			return true
		}
	}
	return false
}

func issueTypeLower(t string) string {
	b := make([]byte, len(t))
	for i := 0; i < len(t); i++ {
		c := t[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

// fieldIDNumber extracts the numeric part from "customfield_NNNNN" → "NNNNN"
// so JQL can use cf[NNNNN] syntax which supports = on link-type fields.
func fieldIDNumber(fieldID string) string {
	const prefix = "customfield_"
	if len(fieldID) > len(prefix) && fieldID[:len(prefix)] == prefix {
		return fieldID[len(prefix):]
	}
	return fieldID
}

// IssueTypeHasEpicLinkChildren returns true for issue types whose children are
// linked via "Epic Link" custom field rather than the native parent/subtask
// relationship. In standard Jira DC, only "Epic" is in this category.
func IssueTypeHasEpicLinkChildren(issueType string) bool {
	return issueTypeLower(issueType) == "epic"
}

// AggregateNodes sums TT and SP from a slice of RollupNode into a RollupRow.
func AggregateNodes(nodes []RollupNode, label string, truncated bool) RollupRow {
	row := RollupRow{
		Label:           label,
		TotalCount:      len(nodes),
		Truncated:       truncated,
		IssueTypeCounts: make(map[string]int, 4),
	}
	for _, n := range nodes {
		row.OriginalEstimateSecs += n.OriginalEstimateSecs
		row.RemainingEstimateSecs += n.RemainingEstimateSecs
		row.TimeSpentSecs += n.TimeSpentSecs
		if n.StoryPoints != nil {
			row.StoryPoints += *n.StoryPoints
			row.PointedCount++
		}
		if n.IssueType != "" {
			row.IssueTypeCounts[n.IssueType]++
		}
	}
	if len(row.IssueTypeCounts) == 0 {
		row.IssueTypeCounts = nil
	}
	return row
}

// RollupNodeFromRaw builds a RollupNode from a raw issue.
func RollupNodeFromRaw(raw IssueRaw, spField string) RollupNode {
	n := RollupNode{
		Key:       raw.Key,
		Summary:   raw.Fields.Summary,
		Status:    raw.Fields.Status.Name,
		StatusCategory: raw.Fields.Status.StatusCategory.Name,
		IssueType: raw.Fields.IssueType.Name,
	}
	if raw.Fields.Assignee != nil {
		n.Assignee = raw.Fields.Assignee.DisplayName
	}
	if raw.Fields.TimeTracking != nil {
		tt := raw.Fields.TimeTracking
		n.OriginalEstimateSecs = tt.OriginalEstimateSeconds
		n.RemainingEstimateSecs = tt.RemainingEstimateSeconds
		n.TimeSpentSecs = tt.TimeSpentSeconds
	}
	if spField != "" {
		if spRaw, ok := raw.RawFields[spField]; ok && len(spRaw) > 0 && string(spRaw) != "null" {
			var f float64
			if err := json.Unmarshal(spRaw, &f); err == nil {
				n.StoryPoints = &f
			}
		}
	}
	return n
}

// SubjectRowFromRaw builds the subject's own RollupRow from its raw issue.
func SubjectRowFromRaw(raw IssueRaw, spField string) RollupRow {
	node := RollupNodeFromRaw(raw, spField)
	row := RollupRow{
		Label:                 raw.Fields.IssueType.Name + " " + raw.Key + " (own)",
		OriginalEstimateSecs:  node.OriginalEstimateSecs,
		RemainingEstimateSecs: node.RemainingEstimateSecs,
		TimeSpentSecs:         node.TimeSpentSecs,
		TotalCount:            1,
	}
	if node.StoryPoints != nil {
		row.StoryPoints = *node.StoryPoints
		row.PointedCount = 1
	}
	return row
}

// RollUpChildren is kept for backward-compat with existing tests.
// New code should use AggregateNodes + RollupNodeFromRaw directly.
func RollUpChildren(epicKey string, children []ChildIssueRecord, fullData []IssueRaw, spField string) RolledUpEstimates {
	bucketLabels := []string{"To Do", "In Progress", "Done"}
	buckets := make([]RollupBucket, 3)
	for i, lbl := range bucketLabels {
		buckets[i].Category = lbl
	}

	var unestimated []ChildIssueRecord

	for i, ch := range children {
		idx := bucketIndex(ch.StatusCategory)
		buckets[idx].Children++

		var origSecs, remSecs, spentSecs int64
		var sp *float64

		if i < len(fullData) {
			raw := fullData[i]
			if raw.Fields.TimeTracking != nil {
				tt := raw.Fields.TimeTracking
				origSecs = tt.OriginalEstimateSeconds
				remSecs = tt.RemainingEstimateSeconds
				spentSecs = tt.TimeSpentSeconds
			}
			if spField != "" {
				if spRaw, ok := raw.RawFields[spField]; ok && len(spRaw) > 0 && string(spRaw) != "null" {
					var n float64
					if err := json.Unmarshal(spRaw, &n); err == nil {
						sp = &n
					}
				}
			}
		}

		buckets[idx].OriginalEstimateSecs += origSecs
		buckets[idx].RemainingEstimateSecs += remSecs
		buckets[idx].TimeSpentSecs += spentSecs
		if origSecs > 0 {
			buckets[idx].EstimatedChildren++
		}
		if sp != nil {
			buckets[idx].StoryPoints += *sp
			buckets[idx].PointedChildren++
		}

		if origSecs == 0 {
			unestimated = append(unestimated, ch)
		}
	}

	total := RollupBucket{Category: "Total"}
	for _, b := range buckets {
		total.Children += b.Children
		total.OriginalEstimateSecs += b.OriginalEstimateSecs
		total.RemainingEstimateSecs += b.RemainingEstimateSecs
		total.TimeSpentSecs += b.TimeSpentSecs
		total.StoryPoints += b.StoryPoints
		total.EstimatedChildren += b.EstimatedChildren
		total.PointedChildren += b.PointedChildren
	}

	if unestimated == nil {
		unestimated = []ChildIssueRecord{}
	}

	return RolledUpEstimates{
		EpicKey:       epicKey,
		Total:         total,
		Buckets:       buckets,
		Unestimated:   unestimated,
		TotalChildren: total.Children,
	}
}

// bucketIndex maps a status category name to a bucket index (0=ToDo, 1=InProgress, 2=Done).
func bucketIndex(category string) int {
	switch StatusCategoryRank(category) {
	case 1:
		return 1
	case 2:
		return 2
	default:
		return 0
	}
}

// RolledUpEstimates is kept for backward-compat with existing code.
type RolledUpEstimates struct {
	EpicKey       string             `json:"epicKey"`
	Total         RollupBucket       `json:"total"`
	Buckets       []RollupBucket     `json:"buckets"`
	Unestimated   []ChildIssueRecord `json:"unestimated"`
	Truncated     bool               `json:"truncated"`
	TotalChildren int                `json:"totalChildren"`
}

// RollupBucket holds aggregated estimates for a single status category.
type RollupBucket struct {
	Category              string  `json:"category"`
	Children              int     `json:"children"`
	OriginalEstimateSecs  int64   `json:"originalEstimateSeconds"`
	RemainingEstimateSecs int64   `json:"remainingEstimateSeconds"`
	TimeSpentSecs         int64   `json:"timeSpentSeconds"`
	StoryPoints           float64 `json:"storyPoints"`
	EstimatedChildren     int     `json:"estimatedChildren"`
	PointedChildren       int     `json:"pointedChildren"`
}
