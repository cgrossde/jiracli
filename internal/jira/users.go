package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SearchUsers calls GET /user/search?query=<q>&maxResults=<limit>.
// When activeOnly is true, includeActive=true is added to the query.
// Results are NOT cached — user data changes frequently.
//
// Some Jira Data Center versions predate the `query` parameter and still
// require the legacy `username` parameter; both are sent so either server
// generation is satisfied. When query is empty, `.` is used as a
// best-effort wildcard (works for the common case of email-style
// usernames, which virtually always contain a dot) so that "list all
// users" still returns something instead of a hard 400.
func (c *Client) SearchUsers(ctx context.Context, query string, limit int, activeOnly bool) ([]UserRef, error) {
	effective := query
	if effective == "" {
		effective = "."
	}
	q := url.Values{}
	q.Set("query", effective)
	q.Set("username", effective)
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
//
// Jira's own `query` filtering on this endpoint is unreliable on some DC
// versions — it can silently return an empty or unrelated set even when a
// genuine substring match exists elsewhere in the assignable pool. To
// compensate, this always fetches a generous, mostly-unfiltered candidate
// pool and applies FilterUsersByQuery client-side; if the server-filtered
// call comes back empty for a non-empty query, it retries once without the
// query param before giving up.
func (c *Client) SearchAssignableUsers(ctx context.Context, project, query string, limit int) ([]UserRef, error) {
	fetchLimit := limit
	if fetchLimit < 200 {
		fetchLimit = 200
	}
	users, err := c.fetchAssignableUsers(ctx, project, query, fetchLimit)
	if err != nil {
		return nil, err
	}
	if query != "" {
		filtered := FilterUsersByQuery(users, query)
		if len(filtered) == 0 && len(users) > 0 {
			// Server-side query filtering may have over-narrowed the
			// result set; re-fetch the broader pool and filter ourselves.
			broad, err2 := c.fetchAssignableUsers(ctx, project, "", fetchLimit)
			if err2 == nil {
				filtered = FilterUsersByQuery(broad, query)
			}
		}
		users = filtered
	}
	if len(users) > limit {
		users = users[:limit]
	}
	return users, nil
}

func (c *Client) fetchAssignableUsers(ctx context.Context, project, query string, maxResults int) ([]UserRef, error) {
	q := url.Values{}
	q.Set("project", project)
	if query != "" {
		q.Set("query", query)
	}
	q.Set("maxResults", strconv.Itoa(maxResults))
	body, status, err := c.Get(ctx, "/user/assignable/search", q)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		// Older DC versions don't have the assignable search endpoint.
		return c.SearchUsers(ctx, query, maxResults, false)
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

// FilterUsersByQuery applies a case-insensitive substring match against
// each user's Name, DisplayName, and EmailAddress. An empty query returns
// users unchanged. Jira's own server-side `query`/`username` filtering is
// treated as a hint at best (it varies by DC version and can be
// unreliable), so every caller that accepts a user query should re-apply
// this filter rather than trusting the raw server response.
func FilterUsersByQuery(users []UserRef, query string) []UserRef {
	if query == "" {
		return users
	}
	ql := strings.ToLower(query)
	filtered := make([]UserRef, 0, len(users))
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), ql) ||
			strings.Contains(strings.ToLower(u.DisplayName), ql) ||
			strings.Contains(strings.ToLower(u.EmailAddress), ql) {
			filtered = append(filtered, u)
		}
	}
	return filtered
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
