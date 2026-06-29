package jira

import (
	"context"
	"fmt"
	"strings"
)

// HierarchyNode is one row in a hierarchy chain.
type HierarchyNode struct {
	Key            string `json:"key"`
	Summary        string `json:"summary"`
	Status         string `json:"status"`
	StatusCategory string `json:"statusCategory"`
	IssueType      string `json:"issueType"`
	Assignee       string `json:"assignee,omitempty"`
	IsSubject      bool   `json:"isSubject,omitempty"`
}

// HierarchyChain is the full Initiative → … → Subject → Children chain.
type HierarchyChain struct {
	Ancestors     []HierarchyNode `json:"ancestors"`
	Subject       HierarchyNode   `json:"subject"`
	Children      []HierarchyNode `json:"children"`
	ChildrenTotal int             `json:"childrenTotal"`
	ChildrenError string          `json:"childrenError,omitempty"`
}

// maxAncestorDepth caps walks to defend against cyclical/long Parent Link chains.
const maxAncestorDepth = 8

// BuildHierarchy fetches the subject, walks up via Portfolio → ParentLink → parent → EpicLink,
// then fetches children appropriate to the subject's type.
//   - Subject is an Epic       → children via JQL `"Epic Link" = KEY`
//   - Subject is a Portfolio   → children via JQL `"<portfolioFieldName>" = KEY`
//   - Subject is otherwise     → children are subtasks from the subject's IssueRaw
//
// hf carries the resolved per-profile field IDs. Empty fields are skipped.
// portfolioFieldName is the human-readable name of the portfolio field used for
// the children JQL — falls back to no children when empty.
func BuildHierarchy(
	ctx context.Context,
	c *Client,
	hf HierarchyFieldIDs,
	portfolioFieldName string,
	key string,
) (HierarchyChain, error) {
	// Build the base fields list for the subject fetch.
	baseFieldSlice := []string{"summary", "status", "issuetype", "assignee", "subtasks", "parent"}
	baseFieldSlice = append(baseFieldSlice, hf.FieldList()...)
	baseFields := strings.Join(baseFieldSlice, ",")

	// Step 1: fetch subject.
	subjectRaw, err := c.GetIssue(ctx, key, baseFields, false)
	if err != nil {
		return HierarchyChain{}, err
	}

	assignee := ""
	if subjectRaw.Fields.Assignee != nil {
		assignee = subjectRaw.Fields.Assignee.DisplayName
	}
	subject := HierarchyNode{
		Key:            subjectRaw.Key,
		Summary:        subjectRaw.Fields.Summary,
		Status:         subjectRaw.Fields.Status.Name,
		StatusCategory: subjectRaw.Fields.Status.StatusCategory.Name,
		IssueType:      subjectRaw.Fields.IssueType.Name,
		Assignee:       assignee,
		IsSubject:      true,
	}

	// Step 2: walk up ancestors.
	var ancestors []HierarchyNode
	seen := map[string]bool{key: true}
	currentRaw := subjectRaw
	for depth := 0; depth < maxAncestorDepth; depth++ {
		parentKey := resolveHierarchyParentKey(currentRaw, hf)
		if parentKey == "" || seen[parentKey] {
			break
		}
		seen[parentKey] = true

		// Fetch ancestor with summary+status+issuetype + hierarchy fields (for further walking).
		ancFields := "summary,status,issuetype,parent," + strings.Join(hf.FieldList(), ",")
		ancRaw, err := c.GetIssue(ctx, parentKey, ancFields, false)
		if err != nil {
			break // fail soft — stop walk
		}
		anc := HierarchyNode{
			Key:            ancRaw.Key,
			Summary:        ancRaw.Fields.Summary,
			Status:         ancRaw.Fields.Status.Name,
			StatusCategory: ancRaw.Fields.Status.StatusCategory.Name,
			IssueType:      ancRaw.Fields.IssueType.Name,
		}
		// Prepend so ancestors[0] is the root.
		ancestors = append([]HierarchyNode{anc}, ancestors...)
		currentRaw = ancRaw
	}

	// Step 3: fetch children.
	var children []HierarchyNode
	childrenTotal := 0
	childrenError := ""

	childFields := []string{"summary", "status", "issuetype", "assignee"}
	switch strings.ToLower(subject.IssueType) {
	case "epic":
		// Classic projects: children linked via Epic Link custom field.
		jql := `"Epic Link" = ` + key
		resp, err := c.Search(ctx, jql, 1, 100, childFields)
		if err != nil || resp.Total == 0 {
			// Next-gen / team-managed projects: children linked via built-in parent field.
			if resp2, err2 := c.Search(ctx, `parent = `+key, 1, 100, childFields); err2 == nil && resp2.Total > 0 {
				resp, err = resp2, nil
			}
		}
		if err != nil {
			childrenError = err.Error()
		} else {
			childrenTotal = resp.Total
			for _, issue := range resp.Issues {
				children = append(children, nodeFromRaw(issue))
			}
		}
	default:
		// Portfolio-level issue: only attempt JQL if the subject's type name
		// matches a known portfolio-level pattern (initiative, program, feature, theme, portfolio).
		// This avoids firing JQL on Stories, Bugs, Tasks, etc.
		if isPortfolioTypeLevel(subject.IssueType) {
			// Try Parent Link field first (= operator, exact match).
			// On many instances Epics are attached to Initiatives via Parent Link.
			if hf.ParentLink != "" && childrenTotal == 0 && childrenError == "" {
				jql := `cf[` + strings.TrimPrefix(hf.ParentLink, "customfield_") + `] = ` + key
				resp, err := c.Search(ctx, jql, 1, 100, childFields)
				if err == nil && resp.Total > 0 {
					childrenTotal = resp.Total
					for _, issue := range resp.Issues {
						children = append(children, nodeFromRaw(issue))
					}
				}
			}
			// Try portfolio display-name field (~ operator, text match).
			if portfolioFieldName != "" && childrenTotal == 0 && childrenError == "" {
				jql := `"` + portfolioFieldName + `" ~ "` + key + `"`
				resp, err := c.Search(ctx, jql, 1, 100, childFields)
				if err != nil {
					childrenError = err.Error()
				} else if resp.Total > 0 {
					childrenTotal = resp.Total
					for _, issue := range resp.Issues {
						children = append(children, nodeFromRaw(issue))
					}
				}
			}
		}
		if childrenTotal == 0 && childrenError == "" {
			// Fall back to subtasks.
			for _, s := range subjectRaw.Fields.Subtasks {
				a := ""
				if s.Fields.Assignee != nil {
					a = s.Fields.Assignee.DisplayName
				}
				children = append(children, HierarchyNode{
					Key:            s.Key,
					Summary:        s.Fields.Summary,
					Status:         s.Fields.Status.Name,
					StatusCategory: s.Fields.Status.StatusCategory.Name,
					IssueType:      s.Fields.IssueType.Name,
					Assignee:       a,
				})
			}
			childrenTotal = len(children)
		}
	}

	if ancestors == nil {
		ancestors = []HierarchyNode{}
	}
	if children == nil {
		children = []HierarchyNode{}
	}
	return HierarchyChain{
		Ancestors:     ancestors,
		Subject:       subject,
		Children:      children,
		ChildrenTotal: childrenTotal,
		ChildrenError: childrenError,
	}, nil
}

// resolveHierarchyParentKey returns the best parent key from a raw issue
// using precedence: Portfolio → ParentLink → typed Parent field → EpicLink.
func resolveHierarchyParentKey(raw IssueRaw, hf HierarchyFieldIDs) string {
	if hf.Portfolio != "" {
		if k := ExtractRawKey(raw.RawFields, hf.Portfolio); k != "" {
			return k
		}
	}
	if hf.ParentLink != "" {
		if k := ExtractRawKey(raw.RawFields, hf.ParentLink); k != "" {
			return k
		}
	}
	if raw.Fields.Parent != nil && raw.Fields.Parent.Key != "" {
		return raw.Fields.Parent.Key
	}
	if hf.EpicLink != "" {
		if k := ExtractRawKey(raw.RawFields, hf.EpicLink); k != "" {
			return k
		}
	}
	return ""
}

// isPortfolioTypeLevel returns true when issueType name matches a known
// portfolio-level type (initiative, program, feature, theme, portfolio).
// This prevents firing the portfolio children JQL on ordinary issue types
// (Story, Bug, Task) that happen to have no portfolio parent.
func isPortfolioTypeLevel(issueType string) bool {
	lower := strings.ToLower(issueType)
	for _, term := range portfolioSeedTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// nodeFromRaw builds a HierarchyNode from a SearchResponse IssueRaw.
func nodeFromRaw(raw IssueRaw) HierarchyNode {
	a := ""
	if raw.Fields.Assignee != nil {
		a = raw.Fields.Assignee.DisplayName
	}
	return HierarchyNode{
		Key:            raw.Key,
		Summary:        raw.Fields.Summary,
		Status:         raw.Fields.Status.Name,
		StatusCategory: raw.Fields.Status.StatusCategory.Name,
		IssueType:      raw.Fields.IssueType.Name,
		Assignee:       a,
	}
}
// FieldProbe holds the result of a live diagnostic test for one hierarchy field.
type FieldProbe struct {
	Label     string // "Epic Link", "Parent Link", "Portfolio"
	FieldID   string // configured field ID, "" if unconfigured
	OK        bool   // true when the field is searchable and returns results
	Note      string // human-readable status detail
}

// ProbeHierarchy runs lightweight live diagnostics (limit-1 searches) for each
// configured hierarchy field. Safe to call with a zero HierarchyFieldIDs — fields
// that are unconfigured are reported as such without making any API calls.
// Errors per field are captured in FieldProbe.Note; the function itself never returns
// an error so callers get a complete report even when one field fails.
func ProbeHierarchy(ctx context.Context, c *Client, hf HierarchyFieldIDs, portfolioFieldName string) []FieldProbe {
	var probes []FieldProbe
	tiny := []string{"summary"}

	// Epic Link — classic projects use "Epic Link" = KEY children.
	epicProbe := FieldProbe{Label: "Epic Link", FieldID: hf.EpicLink}
	if hf.EpicLink == "" {
		epicProbe.Note = "not configured — classic Epic→Story linking disabled"
	} else {
		jql := `"Epic Link" is not EMPTY`
		resp, err := c.Search(ctx, jql, 1, 1, tiny)
		if err != nil {
			epicProbe.Note = "field not searchable: " + err.Error()
		} else if resp.Total == 0 {
			epicProbe.OK = true
			epicProbe.Note = "field exists but no issues found with Epic Link set (next-gen instance?)"
		} else {
			epicProbe.OK = true
			epicProbe.Note = fmt.Sprintf("ok — %d issues have Epic Link set", resp.Total)
		}
	}
	probes = append(probes, epicProbe)

	// Parent Link — used for Epic→Initiative (and next-gen Epic→Story).
	plProbe := FieldProbe{Label: "Parent Link", FieldID: hf.ParentLink}
	if hf.ParentLink == "" {
		plProbe.Note = "not configured — Initiative→Epic linking via Parent Link disabled"
	} else {
		fieldNum := strings.TrimPrefix(hf.ParentLink, "customfield_")
		jql := `cf[` + fieldNum + `] is not EMPTY`
		resp, err := c.Search(ctx, jql, 1, 1, tiny)
		if err != nil {
			plProbe.Note = "field not searchable: " + err.Error()
		} else if resp.Total == 0 {
			plProbe.OK = true
			plProbe.Note = "field exists but no issues found with Parent Link set"
		} else {
			plProbe.OK = true
			plProbe.Note = fmt.Sprintf("ok — %d issues have Parent Link set", resp.Total)
		}
	}
	probes = append(probes, plProbe)

	// Portfolio field.
	pfProbe := FieldProbe{Label: "Portfolio", FieldID: hf.Portfolio}
	if hf.Portfolio == "" {
		pfProbe.Note = "not configured — Initiative→Epic linking via portfolio field disabled"
	} else if portfolioFieldName == "" {
		pfProbe.Note = "field ID set but display name missing — run: jiracli config hierarchy --rediscover"
	} else {
		jql := `"` + portfolioFieldName + `" is not EMPTY`
		resp, err := c.Search(ctx, jql, 1, 1, tiny)
		if err != nil {
			// ~ operator might be required; fall back
			jql2 := `"` + portfolioFieldName + `" ~ "*"`
			resp2, err2 := c.Search(ctx, jql2, 1, 1, tiny)
			if err2 != nil {
				pfProbe.Note = "field not searchable: " + err.Error()
			} else if resp2.Total == 0 {
				pfProbe.OK = true
				pfProbe.Note = "field exists but no issues found with portfolio field set"
			} else {
				pfProbe.OK = true
				pfProbe.Note = fmt.Sprintf("ok (text-search) — %d issues have %s set", resp2.Total, portfolioFieldName)
			}
		} else if resp.Total == 0 {
			pfProbe.OK = true
			pfProbe.Note = "field exists but no issues found with portfolio field set"
		} else {
			pfProbe.OK = true
			pfProbe.Note = fmt.Sprintf("ok — %d issues have %s set", resp.Total, portfolioFieldName)
		}
	}
	probes = append(probes, pfProbe)

	return probes
}
