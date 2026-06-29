package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// TransitionRaw is the shape of one transition item returned by the Jira
// /rest/api/2/issue/<KEY>/transitions endpoint.
type TransitionRaw struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   struct {
		Name           string `json:"name"`
		StatusCategory struct {
			Name string `json:"name"`
		} `json:"statusCategory"`
	} `json:"to"`
}

// TransitionsResponse wraps the transitions array from the API.
type TransitionsResponse struct {
	Transitions []TransitionRaw `json:"transitions"`
}

// GetTransitions fetches the available workflow transitions for an issue.
// The expand=transitions.fields parameter is included so the server returns
// the fields schema for each transition (used by the write path in Phase 4).
func (c *Client) GetTransitions(ctx context.Context, key string) ([]TransitionRaw, error) {
	q := url.Values{}
	q.Set("expand", "transitions.fields")
	body, status, err := c.Get(ctx, "/issue/"+key+"/transitions", q)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("get transitions %s: %w", key, MapStatus("", status, body))
	}
	var resp TransitionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse transitions: %w", err)
	}
	return resp.Transitions, nil
}

// DoTransition POSTs a workflow transition on an issue.
// comment is optional; if non-empty, it is posted atomically via the update block.
func (c *Client) DoTransition(ctx context.Context, key, transitionID, comment string) error {
	body := map[string]any{
		"transition": map[string]string{"id": transitionID},
	}
	if comment != "" {
		body["update"] = map[string]any{
			"comment": []map[string]any{
				{"add": map[string]string{"body": comment}},
			},
		}
	}
	raw, _ := json.Marshal(body)
	respBody, status, err := c.Post(ctx, "/issue/"+key+"/transitions", nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return err
	}
	// Jira returns 204 on success
	if status != 204 && status != 200 {
		return fmt.Errorf("transition %s: %w", key, MapStatus("", status, respBody))
	}
	return nil
}
