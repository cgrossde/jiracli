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
	All     bool // bypass 100-result default cap, page through every child
}

// NewShowRollupCmd builds the "show rollup" subcommand.
func NewShowRollupCmd() *cobra.Command {
	var flags ShowRollupFlags
	c := &cobra.Command{
		Use:   "rollup <EPIC-KEY>",
		Short: "Aggregate time + story-point estimates across an epic's children",
		Long: `Walks the children of an Epic (via "Epic Link" = KEY JQL) and totals
originalEstimate, remainingEstimate, timeSpent, and Story Points. Breaks
totals down by status category and lists unestimated children.`,
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
	c.Flags().BoolVar(&flags.All, "all", false, "Fetch all children (bypass 100-result default cap)")
	return c
}

// ShowRollup is the Layer 1 implementation for show rollup.
func ShowRollup(ctx context.Context, flags ShowRollupFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("show rollup requires a plain issue key — got %q", ref)
	}
	if parsed.Kind != jira.RefIssue {
		return "", fmt.Errorf("show rollup requires a plain issue key — got %q", ref)
	}
	key := parsed.Key

	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)
	_ = store

	spField := entry.Hierarchy.StoryPointsField

	// Fetch the Epic itself
	epicFields := jira.DefaultIssueFields
	if spField != "" {
		epicFields += "," + spField
	}
	epicRaw, err := client.GetIssue(ctx, key, epicFields, false)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(epicRaw.Fields.IssueType.Name, "Epic") {
		return "", fmt.Errorf("rollup only operates on Epics — %s is a %s. For subtask rollup, use jiracli show %s",
			key, epicRaw.Fields.IssueType.Name, key)
	}

	// Build child fetch fields
	childFieldList := []string{"summary", "status", "issuetype", "assignee", "timetracking"}
	if entry.Hierarchy.EpicLinkField != "" {
		childFieldList = append(childFieldList, entry.Hierarchy.EpicLinkField)
	}
	if spField != "" {
		childFieldList = append(childFieldList, spField)
	}

	epicJQL := `"Epic Link" = ` + key

	// Fetch children — single page (limit 100) unless --all
	var childrenRaw []jira.IssueRaw
	var totalChildren int
	var truncated bool

	if flags.All {
		// Page through all children
		page := 1
		const pageSize = 100
		for {
			resp, err := client.Search(ctx, epicJQL, page, pageSize, childFieldList)
			if err != nil {
				if len(childrenRaw) > 0 {
					truncated = true
					break
				}
				return "", fmt.Errorf("fetching children: %w", err)
			}
			if page == 1 {
				totalChildren = resp.Total
			}
			childrenRaw = append(childrenRaw, resp.Issues...)
			startAt := (page-1)*pageSize + len(resp.Issues)
			if startAt >= resp.Total || len(resp.Issues) == 0 {
				break
			}
			page++
		}
	} else {
		resp, err := client.Search(ctx, epicJQL, 1, 100, childFieldList)
		if err != nil {
			return "", fmt.Errorf("fetching children: %w", err)
		}
		totalChildren = resp.Total
		childrenRaw = resp.Issues
		if totalChildren > len(childrenRaw) {
			truncated = true
		}
	}

	if len(childrenRaw) == 0 && totalChildren == 0 {
		return fmt.Sprintf("Epic %s has no children — nothing to roll up.\n", key), nil
	}

	// Build compact ChildIssueRecord slice for display
	children := make([]jira.ChildIssueRecord, 0, len(childrenRaw))
	for _, raw := range childrenRaw {
		assignee := ""
		if raw.Fields.Assignee != nil {
			assignee = raw.Fields.Assignee.DisplayName
		}
		children = append(children, jira.ChildIssueRecord{
			Key:            raw.Key,
			Summary:        raw.Fields.Summary,
			Status:         raw.Fields.Status.Name,
			StatusCategory: raw.Fields.Status.StatusCategory.Name,
			IssueType:      raw.Fields.IssueType.Name,
			Assignee:       assignee,
		})
	}

	result := jira.RollUpChildren(key, children, childrenRaw, spField)
	result.Truncated = truncated
	result.TotalChildren = totalChildren

	if flags.JSON {
		data, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal rollup: %w", err)
		}
		return string(data) + "\n", nil
	}

	return renderRollup(epicRaw, result, spField, truncated, totalChildren), nil
}

// renderRollup produces plain-text rollup output.
func renderRollup(epicRaw jira.IssueRaw, r jira.RolledUpEstimates, spField string, truncated bool, totalChildren int) string {
	var sb strings.Builder
	clr := colorsEnabled()

	// Epic header
	typeBadge := colorIssueType(epicRaw.Fields.IssueType.Name)
	statusBadge := colorStatusName(epicRaw.Fields.Status.Name)
	priority := "—"
	if epicRaw.Fields.Priority != nil {
		priority = epicRaw.Fields.Priority.Name
	}
	if clr {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n",
			typeBadge,
			jira.Bold(r.EpicKey, true),
			statusBadge,
			colorPriority(priority))
	} else {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n", typeBadge, r.EpicKey, epicRaw.Fields.Status.Name, priority)
	}
	fmt.Fprintf(&sb, "%s\n", jira.BoldFgW(epicRaw.Fields.Summary, clr))

	// Fix versions & children count
	fixVersions := make([]string, 0, len(epicRaw.Fields.FixVersions))
	for _, fv := range epicRaw.Fields.FixVersions {
		fixVersions = append(fixVersions, fv.Name)
	}
	if len(fixVersions) > 0 {
		fmt.Fprintf(&sb, "Fix Versions: %s   ", strings.Join(fixVersions, ", "))
	}
	fmt.Fprintf(&sb, "Children: %d\n", totalChildren)
	sb.WriteByte('\n')

	// Estimates block
	tt := r.Total
	if tt.OriginalEstimateSecs > 0 || tt.RemainingEstimateSecs > 0 || tt.TimeSpentSecs > 0 {
		var parts []string
		if tt.OriginalEstimateSecs > 0 {
			parts = append(parts, "Planned "+jira.FormatSeconds(tt.OriginalEstimateSecs))
		}
		if tt.RemainingEstimateSecs > 0 {
			parts = append(parts, "Remaining "+jira.FormatSeconds(tt.RemainingEstimateSecs))
		}
		if tt.TimeSpentSecs > 0 {
			parts = append(parts, "Spent "+jira.FormatSeconds(tt.TimeSpentSecs))
		}
		fmt.Fprintf(&sb, "%s %s\n", sectionLabel("Estimates:"), strings.Join(parts, " · "))
		if tt.OriginalEstimateSecs > 0 {
			bar := jira.FormatProgressBar(tt.TimeSpentSecs, tt.OriginalEstimateSecs, 24, clr)
			fmt.Fprintf(&sb, "%s\n", bar)
		}
	}
	if tt.StoryPoints > 0 || tt.PointedChildren > 0 {
		fmt.Fprintf(&sb, "%s %g SP (%d of %d children pointed)\n",
			sectionLabel("Story Points:"), tt.StoryPoints, tt.PointedChildren, tt.Children)
	}
	if tt.OriginalEstimateSecs > 0 || tt.StoryPoints > 0 {
		sb.WriteByte('\n')
	}

	// Status breakdown table
	sb.WriteString(sectionLabel("Breakdown by status:") + "\n")
	// Header
	fmt.Fprintf(&sb, "  %-14s  %8s  %8s  %8s  %8s  %4s\n",
		"Status", "Children", "Planned", "Remaining", "Logged", "SP")
	rule := "  " + strings.Repeat("─", 14+2+8+2+8+2+8+2+8+2+4) + "\n"
	sb.WriteString(rule)
	for _, b := range r.Buckets {
		planned := dashIfZero(b.OriginalEstimateSecs)
		remaining := dashIfZero(b.RemainingEstimateSecs)
		logged := dashIfZero(b.TimeSpentSecs)
		sp := dashIfZeroFloat(b.StoryPoints)
		fmt.Fprintf(&sb, "  %-14s  %8d  %8s  %8s  %8s  %4s\n",
			b.Category, b.Children, planned, remaining, logged, sp)
	}
	sb.WriteString(rule)
	// Total row
	planned := dashIfZero(tt.OriginalEstimateSecs)
	remaining := dashIfZero(tt.RemainingEstimateSecs)
	logged := dashIfZero(tt.TimeSpentSecs)
	sp := dashIfZeroFloat(tt.StoryPoints)
	fmt.Fprintf(&sb, "  %-14s  %8d  %8s  %8s  %8s  %4s\n",
		"Total", tt.Children, planned, remaining, logged, sp)
	sb.WriteByte('\n')

	// Unestimated children
	unest := r.Unestimated
	if len(unest) > 0 {
		fmt.Fprintf(&sb, "%s (%d of %d):\n",
			sectionLabel("Unestimated children"), len(unest), tt.Children)
		const maxUnest = 15
		display := unest
		if len(unest) > maxUnest {
			display = unest[:maxUnest]
		}
		for _, ch := range display {
			chTypeBadge := colorIssueType(ch.IssueType)
			chStatus := colorStatusName(ch.Status)
			statusVis := len([]rune(jira.TruncateString(ch.Status, 14)))
			statusPadded := chStatus + strings.Repeat(" ", max(0, 14-statusVis))
			fmt.Fprintf(&sb, "  %s  %-12s  %s  %s\n",
				chTypeBadge,
				jira.TruncateString(ch.Key, 12),
				statusPadded,
				jira.TruncateString(ch.Summary, 50))
		}
		if len(unest) > maxUnest {
			fmt.Fprintf(&sb, "  …and %d more\n", len(unest)-maxUnest)
		}
		sb.WriteByte('\n')
		fmt.Fprintf(&sb, "→ jiracli search \"\\\"Epic Link\\\" = %s AND originalEstimate is EMPTY\"\n", r.EpicKey)
		sb.WriteByte('\n')
	}

	if truncated {
		fmt.Fprintf(&sb, "(showing %d of %d children — pass --all to walk every child)\n", r.Total.Children, totalChildren)
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
