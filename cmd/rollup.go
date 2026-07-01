package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// ShowRollupFlags holds parsed flag values for show rollup.
type ShowRollupFlags struct {
	Profile string
	JSON    bool
	All     bool   // --all: fetch all children, overrides Limit
	Limit   int    // --limit N: max children per level (default 100); ignored when All is true
	Depth   int    // 1 = subject + L1; 2 = subject + L1 + L2
	List    bool   // also print per-child table
	JQL     string // --jql: aggregate over arbitrary JQL result set instead of a hierarchy subject
	Sprint  int    // --sprint <id>: aggregate over issues in a sprint (translated to JQL "sprint = <id>")
	GroupBy string // --group-by: currently only "assignee"; empty = no grouping (existing behavior)
}

// NewShowRollupCmd builds the "show rollup" subcommand.
func NewShowRollupCmd() *cobra.Command {
	var flags ShowRollupFlags
	c := &cobra.Command{
		Use:   "rollup [<KEY>]",
		Short: "Aggregate time + story-point estimates from an issue's children",
		Long: `Walks the direct children of any issue and aggregates originalEstimate,
remainingEstimate, timeSpent, and Story Points.

Works on any issue type:
  Epic       — children are its Stories/Tasks via "Epic Link"
  Initiative — children are its Epics via the Parent Link custom field
  Other      — children are subtasks via the typed parent relationship

By default only the immediate children (depth 1) are fetched.  Use --depth 2
to also aggregate the grandchildren of each L1 child.  If the hierarchy goes
deeper, a hint is shown.

Use --list to print a per-child breakdown table beneath the summary.

Use --jql or --sprint to aggregate over an arbitrary set of issues instead of
a hierarchy.

Add --group-by to break down the totals:
  assignee        — group by person (requires --jql or --sprint)
  status          — group by status name (e.g. "In Progress", "Pending Review")
  statusCategory  — group by status category (To Do, In Progress, Done)

In hierarchy mode, --group-by status / statusCategory replaces the per-level
breakdown with a per-status breakdown of the direct children.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hasKey := len(args) == 1
			hasJQL := flags.JQL != ""
			hasSprint := flags.Sprint != 0

			// Mutual exclusion.
			n := 0
			if hasKey {
				n++
			}
			if hasJQL {
				n++
			}
			if hasSprint {
				n++
			}
			if n > 1 {
				return fmt.Errorf("show rollup: <KEY>, --jql, and --sprint are mutually exclusive — choose one")
			}
			if n == 0 {
				return fmt.Errorf("show rollup requires <KEY>, --jql, or --sprint — run: jiracli show rollup --help")
			}

			// --group-by validation.
			if flags.GroupBy != "" {
				switch flags.GroupBy {
				case "assignee", "status", "statusCategory":
					// ok
				default:
					return fmt.Errorf("--group-by: supported values are 'assignee', 'status', 'statusCategory' — got %q", flags.GroupBy)
				}
			}
			// 'assignee' was JQL/sprint-only; 'status' and 'statusCategory' work everywhere.
			if flags.GroupBy == "assignee" && hasKey {
				return fmt.Errorf("--group-by=assignee requires --jql or --sprint")
			}

			var ref string
			if hasKey {
				ref = args[0]
			}
			result, err := ShowRollup(cmd.Context(), flags, ref)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.All, "all", false, "Fetch all children, bypassing the --limit cap")
	c.Flags().IntVar(&flags.Limit, "limit", 100, "Max children to fetch per level (default 100); use --all to fetch everything")
	c.Flags().IntVar(&flags.Depth, "depth", 1, "Levels to aggregate: 1 = direct children only, 2 = children + their children")
	c.Flags().BoolVar(&flags.List, "list", false, "Print a per-child breakdown table beneath the summary")
	c.Flags().StringVar(&flags.JQL, "jql", "", "Aggregate time tracking over JQL results instead of a hierarchy. Mutex with <KEY> and --sprint.")
	c.Flags().IntVar(&flags.Sprint, "sprint", 0, "Aggregate over issues in this sprint id. Mutex with <KEY> and --jql.")
	c.Flags().StringVar(&flags.GroupBy, "group-by", "", "Group rollup rows by this dimension. Supported: 'assignee' (JQL/sprint only), 'status', 'statusCategory'. Rows show Count/Planned/Remaining/Spent/SP.")
	return c
}

// ShowRollup is the Layer 1 implementation for show rollup.
func ShowRollup(ctx context.Context, flags ShowRollupFlags, ref string) (string, error) {
	// Validate --group-by before any I/O so the Layer 1 function is self-contained.
	if flags.GroupBy != "" {
		switch flags.GroupBy {
		case "assignee", "status", "statusCategory":
			// ok
		default:
			return "", fmt.Errorf("--group-by: supported values are 'assignee', 'status', 'statusCategory' — got %q", flags.GroupBy)
		}
	}
	hasKey := ref != ""
	if flags.GroupBy == "assignee" && hasKey {
		return "", fmt.Errorf("--group-by=assignee requires --jql or --sprint")
	}

	// JQL / sprint path — no hierarchy subject needed.
	if flags.JQL != "" || flags.Sprint != 0 {
		return showRollupJQL(ctx, flags)
	}
	// Resolve effective fetch limit: --all overrides --limit (0 = no cap in fetchNodes).
	limit := flags.Limit
	if flags.All {
		limit = 0
	}

	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefIssue {
		return "", fmt.Errorf("show rollup requires a plain issue key — got %q", ref)
	}
	key := parsed.Key

	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	_ = cache.NewStore(entry)

	spField := entry.Hierarchy.StoryPointsField
	epicLinkField := entry.Hierarchy.EpicLinkField
	parentLinkField := entry.Hierarchy.ParentLinkField

	// Guard: if hierarchy fields are not configured, JQL will be wrong.
	if epicLinkField == "" && parentLinkField == "" {
		return "", fmt.Errorf(
			"hierarchy fields not configured for profile %q — run: jiracli config hierarchy --rediscover",
			entry.Profile,
		)
	}

	depth := flags.Depth
	if depth < 1 {
		depth = 1
	}
	if depth > 2 {
		depth = 2 // guard — deeper would need recursive fetch, not yet supported
	}
	// Fields to request when fetching children.
	childFields := []string{"summary", "status", "issuetype", "assignee", "timetracking", "subtasks"}
	if epicLinkField != "" {
		childFields = append(childFields, epicLinkField)
	}
	if parentLinkField != "" {
		childFields = append(childFields, parentLinkField)
	}
	if spField != "" {
		childFields = append(childFields, spField)
	}

	// Fetch the subject.
	subjectFields := jira.DefaultIssueFields
	for _, f := range []string{epicLinkField, parentLinkField, spField} {
		if f != "" && !strings.Contains(subjectFields, f) {
			subjectFields += "," + f
		}
	}
	subjectRaw, err := client.GetIssue(ctx, key, subjectFields, false)
	if err != nil {
		return "", err
	}

	// Derive child JQL from subject issue type.
	childJQL := jira.ChildJQL(subjectRaw.Fields.IssueType.Name, key, epicLinkField, parentLinkField)

	// Fetch L1 children.
	l1Nodes, l1Total, l1Truncated, err := fetchNodes(ctx, client, childJQL, childFields, limit, spField)
	if err != nil {
		return "", fmt.Errorf("fetching children of %s: %w", key, err)
	}

	if len(l1Nodes) == 0 && l1Total == 0 {
		return fmt.Sprintf("%s has no children — nothing to roll up.\n", key), nil
	}

	hasDeeperLevel := false
	// Detect whether any L1 child has its own children (hasDeeperLevel).
	// For subtask-style children, subtasks.len is populated by the search response.
	// For Epic-style children (issue type "Epic"), the Jira search response does NOT
	// include Epic Link children in the subtasks field — they are fetched via JQL.
	// For those nodes, probe a count-only search to determine if children exist.
	for i := range l1Nodes {
		n := &l1Nodes[i]
		if n.ChildrenTotal > 0 {
			// Already known from inline subtasks.
			n.HasChildren = true
			hasDeeperLevel = true
			continue
		}
		if jira.IssueTypeHasEpicLinkChildren(n.IssueType) && epicLinkField != "" {
			// Probe a count-only search to determine if Epic children exist.
			probeJQL := jira.ChildJQL(n.IssueType, n.Key, epicLinkField, parentLinkField)
			resp, probeErr := client.Search(ctx, probeJQL, 1, 1, []string{"key"})
			if probeErr == nil && resp.Total > 0 {
				n.ChildrenTotal = resp.Total
				n.HasChildren = true
				hasDeeperLevel = true
			}
		}
	}

	// --group-by status / statusCategory in hierarchy mode — show a per-status
	// breakdown table for each fetched level (L1, and L2 when depth >= 2).
	if flags.GroupBy == "status" || flags.GroupBy == "statusCategory" {
		var l2Nodes []jira.RollupNode
		var l2Total int
		var l2Truncated bool
		if depth >= 2 && hasDeeperLevel {
			for _, l1 := range l1Nodes {
				if l1.ChildrenTotal == 0 {
					continue
				}
				l2JQL := jira.ChildJQL(l1.IssueType, l1.Key, epicLinkField, parentLinkField)
				nodes, total, trunc, fetchErr := fetchNodes(ctx, client, l2JQL, childFields, limit, spField)
				if fetchErr != nil {
					l2Truncated = true
					continue
				}
				l2Nodes = append(l2Nodes, nodes...)
				l2Total += total
				if trunc {
					l2Truncated = true
				}
			}
		}

		levels := []groupedLevel{
			buildGroupedLevel(l1Nodes, l1Total, l1Truncated, flags.GroupBy, "Level 1"),
		}
		if len(l2Nodes) > 0 || l2Total > 0 {
			levels = append(levels, buildGroupedLevel(l2Nodes, l2Total, l2Truncated, flags.GroupBy, "Level 2"))
		}

		if flags.List {
			levels[0].nodes = l1Nodes
			if len(levels) > 1 {
				levels[1].nodes = l2Nodes
			}
		}

		if flags.JSON {
			var out strings.Builder
			for _, lv := range levels {
				t := jira.RollupTree{
					SubjectKey:       key,
					SubjectIssueType: subjectRaw.Fields.IssueType.Name,
					SubjectSummary:   subjectRaw.Fields.Summary,
					SubjectRow:       jira.SubjectRowFromRaw(subjectRaw, spField),
					Rows:             lv.rows,
					Nodes:            lv.nodes,
					HasDeeperLevel:   hasDeeperLevel,
					MaxFetchedDepth:  depth,
					GroupBy:          flags.GroupBy,
				}
				data, err := json.Marshal(t)
				if err != nil {
					return "", fmt.Errorf("marshal rollup: %w", err)
				}
				out.Write(data)
				out.WriteByte('\n')
			}
			return out.String(), nil
		}
		return renderRollupHierarchyGrouped(subjectRaw, levels, flags.GroupBy, spField), nil
	}

	// Optionally fetch L2 children.
	var l2Nodes []jira.RollupNode
	var l2Total int
	var l2Truncated bool
	if depth >= 2 && hasDeeperLevel {
		for _, l1 := range l1Nodes {
			if l1.ChildrenTotal == 0 {
				continue
			}
			l2JQL := jira.ChildJQL(l1.IssueType, l1.Key, epicLinkField, parentLinkField)
			nodes, total, trunc, fetchErr := fetchNodes(ctx, client, l2JQL, childFields, limit, spField)
			if fetchErr != nil {
				// fail-soft: record truncation but continue
				l2Truncated = true
				continue
			}
			l2Nodes = append(l2Nodes, nodes...)
			l2Total += total
			if trunc {
				l2Truncated = true
			}
		}
	}

	// Build level labels using issue-type counts when available.
	l1Row := jira.AggregateNodes(l1Nodes, "", l1Truncated)
	l1Row.TotalCount = l1Total
	if l1Truncated {
		l1Row.Label = fmt.Sprintf("Level 1 — %d of %d fetched", len(l1Nodes), l1Total)
	} else {
		l1Row.Label = "Level 1 — " + typeCountLabel(l1Row.IssueTypeCounts, l1Total)
	}

	tree := jira.RollupTree{
		SubjectKey:       key,
		SubjectIssueType: subjectRaw.Fields.IssueType.Name,
		SubjectSummary:   subjectRaw.Fields.Summary,
		SubjectRow:       jira.SubjectRowFromRaw(subjectRaw, spField),
		HasDeeperLevel:   hasDeeperLevel,
		MaxFetchedDepth:  1,
	}
	tree.Rows = append(tree.Rows, l1Row)

	if depth >= 2 && hasDeeperLevel {
		l2Row := jira.AggregateNodes(l2Nodes, "", l2Truncated)
		l2Row.TotalCount = l2Total
		if l2Truncated {
			l2Row.Label = fmt.Sprintf("Level 2 — %d of %d fetched", len(l2Nodes), l2Total)
		} else {
			l2Row.Label = "Level 2 — " + typeCountLabel(l2Row.IssueTypeCounts, l2Total)
		}
		tree.Rows = append(tree.Rows, l2Row)
		tree.MaxFetchedDepth = 2
	}

	if flags.List {
		tree.Nodes = l1Nodes
	}

	if flags.JSON {
		data, err := json.Marshal(tree)
		if err != nil {
			return "", fmt.Errorf("marshal rollup: %w", err)
		}
		return string(data) + "\n", nil
	}

	return renderRollupTree(subjectRaw, tree, hasDeeperLevel, depth, l1Truncated, spField), nil
}

// fetchNodes pages through a JQL search and returns RollupNodes.
// limit controls how many nodes to fetch: 0 means fetch all (no cap); any
// positive value stops once that many nodes have been collected.
func fetchNodes(ctx context.Context, client *jira.Client, jql string, fields []string, limit int, spField string) (nodes []jira.RollupNode, total int, truncated bool, err error) {
	const pageSize = 100
	page := 1
	for {
		resp, searchErr := client.Search(ctx, jql, page, pageSize, fields)
		if searchErr != nil {
			if len(nodes) > 0 {
				return nodes, total, true, nil // fail-soft
			}
			return nil, 0, false, searchErr
		}
		if page == 1 {
			total = resp.Total
		}
		for _, raw := range resp.Issues {
			n := jira.RollupNodeFromRaw(raw, spField)
			// Count subtasks as childrenTotal indicator.
			n.ChildrenTotal = len(raw.Fields.Subtasks)
			nodes = append(nodes, n)
			if limit > 0 && len(nodes) >= limit {
				break
			}
		}
		hitLimit := limit > 0 && len(nodes) >= limit
		startAt := (page-1)*pageSize + len(resp.Issues)
		if hitLimit || startAt >= resp.Total || len(resp.Issues) == 0 {
			break
		}
		page++
	}
	if total > len(nodes) {
		truncated = true
	}
	return nodes, total, truncated, nil
}

// groupedLevel holds a status-grouped view of one hierarchy level for rendering.
type groupedLevel struct {
	label     string            // e.g. "Level 1 — 6 Epics"
	rows      []jira.RollupRow  // status-grouped rows, sorted
	nodes     []jira.RollupNode // populated only when --list; nil otherwise
	total     int               // server-reported total count for this level
	truncated bool
}

// buildGroupedLevel groups nodes by the given dimension and returns a groupedLevel.
// levelName is "Level 1" or "Level 2".
func buildGroupedLevel(nodes []jira.RollupNode, total int, truncated bool, groupBy, levelName string) groupedLevel {
	order := []string{}
	groups := map[string][]jira.RollupNode{}
	for _, n := range nodes {
		k := nodeGroupKey(n, groupBy)
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], n)
	}
	rows := make([]jira.RollupRow, 0, len(order))
	for _, k := range order {
		row := jira.AggregateNodes(groups[k], k, false)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, statusRowLess(rows, groupBy == "statusCategory"))

	// Build the level label from issue-type counts.
	var label string
	if truncated {
		label = fmt.Sprintf("%s — %d of %d fetched", levelName, len(nodes), total)
	} else {
		itCounts := map[string]int{}
		for _, n := range nodes {
			itCounts[n.IssueType]++
		}
		label = levelName + " — " + typeCountLabel(itCounts, total)
	}

	return groupedLevel{
		label:     label,
		rows:      rows,
		total:     total,
		truncated: truncated,
	}
}

// nodeGroupKey returns the grouping key for a node given the group-by dimension.
// "" group-by means no grouping; caller MUST check before calling.
func nodeGroupKey(n jira.RollupNode, groupBy string) string {
	switch groupBy {
	case "assignee":
		if n.Assignee == "" {
			return "Unassigned"
		}
		return n.Assignee
	case "status":
		if n.Status == "" {
			return "(no status)"
		}
		return n.Status
	case "statusCategory":
		if n.StatusCategory == "" {
			return "(no category)"
		}
		return n.StatusCategory
	default:
		return ""
	}
}

// statusRank maps a status label to a sort bucket:
// blocked=0, open/to-do/backlog=1, in-progress/review/test/pending=2, done/closed/resolved=3.
// categoryMode=true treats label as a status category name; false uses heuristic matching.
func statusRank(label string, categoryMode bool) int {
	if categoryMode {
		switch label {
		case "To Do":
			return 0
		case "In Progress":
			return 1
		case "Done":
			return 2
		}
		return 3 // unknown last
	}
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "blocked"):
		return 0
	case strings.Contains(lower, "open"), strings.Contains(lower, "to do"),
		strings.Contains(lower, "backlog"), strings.Contains(lower, "ready"):
		return 1
	case strings.Contains(lower, "in progress"), strings.Contains(lower, "review"),
		strings.Contains(lower, "test"), strings.Contains(lower, "deploy"),
		strings.Contains(lower, "pending"):
		return 2
	case strings.Contains(lower, "done"), strings.Contains(lower, "closed"),
		strings.Contains(lower, "resolved"):
		return 3
	}
	return 2 // unknown — group with in-progress
}

// statusRowLess orders RollupRows by status meaning: open → in-progress → done.
// Within each bucket rows are sorted by label.
func statusRowLess(rows []jira.RollupRow, categoryMode bool) func(i, j int) bool {
	return func(i, j int) bool {
		ri, rj := statusRank(rows[i].Label, categoryMode), statusRank(rows[j].Label, categoryMode)
		if ri != rj {
			return ri < rj
		}
		return rows[i].Label < rows[j].Label
	}
}

// showRollupJQL aggregates time-tracking over an arbitrary JQL result set or sprint,
// optionally grouped by a dimension.
func showRollupJQL(ctx context.Context, flags ShowRollupFlags) (string, error) {
	// Resolve effective fetch limit: --all overrides --limit (0 = no cap in fetchNodes).
	limit := flags.Limit
	if flags.All {
		limit = 0
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	jql := flags.JQL
	if flags.Sprint != 0 {
		jql = fmt.Sprintf("sprint = %d", flags.Sprint)
	}
	spField := entry.Hierarchy.StoryPointsField

	fields := []string{"summary", "status", "issuetype", "assignee", "timetracking"}
	if spField != "" {
		fields = append(fields, spField)
	}

	nodes, total, truncated, err := fetchNodes(ctx, client, jql, fields, limit, spField)
	if err != nil {
		return "", fmt.Errorf("fetching issues for rollup: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Sprintf("no issues matched: %s\n", jql), nil
	}

	// Build the subject label used in headers.
	subjectKey := "(jql)"
	if flags.Sprint != 0 {
		subjectKey = fmt.Sprintf("(sprint %d)", flags.Sprint)
	}

	var rows []jira.RollupRow

	if flags.GroupBy != "" {
		order := []string{}
		groups := map[string][]jira.RollupNode{}
		for _, n := range nodes {
			key := nodeGroupKey(n, flags.GroupBy)
			if _, seen := groups[key]; !seen {
				order = append(order, key)
			}
			groups[key] = append(groups[key], n)
		}
		for _, key := range order {
			row := jira.AggregateNodes(groups[key], key, false)
			rows = append(rows, row)
		}
		if flags.GroupBy == "assignee" {
			sort.SliceStable(rows, func(i, j int) bool {
				if rows[i].OriginalEstimateSecs != rows[j].OriginalEstimateSecs {
					return rows[i].OriginalEstimateSecs > rows[j].OriginalEstimateSecs
				}
				if rows[i].TimeSpentSecs != rows[j].TimeSpentSecs {
					return rows[i].TimeSpentSecs > rows[j].TimeSpentSecs
				}
				return rows[i].Label < rows[j].Label
			})
		} else {
			sort.SliceStable(rows, statusRowLess(rows, flags.GroupBy == "statusCategory"))
		}
		// Append total row.
		totalRow := jira.AggregateNodes(nodes, "Total", truncated)
		totalRow.TotalCount = total
		rows = append(rows, totalRow)
	} else {
		totalRow := jira.AggregateNodes(nodes, fmt.Sprintf("Total — %d issues", total), truncated)
		totalRow.TotalCount = total
		rows = append(rows, totalRow)
	}

	tree := jira.RollupTree{
		SubjectKey:       subjectKey,
		SubjectIssueType: "", // empty signals JQL/sprint mode to renderer
		SubjectSummary:   jql,
		SubjectRow:       jira.RollupRow{}, // no own TT for a virtual subject
		Rows:             rows,
		HasDeeperLevel:   false,
		MaxFetchedDepth:  1,
		GroupBy:          flags.GroupBy,
	}
	if flags.List {
		tree.Nodes = nodes
	}

	if flags.JSON {
		data, err := json.Marshal(tree)
		if err != nil {
			return "", fmt.Errorf("marshal rollup: %w", err)
		}
		return string(data) + "\n", nil
	}

	return renderRollupJQL(tree), nil
}

// typeCountLabel builds a human-readable count label from IssueTypeCounts.
// When all children are the same type it returns e.g. "8 Stories".
// When mixed it returns e.g. "3 Epics, 2 Bugs, 1 Story".
// total is the server-reported total (may exceed len of the fetched map when truncated).
func typeCountLabel(counts map[string]int, total int) string {
	if len(counts) == 0 {
		if total == 1 {
			return "1 child"
		}
		return fmt.Sprintf("%d children", total)
	}
	// Stable order: sort by count descending, then name ascending.
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	// Simple insertion sort — N is tiny (< 10 distinct types in practice).
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0; j-- {
			a, b := pairs[j-1], pairs[j]
			if a.v < b.v || (a.v == b.v && a.k > b.k) {
				pairs[j-1], pairs[j] = b, a
			}
		}
	}
	// Cap at 2 most-common types; fold the rest into a count.
	const maxTypes = 2
	parts := make([]string, 0, maxTypes+1)
	remainder := 0
	for i, p := range pairs {
		label := p.k
		if p.v != 1 {
			label += "s"
		}
		if i < maxTypes {
			parts = append(parts, fmt.Sprintf("%d %s", p.v, label))
		} else {
			remainder += p.v
		}
	}
	if remainder > 0 {
		parts = append(parts, fmt.Sprintf("%d more", remainder))
	}
	result := strings.Join(parts, ", ")
	// When fetched < total, prepend totals so the row label stays accurate.
	if total > 0 {
		fetched := 0
		for _, p := range pairs {
			fetched += p.v
		}
		if fetched < total {
			result = fmt.Sprintf("%d of %d: %s", fetched, total, result)
		}
	}
	return result
}

// renderRollupTree produces plain-text output for the RollupTree.
func renderRollupTree(subjectRaw jira.IssueRaw, tree jira.RollupTree, hasDeeperLevel bool, depth int, l1Truncated bool, spField string) string {
	var sb strings.Builder
	clr := colorsEnabled()

	// ── Header ──────────────────────────────────────────────────────────────
	typeBadge := colorIssueType(subjectRaw.Fields.IssueType.Name)
	statusBadge := colorStatusName(subjectRaw.Fields.Status.Name)
	priority := "—"
	if subjectRaw.Fields.Priority != nil {
		priority = subjectRaw.Fields.Priority.Name
	}
	if clr {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n",
			typeBadge, jira.Bold(tree.SubjectKey, true), statusBadge, colorPriority(priority))
	} else {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n", typeBadge, tree.SubjectKey, subjectRaw.Fields.Status.Name, priority)
	}
	fmt.Fprintf(&sb, "%s\n", jira.BoldFgW(tree.SubjectSummary, clr))

	fixVersions := make([]string, 0, len(subjectRaw.Fields.FixVersions))
	for _, fv := range subjectRaw.Fields.FixVersions {
		fixVersions = append(fixVersions, fv.Name)
	}
	if len(fixVersions) > 0 {
		fmt.Fprintf(&sb, "Fix Versions: %s\n", strings.Join(fixVersions, ", "))
	}
	sb.WriteByte('\n')

	// ── Summary table ────────────────────────────────────────────────────────
	// Columns: Planned · Remaining · Spent · SP
	const (
		colW   = 10
		labelW = 36
	)
	rule := strings.Repeat("─", labelW+2+(colW+2)*3+(colW+2)) + "\n"

	fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s\n",
		labelW, "",
		colW, "Planned",
		colW, "Remaining",
		colW, "Spent",
		colW, "SP")
	sb.WriteString(rule)

	spCell := func(r jira.RollupRow, showPartial bool) string {
		if r.StoryPoints == 0 && r.PointedCount == 0 {
			return "—"
		}
		s := fmt.Sprintf("%g", r.StoryPoints)
		if showPartial && r.PointedCount > 0 && r.PointedCount < r.TotalCount {
			s += fmt.Sprintf(" (%d/%d)", r.PointedCount, r.TotalCount)
		}
		return s
	}
	printRow := func(label string, r jira.RollupRow, partial bool, showPartialSP bool) {
		lbl := label
		if partial {
			lbl += " ⚠"
		}
		fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s\n",
			labelW, jira.TruncateString(lbl, labelW),
			colW, dashIfZero(r.OriginalEstimateSecs),
			colW, dashIfZero(r.RemainingEstimateSecs),
			colW, dashIfZero(r.TimeSpentSecs),
			colW, spCell(r, showPartialSP))
	}

	ownLabel := subjectRaw.Fields.IssueType.Name + " " + tree.SubjectKey + " (own)"
	printRow(ownLabel, tree.SubjectRow, false, true)

	for _, row := range tree.Rows {
		printRow(row.Label, row, row.Truncated, true)
	}

	// Total = own + ALL levels (not just deepest).
	total := tree.SubjectRow
	for _, row := range tree.Rows {
		total.OriginalEstimateSecs += row.OriginalEstimateSecs
		total.RemainingEstimateSecs += row.RemainingEstimateSecs
		total.TimeSpentSecs += row.TimeSpentSecs
		total.StoryPoints += row.StoryPoints
		total.PointedCount += row.PointedCount
		total.TotalCount += row.TotalCount
	}
	totalLabel := "Total"
	if len(tree.Rows) > 1 {
		totalLabel = "Total (all levels)"
	}
	sb.WriteString(rule)
	// Total SP: no partial fraction — cross-level counts are not comparable.
	printRow(totalLabel, total, false, false)
	sb.WriteByte('\n')

	// Progress bar — only when total planned > 0
	if total.OriginalEstimateSecs > 0 {
		bar := jira.FormatProgressBar(total.TimeSpentSecs, total.OriginalEstimateSecs, 24, clr)
		fmt.Fprintf(&sb, "%s\n\n", bar)
	}

	// ── Depth hints ──────────────────────────────────────────────────────────
	if hasDeeperLevel && depth < 2 {
		fmt.Fprintf(&sb, "  → pass --depth 2 to also aggregate grandchildren\n\n")
	} else if depth >= 2 {
		fmt.Fprintf(&sb, "  (depth 2 is the maximum — run show rollup on individual children to go deeper)\n\n")
	} else {
		fmt.Fprintf(&sb, "  (leaf level reached — no deeper children)\n\n")
	}

	// ── --list per-child table ───────────────────────────────────────────────
	if len(tree.Nodes) > 0 {
		sb.WriteString(sectionLabel("Children:") + "\n")
		const keyW = 14
		const sumW = 52 // wider summary column
		listRule := strings.Repeat("─", keyW+2+sumW+2+(colW+2)*4) + "\n"
		fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
			keyW, "Key",
			sumW, "Summary",
			colW, "Planned",
			colW, "Remaining",
			colW, "Spent",
			colW, "SP")
		sb.WriteString("  " + listRule)

		// Detect a shared prefix so we can elide the middle instead of the tail.
		// Only activate when the common prefix is long enough to actually obscure
		// the unique part — threshold: prefix longer than half the column.
		summaries := make([]string, len(tree.Nodes))
		for i, n := range tree.Nodes {
			summaries[i] = n.Summary
		}
		commonPfx := jira.CommonRunePrefix(summaries)
		useMidElide := commonPfx > sumW/2
		const midPrefixKeep = 10 // runes of shared prefix to retain for context

		for _, n := range tree.Nodes {
			sp := "—"
			if n.StoryPoints != nil {
				sp = fmt.Sprintf("%g", *n.StoryPoints)
			}
			// Truncate summary; when nodes share a long prefix, elide the middle
			// so the unique tail is always visible.
			// Reserve 2 runes (space + ▸) for the HasChildren indicator.
			effectiveW := sumW
			if n.HasChildren {
				effectiveW = sumW - 2
			}
			var sumTrunc string
			if useMidElide {
				sumTrunc = jira.TruncateMidPrefix(n.Summary, effectiveW, midPrefixKeep)
			} else {
				sumTrunc = jira.TruncateString(n.Summary, effectiveW)
			}
			if n.HasChildren {
				sumTrunc += " ▸"
			}
			fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
				keyW, n.Key,
				sumW, sumTrunc,
				colW, dashIfZero(n.OriginalEstimateSecs),
				colW, dashIfZero(n.RemainingEstimateSecs),
				colW, dashIfZero(n.TimeSpentSecs),
				colW, sp)
		}
		if l1Truncated {
			fmt.Fprintf(&sb, "  (first %d shown — pass --limit <n> or --limit 0 for all)\n", len(tree.Nodes))
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}

// renderRollupHierarchyGrouped renders a hierarchy rollup grouped by status or
// statusCategory. One table is emitted per level (L1, optionally L2), each
// preceded by a section label and followed by a progress bar and, when
// --list is set, a per-child breakdown table.
func renderRollupHierarchyGrouped(subjectRaw jira.IssueRaw, levels []groupedLevel, groupBy string, spField string) string {
	var sb strings.Builder
	clr := colorsEnabled()

	// ── Subject header ───────────────────────────────────────────────────────
	typeBadge := colorIssueType(subjectRaw.Fields.IssueType.Name)
	statusBadge := colorStatusName(subjectRaw.Fields.Status.Name)
	priority := "—"
	if subjectRaw.Fields.Priority != nil {
		priority = subjectRaw.Fields.Priority.Name
	}
	if clr {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n",
			typeBadge, jira.Bold(subjectRaw.Key, true), statusBadge, colorPriority(priority))
	} else {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n",
			typeBadge, subjectRaw.Key, subjectRaw.Fields.Status.Name, priority)
	}
	fmt.Fprintf(&sb, "%s\n", jira.BoldFgW(subjectRaw.Fields.Summary, clr))
	fixVersions := make([]string, 0, len(subjectRaw.Fields.FixVersions))
	for _, fv := range subjectRaw.Fields.FixVersions {
		fixVersions = append(fixVersions, fv.Name)
	}
	if len(fixVersions) > 0 {
		fmt.Fprintf(&sb, "Fix Versions: %s\n", strings.Join(fixVersions, ", "))
	}
	sb.WriteByte('\n')

	// Column layout — shared across all levels.
	const (
		colW   = 10
		countW = 7
		labelW = 30
	)
	rule := strings.Repeat("─", labelW+2+countW+2+(colW+2)*3+(colW+2)) + "\n"

	dimHeader := "Status"
	if groupBy == "statusCategory" {
		dimHeader = "Status Category"
	}

	spCell := func(r jira.RollupRow) string {
		if r.StoryPoints == 0 && r.PointedCount == 0 {
			return "—"
		}
		return fmt.Sprintf("%g", r.StoryPoints)
	}
	printRow := func(label string, r jira.RollupRow) {
		fmt.Fprintf(&sb, "%-*s  %*d  %*s  %*s  %*s  %*s\n",
			labelW, jira.TruncateString(label, labelW),
			countW, r.TotalCount,
			colW, dashIfZero(r.OriginalEstimateSecs),
			colW, dashIfZero(r.RemainingEstimateSecs),
			colW, dashIfZero(r.TimeSpentSecs),
			colW, spCell(r))
	}

	// ── One block per level ──────────────────────────────────────────────────
	for _, lv := range levels {
		// Section label above the table.
		fmt.Fprintf(&sb, "%s\n", sectionLabel(lv.label))

		// Column header + rule.
		fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s  %*s\n",
			labelW, dimHeader,
			countW, "Count",
			colW, "Planned",
			colW, "Remaining",
			colW, "Spent",
			colW, "SP")
		sb.WriteString(rule)

		// Status rows.
		for _, row := range lv.rows {
			printRow(row.Label, row)
		}

		// Total row: sum all status groups; use server total for count.
		var total jira.RollupRow
		total.TotalCount = lv.total
		for _, row := range lv.rows {
			total.OriginalEstimateSecs += row.OriginalEstimateSecs
			total.RemainingEstimateSecs += row.RemainingEstimateSecs
			total.TimeSpentSecs += row.TimeSpentSecs
			total.StoryPoints += row.StoryPoints
			total.PointedCount += row.PointedCount
		}
		sb.WriteString(rule)
		printRow("Total", total)
		sb.WriteByte('\n')

		// Progress bar for this level.
		if total.OriginalEstimateSecs > 0 {
			bar := jira.FormatProgressBar(total.TimeSpentSecs, total.OriginalEstimateSecs, 24, clr)
			fmt.Fprintf(&sb, "%s\n\n", bar)
		}

		// --list per-child table for this level.
		if len(lv.nodes) > 0 {
			sb.WriteString(sectionLabel("Children:") + "\n")
			const keyW = 14
			const sumW = 52
			listRule := strings.Repeat("─", keyW+2+sumW+2+(colW+2)*4) + "\n"
			fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
				keyW, "Key",
				sumW, "Summary",
				colW, "Planned",
				colW, "Remaining",
				colW, "Spent",
				colW, "SP")
			sb.WriteString("  " + listRule)
			summaries := make([]string, len(lv.nodes))
			for i, n := range lv.nodes {
				summaries[i] = n.Summary
			}
			commonPfx := jira.CommonRunePrefix(summaries)
			useMidElide := commonPfx > sumW/2
			const midPrefixKeep = 10
			for _, n := range lv.nodes {
				sp := "—"
				if n.StoryPoints != nil {
					sp = fmt.Sprintf("%g", *n.StoryPoints)
				}
				effectiveW := sumW
				if n.HasChildren {
					effectiveW = sumW - 2
				}
				var sumTrunc string
				if useMidElide {
					sumTrunc = jira.TruncateMidPrefix(n.Summary, effectiveW, midPrefixKeep)
				} else {
					sumTrunc = jira.TruncateString(n.Summary, effectiveW)
				}
				if n.HasChildren {
					sumTrunc += " ▸"
				}
				fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
					keyW, n.Key,
					sumW, sumTrunc,
					colW, dashIfZero(n.OriginalEstimateSecs),
					colW, dashIfZero(n.RemainingEstimateSecs),
					colW, dashIfZero(n.TimeSpentSecs),
					colW, sp)
			}
			if lv.truncated {
				fmt.Fprintf(&sb, "  (first %d shown — pass --limit <n> or --limit 0 for all)\n", len(lv.nodes))
			}
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// dashIfZero returns jira.FormatSeconds(secs) or "—" when secs is 0.
func dashIfZero(secs int64) string {
	if secs == 0 {
		return "—"
	}
	return jira.FormatSeconds(secs)
}

// dashIfZeroFloat returns fmt.Sprintf("%g", f) or "—" when f is 0.
func dashIfZeroFloat(f float64) string {
	if f == 0 {
		return "—"
	}
	return fmt.Sprintf("%g", f)
}

// renderRollupJQL produces plain-text output for the JQL/sprint rollup mode.
// It replaces the subject-header block with a one-line title and renders rows
// as a flat table (one row per assignee group, or a single Total row).
func renderRollupJQL(tree jira.RollupTree) string {
	var sb strings.Builder
	clr := colorsEnabled()

	// How many issues are represented by the last row (the Total row).
	totalIssues := 0
	if len(tree.Rows) > 0 {
		totalIssues = tree.Rows[len(tree.Rows)-1].TotalCount
	}

	header := fmt.Sprintf("Rollup: %s  (%d issues)", tree.SubjectSummary, totalIssues)
	if clr {
		fmt.Fprintf(&sb, "%s\n\n", jira.BoldFgW(header, true))
	} else {
		fmt.Fprintf(&sb, "%s\n\n", header)
	}

	const (
		colW   = 10
		countW = 7
		labelW = 36
	)

	// Choose header label based on grouping dimension.
	dimHeader := "Assignee / Group"
	switch tree.GroupBy {
	case "status":
		dimHeader = "Status"
	case "statusCategory":
		dimHeader = "Status Category"
	}
	grouped := tree.GroupBy != ""

	var rule string
	if grouped {
		rule = strings.Repeat("─", labelW+2+countW+2+(colW+2)*3+(colW+2)) + "\n"
		fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s  %*s\n",
			labelW, dimHeader,
			countW, "Count",
			colW, "Planned",
			colW, "Remaining",
			colW, "Spent",
			colW, "SP")
	} else {
		rule = strings.Repeat("─", labelW+2+(colW+2)*3+(colW+2)) + "\n"
		fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s\n",
			labelW, dimHeader,
			colW, "Planned",
			colW, "Remaining",
			colW, "Spent",
			colW, "SP")
	}
	sb.WriteString(rule)

	printRow := func(label string, r jira.RollupRow) {
		sp := dashIfZeroFloat(r.StoryPoints)
		if grouped {
			fmt.Fprintf(&sb, "%-*s  %*d  %*s  %*s  %*s  %*s\n",
				labelW, jira.TruncateString(label, labelW),
				countW, r.TotalCount,
				colW, dashIfZero(r.OriginalEstimateSecs),
				colW, dashIfZero(r.RemainingEstimateSecs),
				colW, dashIfZero(r.TimeSpentSecs),
				colW, sp)
		} else {
			fmt.Fprintf(&sb, "%-*s  %*s  %*s  %*s  %*s\n",
				labelW, jira.TruncateString(label, labelW),
				colW, dashIfZero(r.OriginalEstimateSecs),
				colW, dashIfZero(r.RemainingEstimateSecs),
				colW, dashIfZero(r.TimeSpentSecs),
				colW, sp)
		}
	}

	// All rows except the last (Total) are regular group rows.
	// If there's only one row it's the Total-only path (no group-by).
	if len(tree.Rows) == 1 {
		printRow(tree.Rows[0].Label, tree.Rows[0])
	} else {
		for i, row := range tree.Rows {
			if i == len(tree.Rows)-1 {
				sb.WriteString(rule)
			}
			printRow(row.Label, row)
		}
	}
	sb.WriteByte('\n')

	// --list per-issue table.
	if len(tree.Nodes) > 0 {
		sb.WriteString(sectionLabel("Issues:") + "\n")
		const keyW = 14
		const sumW = 52
		listRule := strings.Repeat("─", keyW+2+sumW+2+(colW+2)*4) + "\n"
		fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
			keyW, "Key",
			sumW, "Summary",
			colW, "Planned",
			colW, "Remaining",
			colW, "Spent",
			colW, "SP")
		sb.WriteString("  " + listRule)
		for _, n := range tree.Nodes {
			sp := "—"
			if n.StoryPoints != nil {
				sp = fmt.Sprintf("%g", *n.StoryPoints)
			}
			fmt.Fprintf(&sb, "  %-*s  %-*s  %*s  %*s  %*s  %*s\n",
				keyW, n.Key,
				sumW, jira.TruncateString(n.Summary, sumW),
				colW, dashIfZero(n.OriginalEstimateSecs),
				colW, dashIfZero(n.RemainingEstimateSecs),
				colW, dashIfZero(n.TimeSpentSecs),
				colW, sp)
		}
		sb.WriteByte('\n')
	}

	fmt.Fprintf(&sb, "→ jiracli show <KEY>  # to drill into any issue\n")
	return sb.String()
}
