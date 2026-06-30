package jira

import "encoding/json"

// RollupBucket holds aggregated estimates for a single status category.
type RollupBucket struct {
	Category              string  `json:"category"`
	Children              int     `json:"children"`
	OriginalEstimateSecs  int64   `json:"originalEstimateSeconds"`
	RemainingEstimateSecs int64   `json:"remainingEstimateSeconds"`
	TimeSpentSecs         int64   `json:"timeSpentSeconds"`
	StoryPoints           float64 `json:"storyPoints"`
	EstimatedChildren     int     `json:"estimatedChildren"` // children with originalEstimate > 0
	PointedChildren       int     `json:"pointedChildren"`   // children with storyPoints != nil
}

// RolledUpEstimates is the result of RollUpChildren.
type RolledUpEstimates struct {
	EpicKey       string             `json:"epicKey"`
	Total         RollupBucket       `json:"total"`
	Buckets       []RollupBucket     `json:"buckets"` // in order: To Do, In Progress, Done
	Unestimated   []ChildIssueRecord `json:"unestimated"`
	Truncated     bool               `json:"truncated"`
	TotalChildren int                `json:"totalChildren"`
}

// bucketIndex maps a status category name to a bucket index (0=ToDo, 1=InProgress, 2=Done).
// Unknown categories fall to 0 (To Do).
func bucketIndex(category string) int {
	switch StatusCategoryRank(category) {
	case 1:
		return 1 // In Progress
	case 2:
		return 2 // Done
	default:
		return 0 // To Do (rank 0 = unknown or todo)
	}
}

// RollUpChildren aggregates time and story-point estimates across children.
// children provides the compact records for listing; fullData provides the raw issues
// for time and story-points access. Both slices must be in the same order.
// spField is the Story Points custom field ID (empty = disabled).
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

		// Unestimated: no original time estimate (SP alone doesn't count)
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
