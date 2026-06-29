package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cgrossde/jiracli/internal/cache"
)

// CreateLink creates an issue link via POST /issueLink.
// linkType is the link type name (e.g. "Blocks").
// sourceKey is the outward issue; targetKey is the inward issue.
// A successful create returns HTTP 201 with an empty body.
func (c *Client) CreateLink(ctx context.Context, linkType, sourceKey, targetKey string) error {
	payload := map[string]any{
		"type":         map[string]string{"name": linkType},
		"outwardIssue": map[string]string{"key": sourceKey},
		"inwardIssue":  map[string]string{"key": targetKey},
	}
	data, _ := json.Marshal(payload)
	_, status, err := c.Post(ctx, "/issueLink", nil, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	if status != 201 {
		return fmt.Errorf("create link: %w", MapStatus("", status, nil))
	}
	return nil
}

// AggregateLabelsByProject pages through all issues in a project that have
// labels and collects the complete deduplicated label set. Results are sorted
// and cached for TTLLabels (5 min).
// Cache key: "labels/<KEY>".
func (c *Client) AggregateLabelsByProject(ctx context.Context, projectKey string, store *cache.Store, noCache bool) ([]string, error) {
	cacheKey := "labels/" + projectKey
	var cached []string
	if !noCache && store != nil {
		if err := store.Get(cacheKey, TTLLabels, &cached); err == nil {
			return cached, nil
		}
	}

	seen := map[string]struct{}{}
	startAt := 0
	for {
		req := map[string]any{
			"jql":        fmt.Sprintf(`project = %q AND labels IS NOT EMPTY`, projectKey),
			"fields":     []string{"labels"},
			"maxResults": 100,
			"startAt":    startAt,
		}
		data, _ := json.Marshal(req)
		respBody, status, err := c.Post(ctx, "/search", nil, bytes.NewReader(data), "application/json")
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, fmt.Errorf("aggregate labels: %w", MapStatus("", status, respBody))
		}
		var resp struct {
			Total  int `json:"total"`
			Issues []struct {
				Fields struct {
					Labels []string `json:"labels"`
				} `json:"fields"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			return nil, fmt.Errorf("parse label aggregation response: %w", err)
		}
		for _, issue := range resp.Issues {
			for _, l := range issue.Fields.Labels {
				seen[l] = struct{}{}
			}
		}
		startAt += len(resp.Issues)
		if startAt >= resp.Total || len(resp.Issues) == 0 {
			break
		}
	}

	result := make([]string, 0, len(seen))
	for l := range seen {
		result = append(result, l)
	}
	sort.Strings(result)

	if !noCache && store != nil {
		_ = store.Put(cacheKey, result)
	}
	return result, nil
}
