package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// CreateIssue POSTs /issue and returns the new issue key.
func (c *Client) CreateIssue(ctx context.Context, body map[string]any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal create body: %w", err)
	}
	respBody, status, err := c.Post(ctx, "/issue", nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", fmt.Errorf("create issue: %w", MapStatus("", status, respBody))
	}
	var resp struct {
		Key string `json:"key"`
	}
	json.Unmarshal(respBody, &resp) //nolint:errcheck
	return resp.Key, nil
}
