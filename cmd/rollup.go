package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// ShowRollupFlags holds parsed flag values for show rollup.
type ShowRollupFlags struct {
	Profile string
	JSON    bool
	All     bool // page through all children instead of capping at 100
	Depth   int  // 1 = subject + L1; 2 = subject + L1 + L2
	List    bool // also print per-child table
}

// NewShowRollupCmd builds the "show rollup" subcommand.
func NewShowRollupCmd() *cobra.Command {
	var flags ShowRollupFlags
	c := &cobra.Command{
		Use:   "rollup <KEY>",
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

Use --list to print a per-child breakdown table beneath the summary.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ShowRollup(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.All, "all", false, "Page through all children (bypass 100-result cap)")
	c.Flags().IntVar(&flags.Depth, "depth", 1, "Levels to aggregate: 1 = direct children only, 2 = children + their children")
	c.Flags().BoolVar(&flags.List, "list", false, "Print a per-child breakdown table beneath the summary")
	return c
}

// ShowRollup is the Layer 1 implementation for show rollup.
func ShowRollup(ctx context.Context, flags ShowRollupFlags, ref string) (string, error) {
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
	l1Nodes, l1Total, l1Truncated, err := fetchNodes(ctx, client, childJQL, childFields, flags.All, spField)
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
			nodes, total, trunc, fetchErr := fetchNodes(ctx, client, l2JQL, childFields, flags.All, spField)
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
func fetchNodes(ctx context.Context, client *jira.Client, jql string, fields []string, all bool, spField string) (nodes []jira.RollupNode, total int, truncated bool, err error) {
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
		}
		startAt := (page-1)*pageSize + len(resp.Issues)
		if startAt >= resp.Total || len(resp.Issues) == 0 || !all {
			break
		}
		page++
	}
	if total > len(nodes) {
		truncated = true
	}
	return nodes, total, truncated, nil
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
				keyW, jira.TruncateString(n.Key, keyW),
				sumW, sumTrunc,
				colW, dashIfZero(n.OriginalEstimateSecs),
				colW, dashIfZero(n.RemainingEstimateSecs),
				colW, dashIfZero(n.TimeSpentSecs),
				colW, sp)
		}
		if l1Truncated {
			fmt.Fprintf(&sb, "  (first 100 shown — pass --all for full list)\n")
		}
		sb.WriteByte('\n')
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
