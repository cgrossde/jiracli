package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// ShowHierarchyFlags holds parsed flag values for `show hierarchy`.
type ShowHierarchyFlags struct {
	Profile string
	JSON    bool
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
hint — run 'jiracli setup --reconfigure' or 'jiracli config hierarchy'.`,
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
	return c
}

// ShowHierarchy is the Layer 1 implementation.
func ShowHierarchy(ctx context.Context, flags ShowHierarchyFlags, ref string) (string, error) {
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
	chain, err := jira.BuildHierarchy(ctx, client, hf, entry.Hierarchy.PortfolioFieldName, parsed.Key)
	if err != nil {
		return "", fmt.Errorf("walking hierarchy from %s: %w", parsed.Key, err)
	}
	if flags.JSON {
		data, err := json.Marshal(chain)
		if err != nil {
			return "", fmt.Errorf("marshal hierarchy: %w", err)
		}
		return string(data) + "\n", nil
	}
	return jira.RenderHierarchy(chain, jira.ColorsEnabled()), nil
}
