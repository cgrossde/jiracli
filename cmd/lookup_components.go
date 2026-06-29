package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupComponentsFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
}

func newLookupComponentsCmd() *cobra.Command {
	var flags lookupComponentsFlags
	c := &cobra.Command{
		Use:   "components [<query>]",
		Short: "List components for a project",
		Long:  "List components for a Jira project.\n\nRequires --project.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			result, err := lookupComponents(cmd.Context(), flags, q)
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
	cobra.MarkFlagRequired(c.Flags(), "project")
	return c
}

func lookupComponents(ctx context.Context, flags lookupComponentsFlags, q string) (string, error) {
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
	var matched []jira.Component
	for _, comp := range proj.Components {
		if q == "" || strings.HasPrefix(strings.ToLower(comp.Name), ql) {
			matched = append(matched, comp)
		}
	}

	if flags.JSON {
		var sb strings.Builder
		for _, comp := range matched {
			rec := struct {
				ID          string      `json:"id"`
				Name        string      `json:"name"`
				Lead        interface{} `json:"lead,omitempty"`
				Description string      `json:"description"`
				Project     string      `json:"project"`
			}{
				ID:          comp.ID,
				Name:        comp.Name,
				Description: comp.Description,
				Project:     flags.Project,
			}
			if comp.Lead != nil {
				rec.Lead = map[string]string{
					"name":        comp.Lead.Name,
					"displayName": comp.Lead.DisplayName,
				}
			}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s — %d components", flags.Project, len(matched)))
	if q != "" {
		sb.WriteString(fmt.Sprintf(" matching %q", q))
	}
	sb.WriteString(":\n\n")

	if len(matched) > 0 {
		sb.WriteString(fmt.Sprintf("→ jiracli search 'project = %s AND component = \"%s\"'  # (and any name below)\n\n", flags.Project, matched[0].Name))
	}

	for _, comp := range matched {
		lead := ""
		if comp.Lead != nil {
			lead = fmt.Sprintf("  Lead: %s (%s)", comp.Lead.DisplayName, comp.Lead.Name)
		}
		desc := ""
		if comp.Description != "" {
			desc = fmt.Sprintf("  Description: %s", comp.Description)
		}
		sb.WriteString(fmt.Sprintf("  %s%s%s\n", comp.Name, lead, desc))
	}

	if len(matched) == 0 {
		sb.WriteString("  no components found\n")
	} else {
		last := matched[len(matched)-1]
		sb.WriteString(fmt.Sprintf("\n→ jiracli search 'project = %s AND component = \"%s\"'  # (and any name above)\n", flags.Project, last.Name))
	}
	if q != "" {
		sb.WriteString(fmt.Sprintf("→ jiracli lookup components --project %s   # full list\n", flags.Project))
	}
	return sb.String(), nil
}
