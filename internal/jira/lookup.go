package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cgrossde/jiracli/internal/cache"
)

const (
	TTLMyself       = 24 * time.Hour
	TTLFields       = 24 * time.Hour
	TTLProjects     = 24 * time.Hour
	TTLProject      = 1 * time.Hour
	TTLCreateMeta   = 24 * time.Hour
	TTLLinkTypes    = 24 * time.Hour
	TTLPriorities   = 24 * time.Hour
	TTLPrioritySchm = 24 * time.Hour
	TTLLabels       = 5 * time.Minute
	TTLStatuses     = 24 * time.Hour
	TTLIssueTypes   = 24 * time.Hour
)

// UserRef is a lightweight Jira user reference used in lookup results.
type UserRef struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
}

// Component is a Jira project component.
type Component struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Lead        *UserRef `json:"lead,omitempty"`
}

// Version is a Jira project version (fix version / affects version).
type Version struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
	Description string `json:"description"`
}

// Priority is a Jira issue priority.
type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LinkType is a Jira issue link type.
type LinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// Status is a Jira issue status with its category.
type Status struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StatusCategory struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"statusCategory"`
}

// IssueType is a Jira issue type (Bug, Story, Task, etc.).
type IssueType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Subtask     bool   `json:"subtask"`
}

// Field is a Jira field definition (built-in or custom).
type Field struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Schema *struct {
		Type  string `json:"type"`
		Items string `json:"items,omitempty"`
	} `json:"schema,omitempty"`
}

// Project is a Jira project with its metadata.
type Project struct {
	ID         string      `json:"id"`
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	Lead       *UserRef    `json:"lead,omitempty"`
	Components []Component `json:"components"`
	Versions   []Version   `json:"versions"`
	IssueTypes []IssueType `json:"issueTypes"`
}

// CreateMetaField describes a single field in a create-meta response.
type CreateMetaField struct {
	FieldID       string `json:"fieldId"`
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	AllowedValues []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Value string `json:"value,omitempty"`
	} `json:"allowedValues,omitempty"`
	Schema *struct {
		Type  string `json:"type"`
		Items string `json:"items,omitempty"`
	} `json:"schema,omitempty"`
}

// CreateMeta holds the field metadata for a particular project+type combination.
type CreateMeta struct {
	Fields []CreateMetaField
}

// ListStatuses fetches all issue statuses from /status.
// Cache key: "statuses", TTL: 24h.
func (c *Client) ListStatuses(ctx context.Context, store *cache.Store, noCache bool) ([]Status, error) {
	var result []Status
	if !noCache && store != nil {
		if err := store.Get("statuses", TTLStatuses, &result); err == nil {
			return result, nil
		}
	}
	body, status, err := c.Get(ctx, "/status", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list statuses: %w", MapStatus("", status, body))
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse statuses: %w", err)
	}
	if !noCache && store != nil {
		_ = store.Put("statuses", result)
	}
	return result, nil
}

// ListIssueTypes fetches all global issue types from /issuetype.
// Cache key: "issuetypes", TTL: 24h.
func (c *Client) ListIssueTypes(ctx context.Context, store *cache.Store, noCache bool) ([]IssueType, error) {
	var result []IssueType
	if !noCache && store != nil {
		if err := store.Get("issuetypes", TTLIssueTypes, &result); err == nil {
			return result, nil
		}
	}
	body, status, err := c.Get(ctx, "/issuetype", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list issue types: %w", MapStatus("", status, body))
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse issue types: %w", err)
	}
	if !noCache && store != nil {
		_ = store.Put("issuetypes", result)
	}
	return result, nil
}

// ListPriorities fetches all global priorities from /priority.
// Cache key: "priorities", TTL: 24h.
func (c *Client) ListPriorities(ctx context.Context, store *cache.Store, noCache bool) ([]Priority, error) {
	var result []Priority
	if !noCache && store != nil {
		if err := store.Get("priorities", TTLPriorities, &result); err == nil {
			return result, nil
		}
	}
	body, status, err := c.Get(ctx, "/priority", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list priorities: %w", MapStatus("", status, body))
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse priorities: %w", err)
	}
	if !noCache && store != nil {
		_ = store.Put("priorities", result)
	}
	return result, nil
}

// ListFields fetches all field definitions from /field.
// Cache key: "fields", TTL: 24h.
func (c *Client) ListFields(ctx context.Context, store *cache.Store, noCache bool) ([]Field, error) {
	var result []Field
	if !noCache && store != nil {
		if err := store.Get("fields", TTLFields, &result); err == nil {
			return result, nil
		}
	}
	body, status, err := c.Get(ctx, "/field", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list fields: %w", MapStatus("", status, body))
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse fields: %w", err)
	}
	if !noCache && store != nil {
		_ = store.Put("fields", result)
	}
	return result, nil
}

// ListLinkTypes fetches all issue link types from /issueLinkType.
// Cache key: "linktypes", TTL: 24h.
func (c *Client) ListLinkTypes(ctx context.Context, store *cache.Store, noCache bool) ([]LinkType, error) {
	var result []LinkType
	if !noCache && store != nil {
		if err := store.Get("linktypes", TTLLinkTypes, &result); err == nil {
			return result, nil
		}
	}
	body, status, err := c.Get(ctx, "/issueLinkType", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list link types: %w", MapStatus("", status, body))
	}
	var envelope struct {
		IssueLinkTypes []LinkType `json:"issueLinkTypes"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse link types: %w", err)
	}
	result = envelope.IssueLinkTypes
	if !noCache && store != nil {
		_ = store.Put("linktypes", result)
	}
	return result, nil
}

// SuggestLabels queries the JQL autocomplete endpoint for label suggestions.
// Returns the matching labels and a truncated flag that is true when the
// server cap of 15 results was reached.
// Results are NOT cached — autocomplete is inherently real-time.
func (c *Client) SuggestLabels(ctx context.Context, query string) ([]string, bool, error) {
	q := url.Values{}
	q.Set("fieldName", "labels")
	q.Set("fieldValue", query)
	body, status, err := c.Get(ctx, "/jql/autocompletedata/suggestions", q)
	if err != nil {
		return nil, false, err
	}
	if status != 200 {
		return nil, false, fmt.Errorf("suggest labels: %w", MapStatus("", status, body))
	}
	var resp struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("parse label suggestions: %w", err)
	}
	labels := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		labels[i] = r.Value
	}
	// The Jira autocomplete endpoint caps results at 15.
	truncated := len(labels) == 15
	return labels, truncated, nil
}
// portfolioSeedTerms are case-insensitive substring matches for the candidate
// field NAME. A field matches when its name contains any seed term and does
// not contain any deny term.
var portfolioSeedTerms = []string{"initiative", "program", "programme", "feature", "theme", "portfolio"}

// portfolioDenyTerms are whole-word matches (split on non-letter chars) against
// the field NAME, used to discard noisy candidates.
var portfolioDenyTerms = []string{"sarb", "score", "sap", "ucr", "legacy", "api", "investment"}

// PortfolioCandidates filters a field list to plausible portfolio-level fields.
// Excludes fields already assigned to Epic Link or Parent Link, retains only
// custom fields with string-shaped schema (or unknown schema), and applies
// the seed/deny term rules above.
func PortfolioCandidates(all []Field, epicLinkID, parentLinkID string) []Field {
	out := make([]Field, 0, 8)
	for _, f := range all {
		if !f.Custom {
			continue
		}
		if f.ID == epicLinkID || f.ID == parentLinkID {
			continue
		}
		if f.Schema != nil {
			switch f.Schema.Type {
			case "string", "any", "":
			default:
				continue
			}
		}
		nameLower := strings.ToLower(f.Name)
		seeded := false
		for _, s := range portfolioSeedTerms {
			if strings.Contains(nameLower, s) {
				seeded = true
				break
			}
		}
		if !seeded {
			continue
		}
		words := splitWords(nameLower)
		denied := false
		for _, d := range portfolioDenyTerms {
			for _, w := range words {
				if w == d {
					denied = true
					break
				}
			}
			if denied {
				break
			}
		}
		if denied {
			continue
		}
		out = append(out, f)
	}
	return out
}

// splitWords lowercases s and splits on any non-letter rune.
func splitWords(s string) []string {
	var out []string
	cur := make([]rune, 0, 16)
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			cur = append(cur, r)
			continue
		}
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}
