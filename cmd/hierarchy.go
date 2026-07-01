package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// HierarchyFlags holds parsed flag values for the `hierarchy` command.
type HierarchyFlags struct {
	Profile     string
	JSON        bool
	All         bool
	Open        bool   // alias for --exclude-done
	ExcludeDone bool   // hide Done-category children
	State       string // "", "todo", "in-progress", "done", "all"
	Depth       int
	Flat        bool   // emit one row per node (tab-separated) instead of the tree
	Since       string // JQL updated >= date, e.g. "-2w", "-1d", "2024-01-01"
}

// NewHierarchyCmd builds the top-level "hierarchy" command.
func NewHierarchyCmd() *cobra.Command {
	var flags HierarchyFlags
	c := &cobra.Command{
		Use:   "hierarchy <KEY>",
		Short: "Locate an issue in its Initiative/Epic tree and list everything nested beneath it",
		Long: `Map an issue's place in the work hierarchy — in both directions.

Given any issue key, this walks:
  upward   — the parent chain, so you can see which Epic, Initiative, or
             portfolio item the issue ultimately rolls up into; and
  downward — the descendants, so you can list everything that feeds into an
             Initiative or Epic (its Epics → Stories → sub-tasks).

Point it at a Story to answer "where does this sit?"; point it at an
Initiative or Epic to answer "what's everything under this?".

Uses the per-profile hierarchy field IDs configured by 'jiracli setup'.
If the profile has no hierarchy config, this command fails with a corrective
hint — run 'jiracli setup --reconfigure' or 'jiracli config hierarchy'.

Use --depth N to fetch N levels of descendants (default 1 = direct children only).

Filter which children are shown (shared with 'jiracli effort' and 'jiracli search'):
  --exclude-done            hide issues in the Done status category
  --open                    alias for --exclude-done
  --state todo|in-progress|done|all   keep only that status category (all = no filter)

Use --flat to emit one tab-separated row per node (depth, key, type, status, assignee, summary).
Use --since to show only issues updated within a relative period (e.g. -2w, -1d, 2024-01-01).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Hierarchy(cmd.Context(), flags, args[0])
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
	c.Flags().BoolVar(&flags.ExcludeDone, "exclude-done", false, "Hide children in the Done status category")
	c.Flags().BoolVar(&flags.Open, "open", false, "Show only non-Done children (alias for --exclude-done)")
	c.Flags().StringVar(&flags.State, "state", "", "Keep only children in this status category: todo, in-progress, done, all")
	c.Flags().IntVar(&flags.Depth, "depth", 1, "How many levels of descendants to fetch (1 = direct children only, max 5)")
	c.Flags().BoolVar(&flags.Flat, "flat", false, "Flat output: one tab-separated row per node (depth, key, type, status, assignee, summary); --flat --json adds parentKey")
	c.Flags().StringVar(&flags.Since, "since", "", "Only include issues updated on or after this date (e.g. -2w, -1d, 2024-01-01)")
	return c
}

// Hierarchy is the Layer 1 implementation.
func Hierarchy(ctx context.Context, flags HierarchyFlags, ref string) (string, error) {
	filter, err := resolveStateFilter(flags.State, flags.ExcludeDone, flags.Open)
	if err != nil {
		return "", err
	}

	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefIssue {
		return "", fmt.Errorf("hierarchy requires a plain issue key — got %q", ref)
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

	// Warn when depth > 1 and the level-1 children have all-empty subtrees
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
		return jira.RenderHierarchyFlat(chain, filter), nil
	}
	return jira.RenderHierarchy(chain, jira.ColorsEnabled(), filter), nil
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
		Key       string `json:"key"`
		NodeDepth int    `json:"depth"`
		ParentKey string `json:"parentKey,omitempty"`
		IssueType string `json:"issueType"`
		Status    string `json:"status"`
		Assignee  string `json:"assignee,omitempty"`
		Summary   string `json:"summary"`
		IsSubject bool   `json:"isSubject,omitempty"`
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
