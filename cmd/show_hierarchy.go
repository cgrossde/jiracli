package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// ShowHierarchyFlags holds parsed flag values for `show hierarchy`.
type ShowHierarchyFlags struct {
	Profile string
	JSON    bool
	All     bool
	Open    bool   // shorthand for --status open
	Status  string // "", "open", "closed", "not-closed"
	Depth   int
	Flat    bool   // emit one row per node (tab-separated) instead of the tree
	Since   string // JQL updated >= date, e.g. "-2w", "-1d", "2024-01-01"
}

// NewShowHierarchyCmd builds the "show hierarchy" subcommand.
func NewShowHierarchyCmd() *cobra.Command {
	var flags ShowHierarchyFlags
	c := &cobra.Command{
		Use:   "hierarchy <KEY>",
		Short: "Walk Initiative → Epic → Subject → Children for an issue",
		Long: `Walk the full hierarchy chain for an issue.

Uses the per-profile hierarchy field IDs configured by 'jiracli setup'.
If the profile has no hierarchy config, this command fails with a corrective
hint — run 'jiracli setup --reconfigure' or 'jiracli config hierarchy'.

Use --depth N to fetch N levels of descendants (default 1 = direct children only).
Use --open to show only non-Done issues (shorthand for --status open).
Use --flat to emit one tab-separated row per node (key, type, status, assignee, summary).
Use --since to show only issues updated within a relative period (e.g. -2w, -1d, 2024-01-01).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ShowHierarchy(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON (one object: the full chain)")
	c.Flags().BoolVar(&flags.All, "all", false, "Fetch all children (bypasses the 100-result default cap)")
	c.Flags().BoolVar(&flags.Open, "open", false, "Show only non-Done issues (shorthand for --status open)")
	c.Flags().StringVar(&flags.Status, "status", "", "Filter children by status category: open, closed, not-closed")
	c.Flags().IntVar(&flags.Depth, "depth", 1, "How many levels of descendants to fetch (1 = direct children only, max 5)")
	c.Flags().BoolVar(&flags.Flat, "flat", false, "Flat output: one tab-separated row per node (key, depth, parentKey, type, status, assignee, summary)")
	c.Flags().StringVar(&flags.Since, "since", "", "Only include issues updated on or after this date (e.g. -2w, -1d, 2024-01-01)")
	return c
}

// ShowHierarchy is the Layer 1 implementation.
func ShowHierarchy(ctx context.Context, flags ShowHierarchyFlags, ref string) (string, error) {
	// --open is a shorthand for --status open; --status takes precedence if both set.
	if flags.Open && flags.Status == "" {
		flags.Status = "open"
	}

	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefIssue {
		return "", fmt.Errorf("show hierarchy requires a plain issue key — got %q", ref)
	}
	entry, _, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	if entry.Hierarchy.EpicLinkField == "" && entry.Hierarchy.ParentLinkField == "" && entry.Hierarchy.PortfolioField == "" {
		return "", fmt.Errorf("hierarchy not configured for profile %q — run: jiracli setup --reconfigure", entry.Profile)
	}
	client := jira.New(entry)
	hf := jira.HierarchyFieldIDs{
		EpicLink:   entry.Hierarchy.EpicLinkField,
		ParentLink: entry.Hierarchy.ParentLinkField,
		Portfolio:  entry.Hierarchy.PortfolioField,
	}
	if flags.Depth < 1 {
		flags.Depth = 1
	}
	if flags.Depth > 5 {
		return "", fmt.Errorf("--depth max is 5 (got %d) — deeper trees risk excessive API calls", flags.Depth)
	}

	// Normalise --since: strip a leading '-' if user wrote e.g. "2w" meaning "-2w".
	since := normaliseSince(flags.Since)

	chain, err := jira.BuildHierarchy(ctx, client, hf, entry.Hierarchy.PortfolioFieldName, parsed.Key, flags.All || flags.JSON, flags.Depth, since)
	if err != nil {
		return "", fmt.Errorf("walking hierarchy from %s: %w", parsed.Key, err)
	}

	// A — warn when depth > 1 and the level-1 children have all-empty subtrees
	// because no ParentLink/Portfolio field is configured.
	if flags.Depth >= 2 && !flags.JSON {
		if hf.ParentLink == "" && hf.Portfolio == "" {
			allEmpty := true
			for i := range chain.Children {
				if len(chain.Children[i].Children) > 0 {
					allEmpty = false
					break
				}
			}
			if allEmpty && len(chain.Children) > 0 {
				return "", fmt.Errorf(
					"--depth %d returned no grandchildren: ParentLink and Portfolio fields are not configured\n"+
						"run: jiracli config hierarchy --rediscover  (or jiracli setup --reconfigure)",
					flags.Depth,
				)
			}
		}
	}

	if flags.JSON {
		// --flat --json: NDJSON one node per line.
		if flags.Flat {
			return renderFlatJSON(chain), nil
		}
		data, err := json.Marshal(chain)
		if err != nil {
			return "", fmt.Errorf("marshal hierarchy: %w", err)
		}
		return string(data) + "\n", nil
	}

	if flags.Flat {
		return jira.RenderHierarchyFlat(chain, flags.Status), nil
	}
	return jira.RenderHierarchy(chain, jira.ColorsEnabled(), flags.Status), nil
}

// normaliseSince ensures the value is in a form Jira accepts for the updated >= predicate.
// Jira accepts relative dates like "-2w", "-1d", "-30m" and absolute ISO dates "2024-01-01".
// Users might omit the leading '-'; we add it when the value looks like a duration unit.
func normaliseSince(s string) string {
	if s == "" {
		return ""
	}
	// Already has a leading '-' or is an absolute date (starts with digit or year).
	if strings.HasPrefix(s, "-") || (len(s) > 0 && s[0] >= '0' && s[0] <= '9') {
		return s
	}
	// Looks like "2w", "1d", "30m" — prepend '-'.
	return "-" + s
}

// renderFlatJSON emits NDJSON: one JSON object per node in DFS order.
func renderFlatJSON(chain jira.HierarchyChain) string {
	var sb strings.Builder
	type flatNode struct {
		Key        string `json:"key"`
		NodeDepth  int    `json:"depth"`
		ParentKey  string `json:"parentKey,omitempty"`
		IssueType  string `json:"issueType"`
		Status     string `json:"status"`
		Assignee   string `json:"assignee,omitempty"`
		Summary    string `json:"summary"`
		IsSubject  bool   `json:"isSubject,omitempty"`
	}
	var walk func(nodes []jira.HierarchyNode, parentKey string, d int)
	walk = func(nodes []jira.HierarchyNode, parentKey string, d int) {
		for _, n := range nodes {
			fn := flatNode{
				Key:       n.Key,
				NodeDepth: d,
				ParentKey: parentKey,
				IssueType: n.IssueType,
				Status:    n.Status,
				Assignee:  n.Assignee,
				Summary:   n.Summary,
				IsSubject: n.IsSubject,
			}
			data, _ := json.Marshal(fn)
			sb.Write(data)
			sb.WriteByte('\n')
			walk(n.Children, n.Key, d+1)
		}
	}
	// Ancestors at depth < 0 (above subject), subject at 0, children at 1+.
	d := -len(chain.Ancestors)
	for _, anc := range chain.Ancestors {
		fn := flatNode{Key: anc.Key, NodeDepth: d, IssueType: anc.IssueType, Status: anc.Status, Summary: anc.Summary}
		data, _ := json.Marshal(fn)
		sb.Write(data)
		sb.WriteByte('\n')
		d++
	}
	subj := chain.Subject
	fn := flatNode{Key: subj.Key, NodeDepth: 0, IssueType: subj.IssueType, Status: subj.Status, Assignee: subj.Assignee, Summary: subj.Summary, IsSubject: true}
	data, _ := json.Marshal(fn)
	sb.Write(data)
	sb.WriteByte('\n')
	walk(chain.Children, subj.Key, 1)
	return sb.String()
}
