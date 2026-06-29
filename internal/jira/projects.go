package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/cgrossde/jiracli/internal/cache"
)

// ListProjects fetches all projects from /project.
// Cache key: "projects", TTL: 24h.
func (c *Client) ListProjects(ctx context.Context, store *cache.Store, noCache bool) ([]Project, error) {
	var result []Project
	if !noCache && store != nil {
		if err := store.Get("projects", TTLProjects, &result); err == nil {
			return result, nil
		}
	}

	body, status, err := c.Get(ctx, "/project", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list projects: %w", MapStatus("", status, body))
	}

	// Jira returns a lighter-weight list object; we inflate it to []Project.
	var raw []struct {
		ID   string   `json:"id"`
		Key  string   `json:"key"`
		Name string   `json:"name"`
		Lead *UserRef `json:"lead"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse projects: %w", err)
	}
	result = make([]Project, len(raw))
	for i, r := range raw {
		result[i] = Project{
			ID:   r.ID,
			Key:  r.Key,
			Name: r.Name,
			Lead: r.Lead,
		}
	}
	if !noCache && store != nil {
		_ = store.Put("projects", result)
	}
	return result, nil
}

// GetProject fetches a single project by key, expanding components, versions,
// lead, and issue types.
// Cache key: "project/<KEY>", TTL: 1h.
func (c *Client) GetProject(ctx context.Context, key string, store *cache.Store, noCache bool) (Project, error) {
	cacheKey := "project/" + key
	var result Project
	if !noCache && store != nil {
		if err := store.Get(cacheKey, TTLProject, &result); err == nil {
			return result, nil
		}
	}

	q := url.Values{}
	q.Set("expand", "components,versions,lead,issueTypes")
	body, status, err := c.Get(ctx, "/project/"+key, q)
	if err != nil {
		return Project{}, err
	}
	if status != 200 {
		return Project{}, fmt.Errorf("get project %s: %w", key, MapStatus("", status, body))
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return Project{}, fmt.Errorf("parse project: %w", err)
	}
	if !noCache && store != nil {
		_ = store.Put(cacheKey, result)
	}
	return result, nil
}

// ListProjectIssueTypes fetches issue types available for a project via the
// create-meta endpoint. It handles both DC response shapes:
//   - {"issueTypes":[...]}  (older DC)
//   - {"values":[...]}      (newer DC)
//
// Cache key: "issuetypes/<KEY>", TTL: 24h.
func (c *Client) ListProjectIssueTypes(ctx context.Context, projectKey string, store *cache.Store, noCache bool) ([]IssueType, error) {
	cacheKey := "issuetypes/" + projectKey
	var result []IssueType
	if !noCache && store != nil {
		if err := store.Get(cacheKey, TTLIssueTypes, &result); err == nil {
			return result, nil
		}
	}

	body, status, err := c.Get(ctx, "/issue/createmeta/"+projectKey+"/issuetypes", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("list project issue types %s: %w", projectKey, MapStatus("", status, body))
	}

	// Try both DC response envelope shapes.
	var envelope struct {
		IssueTypes []IssueType `json:"issueTypes"`
		Values     []IssueType `json:"values"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parse project issue types: %w", err)
	}
	if len(envelope.IssueTypes) > 0 {
		result = envelope.IssueTypes
	} else {
		result = envelope.Values
	}
	if !noCache && store != nil {
		_ = store.Put(cacheKey, result)
	}
	return result, nil
}

// GetCreateMeta fetches the field metadata for a specific project+issue type
// combination. The response shape is {"fields":[...]}.
// Cache key: "createmeta/<KEY>/<typeID>", TTL: 24h.
func (c *Client) GetCreateMeta(ctx context.Context, projectKey, typeID string, store *cache.Store, noCache bool) (CreateMeta, error) {
	cacheKey := "createmeta/" + projectKey + "/" + typeID
	var result CreateMeta
	if !noCache && store != nil {
		if err := store.Get(cacheKey, TTLCreateMeta, &result); err == nil {
			return result, nil
		}
	}

	body, status, err := c.Get(ctx, "/issue/createmeta/"+projectKey+"/issuetypes/"+typeID, nil)
	if err != nil {
		return CreateMeta{}, err
	}
	if status != 200 {
		return CreateMeta{}, fmt.Errorf("get create meta %s/%s: %w", projectKey, typeID, MapStatus("", status, body))
	}

	var envelope struct {
		Fields []CreateMetaField `json:"fields"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return CreateMeta{}, fmt.Errorf("parse create meta: %w", err)
	}
	result = CreateMeta{Fields: envelope.Fields}
	if !noCache && store != nil {
		_ = store.Put(cacheKey, result)
	}
	return result, nil
}

// ListProjectPriorities resolves the priority scheme for a project (DC 7.7+)
// and returns its priorities. Falls back to the global priority list when the
// priority-scheme endpoint returns 404.
// Cache key: "project/<KEY>/priorityscheme", TTL: 24h.
// Returns (priorities, fallbackUsed, error).
func (c *Client) ListProjectPriorities(ctx context.Context, projectKey string, store *cache.Store, noCache bool) ([]Priority, bool, error) {
	cacheKey := "project/" + projectKey + "/priorityscheme"
	var result []Priority
	if !noCache && store != nil {
		if err := store.Get(cacheKey, TTLPrioritySchm, &result); err == nil {
			return result, false, nil
		}
	}

	// Step 1: get the priority scheme id for this project.
	schemeBody, schemeStatus, err := c.Get(ctx, "/project/"+projectKey+"/priorityscheme", nil)
	if err != nil {
		return nil, false, err
	}
	if schemeStatus == 404 {
		// DC version too old or feature not enabled — fall back to global list.
		priorities, err := c.ListPriorities(ctx, store, noCache)
		return priorities, true, err
	}
	if schemeStatus != 200 {
		return nil, false, fmt.Errorf("get priority scheme for %s: %w", projectKey, MapStatus("", schemeStatus, schemeBody))
	}

	var scheme struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal(schemeBody, &scheme); err != nil {
		return nil, false, fmt.Errorf("parse priority scheme: %w", err)
	}

	// Step 2: fetch the priorities for that scheme.
	priBody, priStatus, err := c.Get(ctx, "/priorityscheme/"+scheme.ID.String()+"/priorities", nil)
	if err != nil {
		return nil, false, err
	}
	if priStatus == 404 {
		// Scheme found but priorities endpoint unavailable — fall back.
		priorities, err := c.ListPriorities(ctx, store, noCache)
		return priorities, true, err
	}
	if priStatus != 200 {
		return nil, false, fmt.Errorf("get scheme priorities: %w", MapStatus("", priStatus, priBody))
	}

	var envelope struct {
		Priorities []Priority `json:"priorities"`
	}
	if err := json.Unmarshal(priBody, &envelope); err != nil {
		return nil, false, fmt.Errorf("parse scheme priorities: %w", err)
	}
	result = envelope.Priorities
	if !noCache && store != nil {
		_ = store.Put(cacheKey, result)
	}
	return result, false, nil
}
