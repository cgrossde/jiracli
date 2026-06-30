package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// SearchRequest is the POST body for /rest/api/2/search.
type SearchRequest struct {
	JQL        string   `json:"jql"`
	StartAt    int      `json:"startAt"`
	MaxResults int      `json:"maxResults"`
	Fields     []string `json:"fields"`
}

// SearchResponse is the raw /search response.
type SearchResponse struct {
	Total      int        `json:"total"`
	StartAt    int        `json:"startAt"`
	MaxResults int        `json:"maxResults"`
	Issues     []IssueRaw `json:"issues"`
}

// Search executes a JQL search via POST /rest/api/2/search.
// page is 1-indexed; limit is capped at 100.
func (c *Client) Search(ctx context.Context, jql string, page, limit int, fields []string) (SearchResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	startAt := (page - 1) * limit
	req := SearchRequest{
		JQL:        jql,
		StartAt:    startAt,
		MaxResults: limit,
		Fields:     fields,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return SearchResponse{}, err
	}
	respBody, status, err := c.Post(ctx, "/search", url.Values{}, bytes.NewReader(body), "application/json")
	if err != nil {
		return SearchResponse{}, err
	}
	if status != 200 {
		return SearchResponse{}, fmt.Errorf("search: %w", MapStatus("", status, respBody))
	}
	var resp SearchResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return SearchResponse{}, fmt.Errorf("parse search response: %w", err)
	}
	return resp, nil
}

// SearchIssueRecord is the NDJSON shape for a single issue in a search result.
type SearchIssueRecord struct {
	Key            string              `json:"key"`
	Summary        string              `json:"summary"`
	Description    string              `json:"description,omitempty"`
	Status         string              `json:"status"`
	StatusCategory string              `json:"statusCategory"`
	Assignee       *IssueUserRef       `json:"assignee"`
	Reporter       *IssueUserRef       `json:"reporter,omitempty"`
	Priority       string              `json:"priority"`
	IssueType      string              `json:"issueType"`
	Updated        string              `json:"updated"`
	Labels         []string            `json:"labels"`
	Components     []string            `json:"components"`
	FixVersions    []string            `json:"fixVersions,omitempty"`
	TimeTracking   *TimeTrackingRecord `json:"timetracking,omitempty"`
	StoryPoints    *float64            `json:"storyPoints,omitempty"`
}

// SearchPaginationTrailer is emitted as the final NDJSON line when more pages exist.
type SearchPaginationTrailer struct {
	Pagination struct {
		Page     int  `json:"page"`
		Pages    int  `json:"pages"`
		Total    int  `json:"total"`
		NextPage int  `json:"next_page"`
		HasMore  bool `json:"has_more"`
	} `json:"_pagination"`
}

// ToSearchRecord maps an IssueRaw to a SearchIssueRecord.
// spField is the instance-specific Story Points custom field ID (empty = disabled).
func ToSearchRecord(raw IssueRaw, spField string) SearchIssueRecord {
	rec := SearchIssueRecord{
		Key:            raw.Key,
		Summary:        raw.Fields.Summary,
		Description:    raw.Fields.Description,
		Status:         raw.Fields.Status.Name,
		StatusCategory: raw.Fields.Status.StatusCategory.Name,
		IssueType:      raw.Fields.IssueType.Name,
		Updated:        raw.Fields.Updated,
		Labels:         raw.Fields.Labels,
	}

	if rec.Labels == nil {
		rec.Labels = []string{}
	}

	if raw.Fields.Priority != nil {
		rec.Priority = raw.Fields.Priority.Name
	}

	if raw.Fields.Assignee != nil {
		rec.Assignee = &IssueUserRef{
			Name:        raw.Fields.Assignee.Name,
			DisplayName: raw.Fields.Assignee.DisplayName,
		}
	}

	components := make([]string, 0, len(raw.Fields.Components))
	for _, c := range raw.Fields.Components {
		components = append(components, c.Name)
	}
	rec.Components = components

	if raw.Fields.Reporter != nil {
		rec.Reporter = &IssueUserRef{
			Name:        raw.Fields.Reporter.Name,
			DisplayName: raw.Fields.Reporter.DisplayName,
		}
	}

	fixVersions := make([]string, 0, len(raw.Fields.FixVersions))
	for _, fv := range raw.Fields.FixVersions {
		fixVersions = append(fixVersions, fv.Name)
	}
	if len(fixVersions) > 0 {
		rec.FixVersions = fixVersions
	}

	// Time tracking
	if raw.Fields.TimeTracking != nil {
		tt := raw.Fields.TimeTracking
		if tt.OriginalEstimateSeconds != 0 || tt.RemainingEstimateSeconds != 0 || tt.TimeSpentSeconds != 0 {
			rec.TimeTracking = &TimeTrackingRecord{
				OriginalEstimateSeconds:  tt.OriginalEstimateSeconds,
				RemainingEstimateSeconds: tt.RemainingEstimateSeconds,
				TimeSpentSeconds:         tt.TimeSpentSeconds,
			}
		}
	}

	// Story Points — dynamic custom field
	if spField != "" {
		if spRaw, ok := raw.RawFields[spField]; ok && len(spRaw) > 0 && string(spRaw) != "null" {
			var n float64
			if err := json.Unmarshal(spRaw, &n); err == nil {
				rec.StoryPoints = &n
			}
		}
	}

	return rec
}
