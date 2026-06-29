package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupIssueTypesFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
}

func newLookupIssueTypesCmd() *cobra.Command {
	var flags lookupIssueTypesFlags
	c := &cobra.Command{
		Use:   "issue-types",
		Short: "List issue types (global or per-project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := lookupIssueTypes(cmd.Context(), flags)
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
	c.Flags().StringVar(&flags.Project, "project", "", "Restrict to issue types for this project key")
	return c
}

func lookupIssueTypes(ctx context.Context, flags lookupIssueTypesFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	var types []jira.IssueType
	if flags.Project != "" {
		types, err = client.ListProjectIssueTypes(ctx, flags.Project, store, flags.NoCache)
	} else {
		types, err = client.ListIssueTypes(ctx, store, flags.NoCache)
	}
	if err != nil {
		return "", fmt.Errorf("fetching issue types: %w", err)
	}

	if flags.JSON {
		var sb strings.Builder
		for _, t := range types {
			rec := struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Subtask     bool   `json:"subtask"`
			}{t.ID, t.Name, t.Description, t.Subtask}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, t := range types {
		sub := ""
		if t.Subtask {
			sub = "  [subtask]"
		}
		desc := ""
		if t.Description != "" {
			desc = "  " + t.Description
		}
		sb.WriteString(fmt.Sprintf("%-25s%s%s\n", t.Name, sub, desc))
	}
	if len(types) == 0 {
		sb.WriteString("no issue types found\n")
	}
	return sb.String(), nil
}
