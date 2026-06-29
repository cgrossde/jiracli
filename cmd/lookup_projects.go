package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupProjectsFlags struct {
	Profile string
	JSON    bool
	NoCache bool
}

func newLookupProjectsCmd() *cobra.Command {
	var flags lookupProjectsFlags
	c := &cobra.Command{
		Use:   "projects [<query>]",
		Short: "List Jira projects",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			result, err := lookupProjects(cmd.Context(), flags, q)
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
	return c
}

func lookupProjects(ctx context.Context, flags lookupProjectsFlags, q string) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	projects, err := client.ListProjects(ctx, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching projects: %w", err)
	}

	ql := strings.ToLower(q)
	var matched []jira.Project
	for _, p := range projects {
		if q == "" || strings.HasPrefix(strings.ToLower(p.Key), ql) || strings.HasPrefix(strings.ToLower(p.Name), ql) {
			matched = append(matched, p)
		}
	}

	if flags.JSON {
		var sb strings.Builder
		for _, p := range matched {
			rec := struct {
				ID   string `json:"id"`
				Key  string `json:"key"`
				Name string `json:"name"`
			}{p.ID, p.Key, p.Name}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	if len(matched) > 0 {
		sb.WriteString(fmt.Sprintf("→ jiracli search \"project = %s\"  # (and any key below)\n\n", matched[0].Key))
	}
	for _, p := range matched {
		sb.WriteString(fmt.Sprintf("  %-15s  %s\n", p.Key, p.Name))
	}
	if len(matched) == 0 {
		sb.WriteString("no projects found\n")
	} else {
		last := matched[len(matched)-1]
		sb.WriteString(fmt.Sprintf("\n→ jiracli search \"project = %s\"  # (and any key above)\n", last.Key))
	}
	return sb.String(), nil
}
