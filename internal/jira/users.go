package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// SearchUsers calls GET /user/search?query=<q>&maxResults=<limit>.
// When activeOnly is true, includeActive=true is added to the query.
// Results are NOT cached — user data changes frequently.
func (c *Client) SearchUsers(ctx context.Context, query string, limit int, activeOnly bool) ([]UserRef, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("maxResults", strconv.Itoa(limit))
	if activeOnly {
		q.Set("includeActive", "true")
	}
	body, status, err := c.Get(ctx, "/user/search", q)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("search users: %w", MapStatus("", status, body))
	}
	var users []UserRef
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("parse users: %w", err)
	}
	return users, nil
}

// SearchAssignableUsers calls GET /user/assignable/search for a project.
// Falls back to SearchUsers on 404 (older DC versions where the endpoint is
// unavailable).
// Results are NOT cached.
func (c *Client) SearchAssignableUsers(ctx context.Context, project, query string, limit int) ([]UserRef, error) {
	q := url.Values{}
	q.Set("project", project)
	q.Set("query", query)
	q.Set("maxResults", strconv.Itoa(limit))
	body, status, err := c.Get(ctx, "/user/assignable/search", q)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		// Older DC versions don't have the assignable search endpoint.
		return c.SearchUsers(ctx, query, limit, false)
	}
	if status != 200 {
		return nil, fmt.Errorf("search assignable users: %w", MapStatus("", status, body))
	}
	var users []UserRef
	if err := json.Unmarshal(body, &users); err != nil {
		return nil, fmt.Errorf("parse assignable users: %w", err)
	}
	return users, nil
}

// Assign sets the assignee of an issue via PUT /issue/<KEY>/assignee.
// name="" sends {"name":null} to unassign.
func (c *Client) Assign(ctx context.Context, key, name string) error {
	var payload any
	if name == "" {
		payload = map[string]any{"name": nil}
	} else {
		payload = map[string]string{"name": name}
	}
	raw, _ := json.Marshal(payload)
	respBody, status, err := c.Put(ctx, "/issue/"+key+"/assignee", nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return err
	}
	if status != 204 && status != 200 && status != 201 {
		return fmt.Errorf("assign %s: %w", key, MapStatus("", status, respBody))
	}
	return nil
}
