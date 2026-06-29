package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// ChangelogAuthor is the author sub-object on a changelog entry.
type ChangelogAuthor struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// ChangelogItem describes one field change within a changelog entry.
type ChangelogItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

// ChangelogEntry is a single Jira changelog history record.
type ChangelogEntry struct {
	ID      string          `json:"id"`
	Author  ChangelogAuthor `json:"author"`
	Created string          `json:"created"`
	Items   []ChangelogItem `json:"items"`
}

// ChangelogResponse is the paginated response from /issue/<KEY>/changelog.
// The Values field is populated for the dedicated endpoint; the Histories
// field is populated when using the embedded fallback (expand=changelog).
type ChangelogResponse struct {
	StartAt    int              `json:"startAt"`
	MaxResults int              `json:"maxResults"`
	Total      int              `json:"total"`
	Values     []ChangelogEntry `json:"values"`
	// Histories is used in the fallback embedded-changelog shape.
	Histories []ChangelogEntry `json:"histories"`
}

// GetChangelog fetches changelog entries for an issue. page is 1-indexed.
// It first tries the dedicated /changelog endpoint (Jira DC 8.7+); on 404 it
// falls back to expand=changelog on the issue endpoint.
//
// The second return value (wasTruncated) is true only on the fallback path
// when the server returned fewer entries than the reported total (the embedded
// changelog is bounded by the server's maxResults, typically 100).
func (c *Client) GetChangelog(ctx context.Context, key string, page, limit int) (ChangelogResponse, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	startAt := (page - 1) * limit

	q := url.Values{}
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(limit))

	body, status, err := c.Get(ctx, "/issue/"+key+"/changelog", q)
	if err != nil {
		return ChangelogResponse{}, false, err
	}
	if status == 404 {
		// Older DC version — fall back to expand=changelog on the issue.
		return c.getChangelogFallback(ctx, key)
	}
	if status != 200 {
		return ChangelogResponse{}, false, fmt.Errorf("get changelog %s: %w", key, MapStatus("", status, body))
	}

	var resp ChangelogResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ChangelogResponse{}, false, fmt.Errorf("parse changelog: %w", err)
	}
	return resp, false, nil
}

// getChangelogFallback fetches changelog data via expand=changelog on the
// issue endpoint. Used for Jira DC versions that predate the dedicated
// /changelog endpoint.
func (c *Client) getChangelogFallback(ctx context.Context, key string) (ChangelogResponse, bool, error) {
	q := url.Values{}
	q.Set("fields", "key")
	q.Set("expand", "changelog")

	body, status, err := c.Get(ctx, "/issue/"+key, q)
	if err != nil {
		return ChangelogResponse{}, false, err
	}
	if status != 200 {
		return ChangelogResponse{}, false, fmt.Errorf("get issue changelog fallback %s: %w", key, MapStatus("", status, body))
	}

	var raw struct {
		Changelog struct {
			MaxResults int              `json:"maxResults"`
			Total      int              `json:"total"`
			Histories  []ChangelogEntry `json:"histories"`
		} `json:"changelog"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ChangelogResponse{}, false, fmt.Errorf("parse fallback changelog: %w", err)
	}

	truncated := len(raw.Changelog.Histories) < raw.Changelog.Total
	return ChangelogResponse{
		StartAt:    0,
		MaxResults: raw.Changelog.MaxResults,
		Total:      raw.Changelog.Total,
		Values:     raw.Changelog.Histories,
	}, truncated, nil
}
