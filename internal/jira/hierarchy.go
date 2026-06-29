package jira

import (
	"context"
	"fmt"
	"strings"
)

// HierarchyNode is one row in a hierarchy chain.
type HierarchyNode struct {
	Key            string          `json:"key"`
	Summary        string          `json:"summary"`
	Status         string          `json:"status"`
	StatusCategory string          `json:"statusCategory"`
	IssueType      string          `json:"issueType"`
	Assignee       string          `json:"assignee,omitempty"`
	IsSubject      bool            `json:"isSubject,omitempty"`
	Children       []HierarchyNode `json:"children,omitempty"`
}

// HierarchyChain is the full Initiative → … → Subject → Children chain.
type HierarchyChain struct {
	Ancestors              []HierarchyNode `json:"ancestors"`
	Subject                HierarchyNode   `json:"subject"`
	Children               []HierarchyNode `json:"children"`
	ChildrenTotal          int             `json:"childrenTotal"`
	ChildrenTruncated      bool            `json:"childrenTruncated,omitempty"`
	DescendantsTruncated   bool            `json:"descendantsTruncated,omitempty"`
	ChildrenError          string          `json:"childrenError,omitempty"`
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
	fetchAll bool,
	depth int,
	since string,
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

	// sinceClause appends an updated-date predicate when since is non-empty.
	sinceClause := ""
	if since != "" {
		sinceClause = ` AND updated >= "` + since + `"`
	}

	// fetch wraps fetchAllChildren / single-page based on the fetchAll flag.
	// since is automatically appended to every JQL expression.
	fetch := func(jql string) ([]HierarchyNode, int, error) {
		jql += sinceClause
		if fetchAll {
			return fetchAllChildren(ctx, c, jql, childFields)
		}
		resp, err := c.Search(ctx, jql, 1, 100, childFields)
		if err != nil {
			return nil, 0, err
		}
		var nodes []HierarchyNode
		for _, issue := range resp.Issues {
			nodes = append(nodes, nodeFromRaw(issue))
		}
		return nodes, resp.Total, nil
	}

	truncated := false
	switch strings.ToLower(subject.IssueType) {
	case "epic":
		// Classic projects: children linked via Epic Link custom field.
		kids, total, err := fetch(`"Epic Link" = ` + key)
		if err != nil || total == 0 {
			// Next-gen / team-managed projects: children linked via built-in parent field.
			if kids2, total2, err2 := fetch(`parent = ` + key); err2 == nil && total2 > 0 {
				kids, total, err = kids2, total2, nil
			}
		}
		if err != nil {
			childrenError = err.Error()
		} else {
			children = kids
			childrenTotal = total
			truncated = !fetchAll && total > len(kids)
		}
	default:
		// Portfolio-level issue: only attempt JQL if the subject's type name
		// matches a known portfolio-level pattern (initiative, program, feature, theme, portfolio).
		// This avoids firing JQL on Stories, Bugs, Tasks, etc.
		if isPortfolioTypeLevel(subject.IssueType) {
			// Try Parent Link field first (= operator, exact match).
			if hf.ParentLink != "" {
				jql := `cf[` + strings.TrimPrefix(hf.ParentLink, "customfield_") + `] = ` + key
				if kids, total, err := fetch(jql); err == nil && total > 0 {
					children = kids
					childrenTotal = total
					truncated = !fetchAll && total > len(kids)
				}
			}
			// Try portfolio display-name field (~ operator, text match).
			if portfolioFieldName != "" && childrenTotal == 0 {
				jql := `"` + portfolioFieldName + `" ~ "` + key + `"`
				kids, total, err := fetch(jql)
				if err != nil {
					childrenError = err.Error()
				} else if total > 0 {
					children = kids
					childrenTotal = total
					truncated = !fetchAll && total > len(kids)
				}
			}
		}
		if childrenTotal == 0 && childrenError == "" {
			// Fall back to subtasks (already fully fetched inline with the subject).
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

	// Recursive descent: depth >= 2 fetches children-of-children.
	if depth < 1 {
		depth = 1
	}
	descendantsTruncated := false
	if depth >= 2 && len(children) > 0 {
		descendantFields := []string{"summary", "status", "issuetype", "assignee", hf.EpicLink, hf.ParentLink, hf.Portfolio}
		// Filter empty strings from fields list.
		filteredFields := descendantFields[:0:0]
		for _, f := range descendantFields {
			if f != "" {
				filteredFields = append(filteredFields, f)
			}
		}
		// currentPtrs holds pointers into the actual slice hierarchy so assignments persist.
		currentPtrs := make([]*HierarchyNode, len(children))
		for i := range children {
			currentPtrs[i] = &children[i]
		}
		for d := 2; d <= depth; d++ {
			if len(currentPtrs) == 0 {
				break
			}
			parentKeys := make([]string, len(currentPtrs))
			for i, p := range currentPtrs {
				parentKeys[i] = p.Key
			}
			strategy, jqlReady := strategyForLevel(currentPtrs, hf, portfolioFieldName)
			if !jqlReady {
				break
			}
			byParent, levelTotal, err := fetchChildrenForParents(ctx, c, parentKeys, strategy, hf, portfolioFieldName, filteredFields, fetchAll, since)
			if err != nil {
				break // fail-soft: keep what we have
			}
			// Count fetched descendants; if server total exceeds what we got, flag truncation.
			fetchedCount := 0
			for _, kids := range byParent {
				fetchedCount += len(kids)
			}
			if !fetchAll && levelTotal > fetchedCount {
				descendantsTruncated = true
			}
			var nextPtrs []*HierarchyNode
			for _, ptr := range currentPtrs {
				kids := byParent[ptr.Key]
				// Always set Children (even empty) so the renderer knows descent was attempted.
				ptr.Children = kids
				if kids == nil {
					ptr.Children = []HierarchyNode{}
				}
				for i := range ptr.Children {
					nextPtrs = append(nextPtrs, &ptr.Children[i])
				}
			}
			currentPtrs = nextPtrs
		}
	}

	return HierarchyChain{
		Ancestors:            ancestors,
		Subject:              subject,
		Children:             children,
		ChildrenTotal:        childrenTotal,
		ChildrenTruncated:    truncated,
		DescendantsTruncated: descendantsTruncated,
		ChildrenError:        childrenError,
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

// fetchAllChildren executes jql in pages of 100 until all results are collected.
// Returns the full slice of nodes, the server-reported total, and any error.
// On the first-page error the error is returned immediately.
// On a mid-pagination error we return what we have so far with no error (fail-soft).
func fetchAllChildren(ctx context.Context, c *Client, jql string, fields []string) ([]HierarchyNode, int, error) {
	const pageSize = 100
	var nodes []HierarchyNode
	total := 0
	for page := 1; ; page++ {
		resp, err := c.Search(ctx, jql, page, pageSize, fields)
		if err != nil {
			if page == 1 {
				return nil, 0, err
			}
			// Mid-pagination failure: return what we have.
			break
		}
		total = resp.Total
		for _, issue := range resp.Issues {
			nodes = append(nodes, nodeFromRaw(issue))
		}
		fetched := (page-1)*pageSize + len(resp.Issues)
		if fetched >= total {
			break
		}
	}
	return nodes, total, nil
}

// strategyForLevel inspects a level's nodes and picks the JQL strategy that
// will reach their children. Returns ("", false) when descent is not possible
// (leaf-level types like Story/Sub-task, or unconfigured hf fields).
func strategyForLevel(level []*HierarchyNode, hf HierarchyFieldIDs, portfolioFieldName string) (string, bool) {
	if len(level) == 0 {
		return "", false
	}
	// Count issue types.
	epicCount := 0
	portfolioCount := 0
	leafCount := 0
	for _, n := range level {
		lower := strings.ToLower(n.IssueType)
		switch {
		case lower == "epic":
			epicCount++
		case isPortfolioTypeLevel(n.IssueType):
			portfolioCount++
		default:
			leafCount++
		}
	}
	total := len(level)
	// All leaves (Story, Bug, Task, Sub-task, etc.): don't descend.
	if leafCount == total {
		return "", false
	}
	// Majority are epics: use epicLinkClassic (fetchChildrenForParents handles
	// nextGen fallback internally when result count is 0).
	if epicCount > 0 && epicCount >= total-leafCount {
		if hf.EpicLink != "" {
			return "epicLinkClassic", true
		}
		// No epic link field configured — try parentNextGen.
		return "parentNextGen", true
	}
	// Majority are portfolio-level nodes.
	if portfolioCount > 0 {
		if hf.ParentLink != "" {
			return "parentLinkCF", true
		}
		if portfolioFieldName != "" {
			return "portfolioText", true
		}
	}
	return "", false
}

// parentKeyForChild inspects a result IssueRaw and returns which parent key
// in parentKeySet claims it, based on strategy. Returns "" if no parent in
// parentKeySet claims it.
func parentKeyForChild(raw IssueRaw, parentKeySet map[string]bool, strategy string, hf HierarchyFieldIDs) string {
	var k string
	switch strategy {
	case "epicLinkClassic":
		k = ExtractRawKey(raw.RawFields, hf.EpicLink)
	case "parentNextGen":
		if raw.Fields.Parent != nil {
			k = raw.Fields.Parent.Key
		}
	case "parentLinkCF":
		k = ExtractRawKey(raw.RawFields, hf.ParentLink)
	case "portfolioText":
		k = ExtractRawKey(raw.RawFields, hf.Portfolio)
		if k == "" {
			// Fallback: substring match on portfolio field's raw string value.
			if raw.RawFields != nil {
				if v, ok := raw.RawFields[hf.Portfolio]; ok {
					s := string(v)
					for pk := range parentKeySet {
						if strings.Contains(s, pk) {
							return pk
						}
					}
				}
			}
		}
	}
	if parentKeySet[k] {
		return k
	}
	return ""
}

// fetchChildrenForParents returns, for the given parent keys, every direct
// child of each, grouped by parent key. It issues one JQL per batch of ≤90
// parent keys (90 for safety under typical Jira JQL clause limits; on 400
// the batch is halved, down to a minimum of 10).
//
// strategy controls the JQL shape and ownership-attribution field:
//   - "epicLinkClassic":  "Epic Link" in (K1,K2,…)
//   - "parentNextGen":    parent in (K1,K2,…)
//   - "parentLinkCF":     cf[NNNN] in (K1,K2,…)
//   - "portfolioText":    "<portfolioFieldName>" ~ "K1" OR … (one clause per key)
//
// For epicLinkClassic when result count is 0, a parentNextGen fallback is tried
// automatically (next-gen / team-managed projects).
//
// Returns map[parentKey][]HierarchyNode, total count across all parents, error
// (non-nil only on first-page total failure).
func fetchChildrenForParents(
	ctx context.Context, c *Client,
	parentKeys []string, strategy string, hf HierarchyFieldIDs, portfolioFieldName string,
	childFields []string, fetchAll bool, since string,
) (map[string][]HierarchyNode, int, error) {
	const maxBatch = 90
	const minBatch = 10

	parentKeySet := make(map[string]bool, len(parentKeys))
	for _, k := range parentKeys {
		parentKeySet[k] = true
	}

	result := make(map[string][]HierarchyNode)
	totalCount := 0
	var firstErr error
	gotAny := false

	// sinceClause is appended to every JQL query when since is non-empty.
	sinceClause := ""
	if since != "" {
		sinceClause = ` AND updated >= "` + since + `"`
	}

	// buildJQL builds the JQL for a batch of keys, including the since clause.
	buildJQL := func(batch []string) string {
		var base string
		switch strategy {
		case "epicLinkClassic":
			base = `"Epic Link" in (` + strings.Join(batch, ",") + `)`
		case "parentNextGen":
			base = `parent in (` + strings.Join(batch, ",") + `)`
		case "parentLinkCF":
			num := strings.TrimPrefix(hf.ParentLink, "customfield_")
			base = `cf[` + num + `] in (` + strings.Join(batch, ",") + `)`
		case "portfolioText":
			clauses := make([]string, len(batch))
			for i, k := range batch {
				clauses[i] = `"` + portfolioFieldName + `" ~ "` + k + `"`
			}
			base = strings.Join(clauses, " OR ")
		}
		if base == "" {
			return ""
		}
		return base + sinceClause
	}

	// searchRaw issues jql and returns raw issues, with 400-retry halving down to minBatch.
	searchRaw := func(batch []string) ([]IssueRaw, int, error) {
		currentKeys := batch
		batchSize := len(batch)
		for {
			jqlQ := buildJQL(currentKeys)
			if jqlQ == "" {
				return nil, 0, nil
			}
			var raws []IssueRaw
			var total int
			var searchErr error

			fetchPage := func(jqlPage string, page int) ([]IssueRaw, int, error) {
				resp, err := c.Search(ctx, jqlPage, page, 100, childFields)
				if err != nil {
					return nil, 0, err
				}
				return resp.Issues, resp.Total, nil
			}

			if fetchAll {
				for page := 1; ; page++ {
					pageRaws, pageTotal, err := fetchPage(jqlQ, page)
					if err != nil {
						if page == 1 {
							searchErr = err
						}
						break
					}
					total = pageTotal
					raws = append(raws, pageRaws...)
					if len(raws) >= total {
						break
					}
				}
			} else {
				raws, total, searchErr = fetchPage(jqlQ, 1)
			}

			if searchErr == nil {
				return raws, total, nil
			}
			// On 400 (bad request / clause limit): halve and retry.
			msg := searchErr.Error()
			if strings.Contains(msg, "bad request") && batchSize > minBatch {
				batchSize = batchSize / 2
				if batchSize < minBatch {
					batchSize = minBatch
				}
				currentKeys = currentKeys[:batchSize]
				continue
			}
			return nil, 0, searchErr
		}
	}

	// Process parentKeys in batches.
	for start := 0; start < len(parentKeys); start += maxBatch {
		end := start + maxBatch
		if end > len(parentKeys) {
			end = len(parentKeys)
		}
		batch := parentKeys[start:end]

		raws, total, err := searchRaw(batch)
		if err != nil {
			if !gotAny {
				firstErr = err
			}
			continue // fail-soft: skip this batch
		}
		gotAny = true

		// epicLinkClassic: if no results, try parentNextGen fallback.
		if strategy == "epicLinkClassic" && len(raws) == 0 {
			fallbackJQL := `parent in (` + strings.Join(batch, ",") + `)` + sinceClause
			if raws2, total2, err2 := func() ([]IssueRaw, int, error) {
				var r []IssueRaw
				var t int
				fetchPage2 := func(page int) ([]IssueRaw, int, error) {
					resp, err := c.Search(ctx, fallbackJQL, page, 100, childFields)
					if err != nil {
						return nil, 0, err
					}
					return resp.Issues, resp.Total, nil
				}
				if fetchAll {
					for page := 1; ; page++ {
						pr, pt, pe := fetchPage2(page)
						if pe != nil {
							break
						}
						t = pt
						r = append(r, pr...)
						if len(r) >= t {
							break
						}
					}
				} else {
					r, t, _ = fetchPage2(1)
				}
				return r, t, nil
			}(); err2 == nil && len(raws2) > 0 {
				raws = raws2
				total = total2
			}
		}

		totalCount += total
		for _, raw := range raws {
			node := nodeFromRaw(raw)
			pk := parentKeyForChild(raw, parentKeySet, strategy, hf)
			if pk == "" {
				continue // can't attribute — skip
			}
			result[pk] = append(result[pk], node)
		}
	}

	if !gotAny && firstErr != nil {
		return nil, 0, firstErr
	}
	return result, totalCount, nil
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
