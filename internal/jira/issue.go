package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cgrossde/jiracli/internal/cache"
)

// DefaultIssueFields is the lean field list fetched by default.
const DefaultIssueFields = "key,summary,status,assignee,reporter,description,labels,components,priority,issuetype,created,updated,comment,fixVersions,parent,issuelinks,attachment,resolution,timetracking"

// IssueRaw is the raw Jira API response for a single issue.
// RawFields holds the complete fields map as raw JSON so callers can
// read dynamic custom fields (e.g. Parent Link, Epic Link) without a
// second HTTP request or a type assertion.
type IssueRaw struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Resolution  *struct {
			Name string `json:"name"`
		} `json:"resolution"`
		Status struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			StatusCategory struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"statusCategory"`
		} `json:"status"`
		Priority *struct {
			Name string `json:"name"`
		} `json:"priority"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Assignee *struct {
			Name         string `json:"name"`
			DisplayName  string `json:"displayName"`
			EmailAddress string `json:"emailAddress"`
		} `json:"assignee"`
		Reporter *struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"reporter"`
		Created    string   `json:"created"`
		Updated    string   `json:"updated"`
		Labels     []string `json:"labels"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
		FixVersions []struct {
			Name string `json:"name"`
		} `json:"fixVersions"`
		Parent *struct {
			Key    string `json:"key"`
			Fields struct {
				Summary   string `json:"summary"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
				Status struct {
					Name           string `json:"name"`
					StatusCategory struct {
						Name string `json:"name"`
					} `json:"statusCategory"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"parent"`
		IssueLinks []struct {
			ID   string `json:"id"`
			Type struct {
				Name    string `json:"name"`
				Inward  string `json:"inward"`
				Outward string `json:"outward"`
			} `json:"type"`
			InwardIssue *struct {
				Key    string `json:"key"`
				Fields struct {
					Summary   string `json:"summary"`
					IssueType struct {
						Name string `json:"name"`
					} `json:"issuetype"`
					Status struct {
						Name           string `json:"name"`
						StatusCategory struct {
							Name string `json:"name"`
						} `json:"statusCategory"`
					} `json:"status"`
				} `json:"fields"`
			} `json:"inwardIssue"`
			OutwardIssue *struct {
				Key    string `json:"key"`
				Fields struct {
					Summary   string `json:"summary"`
					IssueType struct {
						Name string `json:"name"`
					} `json:"issuetype"`
					Status struct {
						Name           string `json:"name"`
						StatusCategory struct {
							Name string `json:"name"`
						} `json:"statusCategory"`
					} `json:"status"`
				} `json:"fields"`
			} `json:"outwardIssue"`
		} `json:"issuelinks"`
		Attachment []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			MimeType string `json:"mimeType"`
			Size     int64  `json:"size"`
			Created  string `json:"created"`
			Author   struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Content string `json:"content"` // download URL
		} `json:"attachment"`
		Subtasks []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name           string `json:"name"`
					StatusCategory struct {
						Name string `json:"name"`
					} `json:"statusCategory"`
				} `json:"status"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
				Assignee *struct {
					DisplayName string `json:"displayName"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"subtasks"`
		Comment struct {
			Total    int `json:"total"`
			Comments []struct {
				ID     string `json:"id"`
				Author struct {
					Name        string `json:"name"`
					DisplayName string `json:"displayName"`
				} `json:"author"`
				Created string `json:"created"`
				Updated string `json:"updated"`
				Body    string `json:"body"`
			} `json:"comments"`
		} `json:"comment"`
		TimeTracking *struct {
			OriginalEstimateSeconds  int64 `json:"originalEstimateSeconds"`
			RemainingEstimateSeconds int64 `json:"remainingEstimateSeconds"`
			TimeSpentSeconds         int64 `json:"timeSpentSeconds"`
		} `json:"timetracking"`
		Project struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"project"`
	} `json:"fields"`
	// RawFields is the complete "fields" object as a key→raw-JSON map.
	// Used to read dynamic custom fields not captured by the typed Fields struct.
	RawFields map[string]json.RawMessage `json:"-"`
	Changelog *struct {
		Total     int `json:"total"`
		Histories []struct {
			ID     string `json:"id"`
			Author struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Created string `json:"created"`
			Items   []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			} `json:"items"`
		} `json:"histories"`
	} `json:"changelog"`
}

// UnmarshalJSON implements json.Unmarshaler for IssueRaw so that both the
// typed Fields struct and the RawFields map are populated in a single decode.
func (r *IssueRaw) UnmarshalJSON(data []byte) error {
	// Use a type alias to avoid infinite recursion.
	type issueRawAlias IssueRaw
	if err := json.Unmarshal(data, (*issueRawAlias)(r)); err != nil {
		return err
	}
	// Extract the raw fields map for custom-field access.
	var envelope struct {
		Fields map[string]json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil {
		r.RawFields = envelope.Fields
	}
	return nil
}

// GetIssue fetches a single issue. fields is the comma-joined field list;
// pass DefaultIssueFields if empty. expandChangelog=true adds &expand=changelog.
// Dynamic custom fields are accessible via the returned IssueRaw.RawFields map.
func (c *Client) GetIssue(ctx context.Context, key string, fields string, expandChangelog bool) (IssueRaw, error) {
	if fields == "" {
		fields = DefaultIssueFields
	}
	q := url.Values{}
	q.Set("fields", fields)
	if expandChangelog {
		q.Set("expand", "changelog")
	}
	body, status, err := c.Get(ctx, "/issue/"+key, q)
	if err != nil {
		return IssueRaw{}, err
	}
	if status != 200 {
		return IssueRaw{}, fmt.Errorf("fetch issue %s: %w", key, MapStatus("", status, body))
	}
	var raw IssueRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return IssueRaw{}, fmt.Errorf("parse issue %s: %w", key, err)
	}
	return raw, nil
}

// ---------------------------------------------------------------------------
// NDJSON record types — v1 contract (additive changes only)
// ---------------------------------------------------------------------------

// IssueUserRef is a compact user reference used in NDJSON output.
type IssueUserRef struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// IssueSummary is a compact issue reference used in links, parent, etc.
type IssueSummary struct {
	Key            string `json:"key"`
	Summary        string `json:"summary"`
	Status         string `json:"status"`
	StatusCategory string `json:"statusCategory"`
	IssueType      string `json:"issueType,omitempty"`
}

// IssueLinkRecord is one entry in the IssueRecord.Links slice.
type IssueLinkRecord struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	Direction    string       `json:"direction"`
	Relationship string       `json:"relationship"`
	Issue        IssueSummary `json:"issue"`
}

// AttachmentRecord is one entry in the IssueRecord.Attachments slice.
type AttachmentRecord struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Uploaded string `json:"uploaded"`
	Author   string `json:"author"`
}

// CommentRecord is one entry in CommentsBlock.Items.
type CommentRecord struct {
	ID      string       `json:"id"`
	Author  IssueUserRef `json:"author"`
	Created string       `json:"created"`
	Updated string       `json:"updated"`
	Body    string       `json:"body"`
}

// CommentsBlock is the comments sub-object in IssueRecord.
type CommentsBlock struct {
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
	Items     []CommentRecord `json:"items"`
}

// HistoryChangeRecord is one field change within an ActivityRecord.
type HistoryChangeRecord struct {
	Field        string `json:"field"`
	From         string `json:"from"`
	To           string `json:"to"`
	FromCategory string `json:"fromCategory,omitempty"`
	ToCategory   string `json:"toCategory,omitempty"`
}

// ActivityRecord is one entry in IssueRecord.ActivityTimeline.
type ActivityRecord struct {
	Type    string                `json:"type"`
	Author  IssueUserRef          `json:"author"`
	Created string                `json:"created"`
	Changes []HistoryChangeRecord `json:"changes"`
}

// TimeTrackingRecord is the time-tracking sub-object in IssueRecord.
// All fields are seconds; absent/zero values are omitted from JSON.
type TimeTrackingRecord struct {
	OriginalEstimateSeconds  int64 `json:"originalEstimateSeconds,omitempty"`
	RemainingEstimateSeconds int64 `json:"remainingEstimateSeconds,omitempty"`
	TimeSpentSeconds         int64 `json:"timeSpentSeconds,omitempty"`
}

// ChildIssueRecord is a compact child issue reference (subtask or epic child).
type ChildIssueRecord struct {
	Key            string `json:"key"`
	Summary        string `json:"summary"`
	Status         string `json:"status"`
	StatusCategory string `json:"statusCategory"`
	IssueType      string `json:"issueType"`
	Assignee       string `json:"assignee"` // display name, "" if unassigned
}

// SprintRef is a compact sprint reference used in IssueRecord.Sprints.
type SprintRef struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

// IssueRecord is the NDJSON v1 schema for a single issue.
type IssueRecord struct {
	Key              string              `json:"key"`
	Summary          string              `json:"summary"`
	Status           string              `json:"status"`
	StatusID         string              `json:"statusId,omitempty"`
	StatusCategory   string              `json:"statusCategory"`
	Resolution       *string             `json:"resolution"`
	Priority         string              `json:"priority"`
	IssueType        string              `json:"issueType"`
	Assignee         *IssueUserRef       `json:"assignee"`
	Reporter         *IssueUserRef       `json:"reporter"`
	Created          string              `json:"created"`
	Updated          string              `json:"updated"`
	Description      string              `json:"description"`
	Labels           []string            `json:"labels"`
	Components       []string            `json:"components"`
	FixVersions      []string            `json:"fixVersions"`
	Parent           *IssueSummary       `json:"parent"`
	Epic             *IssueSummary       `json:"epic"`
	Portfolio        *IssueSummary       `json:"portfolio,omitempty"`
	Links            []IssueLinkRecord   `json:"links"`
	Attachments      []AttachmentRecord  `json:"attachments"`
	Comments         CommentsBlock       `json:"comments"`
	HistoryTruncated bool                `json:"historyTruncated"`
	HistoryTotal     int                 `json:"historyTotal"`
	ActivityTimeline []ActivityRecord    `json:"activityTimeline"`
	Children         []ChildIssueRecord  `json:"children"`
	ChildrenTotal    int                 `json:"childrenTotal"`
	ChildrenError    string              `json:"childrenError,omitempty"`
	TimeTracking     *TimeTrackingRecord `json:"timetracking,omitempty"`
	StoryPoints      *float64            `json:"storyPoints,omitempty"`
	Sprints          []SprintRef         `json:"sprints,omitempty"`
}

// HierarchyFieldIDs names the instance-specific custom field IDs used by
// ToIssueRecord to populate Epic/Portfolio/Sprint without per-call API lookups.
// Empty strings disable the corresponding read — caller-provided zero value
// is valid and behaves as "no hierarchy info available".
// SprintField is included for ergonomic threading despite not being hierarchical.
type HierarchyFieldIDs struct {
	EpicLink    string
	ParentLink  string
	Portfolio   string
	StoryPoints string
	SprintField string // Sprint custom field id, e.g. "customfield_11002"
}

// FieldList returns the field IDs in a stable order for use in the
// /issue?fields= query string. Empty entries are skipped.
func (h HierarchyFieldIDs) FieldList() []string {
	out := make([]string, 0, 4)
	if h.EpicLink != "" {
		out = append(out, h.EpicLink)
	}
	if h.ParentLink != "" {
		out = append(out, h.ParentLink)
	}
	if h.Portfolio != "" {
		out = append(out, h.Portfolio)
	}
	if h.StoryPoints != "" {
		out = append(out, h.StoryPoints)
	}
	if h.SprintField != "" {
		out = append(out, h.SprintField)
	}
	return out
}

// ExtractRawKey reads a custom field by id from a RawFields map.
// Returns "" if the field is missing, null, or its key cannot be resolved.
// Handles both string-key and {"key":"..."} object shapes.
func ExtractRawKey(rawFields map[string]json.RawMessage, fieldID string) string {
	if rawFields == nil {
		return ""
	}
	raw, ok := rawFields[fieldID]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// Jira returns some fields as a plain string key.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return asString
	}
	// Or as an object {"key":"..."}.
	var asObj struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil {
		return asObj.Key
	}
	return ""
}

// ToIssueRecord maps IssueRaw to IssueRecord.
// previewN is the number of comments to include in Comments.Items (0 = none).
// hf carries per-instance custom field IDs for Epic Link and Portfolio reads.
func ToIssueRecord(raw IssueRaw, previewN int, hf HierarchyFieldIDs) IssueRecord {
	f := raw.Fields
	rec := IssueRecord{
		Key:            raw.Key,
		Summary:        f.Summary,
		Status:         f.Status.Name,
		StatusID:       f.Status.ID,
		StatusCategory: f.Status.StatusCategory.Name,
		IssueType:      f.IssueType.Name,
		Created:        f.Created,
		Updated:        f.Updated,
		Description:    f.Description,
	}

	// Resolution
	if f.Resolution != nil {
		s := f.Resolution.Name
		rec.Resolution = &s
	}

	// Priority
	if f.Priority != nil {
		rec.Priority = f.Priority.Name
	}

	// Assignee / Reporter
	if f.Assignee != nil {
		rec.Assignee = &IssueUserRef{Name: f.Assignee.Name, DisplayName: f.Assignee.DisplayName}
	}
	if f.Reporter != nil {
		rec.Reporter = &IssueUserRef{Name: f.Reporter.Name, DisplayName: f.Reporter.DisplayName}
	}

	// Labels
	rec.Labels = make([]string, 0, len(f.Labels))
	rec.Labels = append(rec.Labels, f.Labels...)

	// Components
	rec.Components = make([]string, 0, len(f.Components))
	for _, c := range f.Components {
		rec.Components = append(rec.Components, c.Name)
	}

	// FixVersions
	rec.FixVersions = make([]string, 0, len(f.FixVersions))
	for _, v := range f.FixVersions {
		rec.FixVersions = append(rec.FixVersions, v.Name)
	}

	// Parent / Epic
	if f.Parent != nil {
		parentType := strings.ToLower(f.Parent.Fields.IssueType.Name)
		if parentType == "epic" {
			rec.Epic = &IssueSummary{
				Key:            f.Parent.Key,
				Summary:        f.Parent.Fields.Summary,
				Status:         f.Parent.Fields.Status.Name,
				StatusCategory: f.Parent.Fields.Status.StatusCategory.Name,
			}
		} else {
			rec.Parent = &IssueSummary{
				Key:     f.Parent.Key,
				Summary: f.Parent.Fields.Summary,
			}
		}
	}
	// Epic from configured custom field (instance-specific ID, e.g. customfield_10100).
	if rec.Epic == nil && hf.EpicLink != "" {
		if epicKey := ExtractRawKey(raw.RawFields, hf.EpicLink); epicKey != "" {
			rec.Epic = &IssueSummary{Key: epicKey}
		}
	}
	// Portfolio (e.g. Initiative Link). Stored as key-only; caller may resolve summary.
	// Primary source: dedicated Portfolio field (e.g. customfield_21607).
	// Fallback: ParentLink (e.g. customfield_15401) — used when an Epic's parent is
	// an Initiative. Only applied when the key isn't already claimed by rec.Epic or
	// rec.Parent, so a Story whose ParentLink points to its Epic is unaffected.
	if hf.Portfolio != "" {
		if pkKey := ExtractRawKey(raw.RawFields, hf.Portfolio); pkKey != "" {
			rec.Portfolio = &IssueSummary{Key: pkKey}
		}
	}
	if rec.Portfolio == nil && hf.ParentLink != "" {
		if pkKey := ExtractRawKey(raw.RawFields, hf.ParentLink); pkKey != "" {
			epicKey := ""
			if rec.Epic != nil {
				epicKey = rec.Epic.Key
			}
			parentKey := ""
			if rec.Parent != nil {
				parentKey = rec.Parent.Key
			}
			if pkKey != epicKey && pkKey != parentKey {
				rec.Portfolio = &IssueSummary{Key: pkKey}
			}
		}
	}

	// Links
	rec.Links = make([]IssueLinkRecord, 0, len(f.IssueLinks))
	for _, l := range f.IssueLinks {
		if l.OutwardIssue != nil {
			rec.Links = append(rec.Links, IssueLinkRecord{
				ID:           l.ID,
				Type:         l.Type.Name,
				Direction:    "outward",
				Relationship: l.Type.Outward,
				Issue: IssueSummary{
					Key:            l.OutwardIssue.Key,
					Summary:        l.OutwardIssue.Fields.Summary,
					IssueType:      l.OutwardIssue.Fields.IssueType.Name,
					Status:         l.OutwardIssue.Fields.Status.Name,
					StatusCategory: l.OutwardIssue.Fields.Status.StatusCategory.Name,
				},
			})
		} else if l.InwardIssue != nil {
			rec.Links = append(rec.Links, IssueLinkRecord{
				ID:           l.ID,
				Type:         l.Type.Name,
				Direction:    "inward",
				Relationship: l.Type.Inward,
				Issue: IssueSummary{
					Key:            l.InwardIssue.Key,
					Summary:        l.InwardIssue.Fields.Summary,
					IssueType:      l.InwardIssue.Fields.IssueType.Name,
					Status:         l.InwardIssue.Fields.Status.Name,
					StatusCategory: l.InwardIssue.Fields.Status.StatusCategory.Name,
				},
			})
		}
	}

	// Attachments
	rec.Attachments = make([]AttachmentRecord, 0, len(f.Attachment))
	for _, a := range f.Attachment {
		rec.Attachments = append(rec.Attachments, AttachmentRecord{
			ID:       a.ID,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Size:     a.Size,
			Uploaded: a.Created,
			Author:   a.Author.DisplayName,
		})
	}

	// Comments — take the last previewN (Jira returns oldest-first)
	allComments := f.Comment.Comments
	total := f.Comment.Total
	var items []CommentRecord
	if previewN > 0 && len(allComments) > 0 {
		start := len(allComments) - previewN
		if start < 0 {
			start = 0
		}
		tail := allComments[start:]
		items = make([]CommentRecord, 0, len(tail))
		for _, c := range tail {
			items = append(items, CommentRecord{
				ID:      c.ID,
				Author:  IssueUserRef{Name: c.Author.Name, DisplayName: c.Author.DisplayName},
				Created: c.Created,
				Updated: c.Updated,
				Body:    c.Body,
			})
		}
	}
	if items == nil {
		items = []CommentRecord{}
	}
	rec.Comments = CommentsBlock{
		Total:     total,
		Truncated: total > len(items),
		Items:     items,
	}

	// Activity timeline from changelog
	if raw.Changelog != nil {
		rec.HistoryTotal = raw.Changelog.Total
		rec.HistoryTruncated = len(raw.Changelog.Histories) < raw.Changelog.Total
		rec.ActivityTimeline = make([]ActivityRecord, 0, len(raw.Changelog.Histories))
		for _, h := range raw.Changelog.Histories {
			changes := make([]HistoryChangeRecord, 0, len(h.Items))
			for _, item := range h.Items {
				// Skip Jira drag-reorder noise
				if strings.HasPrefix(item.Field, "Rank") {
					continue
				}
				ch := HistoryChangeRecord{
					Field: item.Field,
					From:  item.FromString,
					To:    item.ToString,
				}
				changes = append(changes, ch)
			}
			if len(changes) == 0 {
				continue
			}
			// Determine type: "transition" for status field changes, "update" otherwise
			actType := "update"
			for _, ch := range changes {
				if ch.Field == "status" {
					actType = "transition"
					break
				}
			}
			rec.ActivityTimeline = append(rec.ActivityTimeline, ActivityRecord{
				Type:    actType,
				Author:  IssueUserRef{Name: h.Author.Name, DisplayName: h.Author.DisplayName},
				Created: h.Created,
				Changes: changes,
			})
		}
	} else {
		rec.ActivityTimeline = []ActivityRecord{}
	}

	// Subtasks — always present as a slice (never null) in JSON output.
	rec.Children = make([]ChildIssueRecord, 0, len(f.Subtasks))
	for _, s := range f.Subtasks {
		assignee := ""
		if s.Fields.Assignee != nil {
			assignee = s.Fields.Assignee.DisplayName
		}
		rec.Children = append(rec.Children, ChildIssueRecord{
			Key:            s.Key,
			Summary:        s.Fields.Summary,
			Status:         s.Fields.Status.Name,
			StatusCategory: s.Fields.Status.StatusCategory.Name,
			IssueType:      s.Fields.IssueType.Name,
			Assignee:       assignee,
		})
	}
	rec.ChildrenTotal = len(rec.Children)

	// Time tracking — present only when Jira returned the sub-object with non-zero data
	if f.TimeTracking != nil {
		o, r, s := f.TimeTracking.OriginalEstimateSeconds, f.TimeTracking.RemainingEstimateSeconds, f.TimeTracking.TimeSpentSeconds
		if o != 0 || r != 0 || s != 0 {
			rec.TimeTracking = &TimeTrackingRecord{
				OriginalEstimateSeconds:  o,
				RemainingEstimateSeconds: r,
				TimeSpentSeconds:         s,
			}
		}
	}

	// Story Points — dynamic custom field, read via RawFields
	if hf.StoryPoints != "" {
		if spRaw, ok := raw.RawFields[hf.StoryPoints]; ok && len(spRaw) > 0 && string(spRaw) != "null" {
			var n float64
			if err := json.Unmarshal(spRaw, &n); err == nil {
				rec.StoryPoints = &n
			}
		}
	}

	// Sprints — dynamic custom field, read via RawFields
	if hf.SprintField != "" {
		if sprintRaw, ok := raw.RawFields[hf.SprintField]; ok && len(sprintRaw) > 0 && string(sprintRaw) != "null" {
			rec.Sprints = parseSprintRaw(sprintRaw)
		}
	}

	return rec
}

// parseSprintRaw parses the sprint custom field which Jira DC returns in two formats:
//
// Newer JSON array:
//
//	[{"id":28037,"name":"Sprint 153","state":"active","startDate":"...","endDate":"..."}]
//
// Legacy string array (older Jira DC versions):
//
//	["com.atlassian.greenhopper...Sprint@xxx[id=28037,name=Sprint 153,state=ACTIVE,...]"]
//
// Returns nil on any parse error — caller silently omits the section.
func parseSprintRaw(raw json.RawMessage) []SprintRef {
	// Try JSON object array first.
	var jsonSprints []struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		State     string `json:"state"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	if err := json.Unmarshal(raw, &jsonSprints); err == nil && len(jsonSprints) > 0 {
		refs := make([]SprintRef, 0, len(jsonSprints))
		for _, s := range jsonSprints {
			// Filter out zero-id entries (sometimes returned for future sprints not yet started).
			if s.ID == 0 {
				continue
			}
			refs = append(refs, SprintRef{
				ID:        s.ID,
				Name:      s.Name,
				State:     strings.ToLower(s.State),
				StartDate: s.StartDate,
				EndDate:   s.EndDate,
			})
		}
		if len(refs) > 0 {
			return refs
		}
	}
	// Try legacy string array.
	var legacyStrings []string
	if err := json.Unmarshal(raw, &legacyStrings); err != nil {
		return nil
	}
	var refs []SprintRef
	for _, s := range legacyStrings {
		// Format: "com.atlassian...Sprint@xxx[id=28037,name=Sprint 153,state=ACTIVE,startDate=...,endDate=...]"
		open := strings.Index(s, "[")
		close := strings.LastIndex(s, "]")
		if open < 0 || close <= open {
			continue
		}
		inner := s[open+1 : close]
		kv := make(map[string]string)
		for _, pair := range strings.Split(inner, ",") {
			eq := strings.Index(pair, "=")
			if eq < 0 {
				continue
			}
			k := strings.TrimSpace(pair[:eq])
			v := strings.TrimSpace(pair[eq+1:])
			if v == "<null>" || v == "null" {
				v = ""
			}
			kv[k] = v
		}
		id := 0
		if idStr, ok := kv["id"]; ok {
			if n, err := strconv.Atoi(idStr); err == nil {
				id = n
			}
		}
		if id == 0 {
			continue
		}
		refs = append(refs, SprintRef{
			ID:        id,
			Name:      kv["name"],
			State:     strings.ToLower(kv["state"]),
			StartDate: kv["startDate"],
			EndDate:   kv["endDate"],
		})
	}
	return refs
}

// FormatSprintField renders a raw sprint custom-field value as a compact,
// human-readable one-line string, e.g. "Sprint 152, Sprint 153 (active)".
// It handles both the modern JSON-object array and the legacy GreenHopper
// toString array. Returns "" when the value is empty or unparseable, so callers
// can fall back to their own empty-value handling.
func FormatSprintField(raw json.RawMessage) string {
	refs := parseSprintRaw(raw)
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("sprint %d", r.ID)
		}
		if r.State == "active" {
			name += " (active)"
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ", ")
}

// ResolveActivityStatusCategories fills the FromCategory and ToCategory fields
// of every status-field change in the activity timeline by matching the status
// display name against the provided status list.
//
// The Jira changelog API returns status transitions as fromString/toString
// (display names) without category information. This function resolves that gap
// using a cached status list — call ListStatuses first, then pass the result here.
//
// Changes for non-status fields are left untouched.
// Unknown status names (custom or deleted statuses) leave the category empty.
func ResolveActivityStatusCategories(activities []ActivityRecord, statuses []Status) {
	if len(activities) == 0 || len(statuses) == 0 {
		return
	}
	// Build name → category key map (case-insensitive match).
	byName := make(map[string]string, len(statuses))
	for _, s := range statuses {
		byName[strings.ToLower(s.Name)] = s.StatusCategory.Name
	}
	for i := range activities {
		for j := range activities[i].Changes {
			ch := &activities[i].Changes[j]
			if ch.Field != "status" {
				continue
			}
			if cat, ok := byName[strings.ToLower(ch.From)]; ok {
				ch.FromCategory = cat
			}
			if cat, ok := byName[strings.ToLower(ch.To)]; ok {
				ch.ToCategory = cat
			}
		}
	}
}

// DeleteIssue deletes an issue. When deleteSubtasks is true, subtasks are
// cascaded; otherwise Jira returns 400 if subtasks exist.
func (c *Client) DeleteIssue(ctx context.Context, key string, deleteSubtasks bool) error {
	q := url.Values{}
	if deleteSubtasks {
		q.Set("deleteSubtasks", "true")
	}
	body, status, err := c.Delete(ctx, "/issue/"+key, q)
	if err != nil {
		return err
	}
	if status != 204 {
		return fmt.Errorf("delete issue %s: %w", key, MapStatus("", status, body))
	}
	return nil
}

// TTLIssueSummary is the cache TTL for resolved issue summaries (Epic, Portfolio).
// Short enough to pick up renames/status changes within an hour.
const TTLIssueSummary = 1 * time.Hour

// issueSummaryCache is the on-disk shape for a cached summary.
type issueSummaryCache struct {
	Summary        string `json:"summary"`
	Status         string `json:"status"`
	StatusCategory string `json:"statusCategory"`
	IssueType      string `json:"issuetype"`
}

// GetIssueSummary fetches summary, status, and issuetype for a single issue key,
// returning the result as an IssueSummary. Results are cached per-profile with a
// 1-hour TTL so Epic Link / Portfolio backfills don't fire a live request every
// time the same issue is shown.
//
// Fail-soft: on any error the returned IssueSummary has only Key populated.
func (c *Client) GetIssueSummary(ctx context.Context, key string, store *cache.Store) IssueSummary {
	out := IssueSummary{Key: key}
	if store != nil {
		var cached issueSummaryCache
		if err := store.Get("issue-summary/"+key, TTLIssueSummary, &cached); err == nil {
			out.Summary = cached.Summary
			out.Status = cached.Status
			out.StatusCategory = cached.StatusCategory
			out.IssueType = cached.IssueType
			return out
		}
	}
	raw, err := c.GetIssue(ctx, key, "summary,status,issuetype", false)
	if err != nil {
		return out
	}
	out.Summary = raw.Fields.Summary
	out.Status = raw.Fields.Status.Name
	out.StatusCategory = raw.Fields.Status.StatusCategory.Name
	out.IssueType = raw.Fields.IssueType.Name
	if store != nil {
		_ = store.Put("issue-summary/"+key, issueSummaryCache{
			Summary:        out.Summary,
			Status:         out.Status,
			StatusCategory: out.StatusCategory,
			IssueType:      out.IssueType,
		})
	}
	return out
}
