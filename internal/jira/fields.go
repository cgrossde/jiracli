package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cgrossde/jiracli/internal/cache"
)

// ResolveFieldID resolves a human name or canonical id to the Jira field id.
// Accepts both human names ("Story Points") and ids ("customfield_10031").
// Name matching is case-insensitive; exact id match is tried first.
// Returns the canonical id, the full Field record, and any error.
func (c *Client) ResolveFieldID(ctx context.Context, name string, store *cache.Store, noCache bool) (string, Field, error) {
	fields, err := c.ListFields(ctx, store, noCache)
	if err != nil {
		return "", Field{}, err
	}
	// Exact id match first (e.g. "customfield_10031").
	for _, f := range fields {
		if f.ID == name {
			return f.ID, f, nil
		}
	}
	// Case-insensitive name match (e.g. "story points" → "Story Points").
	lower := strings.ToLower(name)
	for _, f := range fields {
		if strings.ToLower(f.Name) == lower {
			return f.ID, f, nil
		}
	}
	return "", Field{}, fmt.Errorf("unknown field %q — run: jiracli lookup fields %s", name, name)
}

// UpdateFields sends a PUT /issue/<KEY> with the given body.
// body should be either {"fields":{...}} or {"update":{...}} or both.
func (c *Client) UpdateFields(ctx context.Context, key string, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal field update: %w", err)
	}
	respBody, status, err := c.Put(ctx, "/issue/"+key, nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return err
	}
	if status != 204 && status != 200 {
		return fmt.Errorf("update fields on %s: %w", key, MapStatus("", status, respBody))
	}
	return nil
}
