package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupVersionsFlags struct {
	Profile    string
	JSON       bool
	NoCache    bool
	Project    string
	Released   bool
	Unreleased bool
	Archived   bool
	Limit      int
}

func newLookupVersionsCmd() *cobra.Command {
	var flags lookupVersionsFlags
	c := &cobra.Command{
		Use:   "versions [<query>]",
		Short: "List versions for a project",
		Long:  "List versions for a Jira project.\n\nRequires --project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			result, err := lookupVersions(cmd.Context(), flags, q)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Skip cache")
	c.Flags().StringVar(&flags.Project, "project", "", "Project key (required)")
	c.Flags().BoolVar(&flags.Released, "released", false, "Show only released versions")
	c.Flags().BoolVar(&flags.Unreleased, "unreleased", false, "Show only unreleased versions")
	c.Flags().BoolVar(&flags.Archived, "archived", false, "Include archived versions")
	c.Flags().IntVar(&flags.Limit, "limit", 0, "Maximum number of versions to show (0 = all)")
	cobra.MarkFlagRequired(c.Flags(), "project")
	return c
}

func lookupVersions(ctx context.Context, flags lookupVersionsFlags, q string) (string, error) {
	if flags.Project == "" {
		return "", fmt.Errorf("--project required — run: jiracli lookup projects")
	}
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	proj, err := client.GetProject(ctx, flags.Project, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching project %s: %w", flags.Project, err)
	}

	ql := strings.ToLower(q)
	var matched []jira.Version
	for _, v := range proj.Versions {
		if v.Archived && !flags.Archived {
			continue
		}
		if flags.Released && !v.Released {
			continue
		}
		if flags.Unreleased && v.Released {
			continue
		}
		if q != "" && !strings.HasPrefix(strings.ToLower(v.Name), ql) {
			continue
		}
		matched = append(matched, v)
	}

	// Apply --limit after all filters.
	total := len(matched)
	if flags.Limit > 0 && len(matched) > flags.Limit {
		matched = matched[:flags.Limit]
	}

	if flags.JSON {
		var sb strings.Builder
		for _, v := range matched {
			rec := struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Released    bool   `json:"released"`
				Archived    bool   `json:"archived"`
				Description string `json:"description"`
			}{v.ID, v.Name, v.Released, v.Archived, v.Description}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, v := range matched {
		status := "unreleased"
		if v.Released {
			status = "released"
		}
		if v.Archived {
			status += " (archived)"
		}
		desc := ""
		if v.Description != "" {
			desc = "  " + v.Description
		}
		sb.WriteString(fmt.Sprintf("%-30s  %-20s%s\n", v.Name, status, desc))
	}
	if len(matched) == 0 {
		sb.WriteString("no versions found\n")
	}
	if flags.Limit > 0 && total > flags.Limit {
		sb.WriteString(fmt.Sprintf("\n(showing %d of %d — use --limit 0 to see all)\n", flags.Limit, total))
	}
	return sb.String(), nil
}
