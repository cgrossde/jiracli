package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupPrioritiesFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
}

func newLookupPrioritiesCmd() *cobra.Command {
	var flags lookupPrioritiesFlags
	c := &cobra.Command{
		Use:   "priorities",
		Short: "List priorities (global or per-project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := lookupPriorities(cmd.Context(), flags)
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
	c.Flags().StringVar(&flags.Project, "project", "", "Restrict to the priority scheme for this project key")
	return c
}

func lookupPriorities(ctx context.Context, flags lookupPrioritiesFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	var priorities []jira.Priority
	var fallbackUsed bool
	if flags.Project != "" {
		var ferr error
		priorities, fallbackUsed, ferr = client.ListProjectPriorities(ctx, flags.Project, store, flags.NoCache)
		if ferr != nil {
			return "", fmt.Errorf("fetching priorities for project %s: %w", flags.Project, ferr)
		}
	} else {
		var ferr error
		priorities, ferr = client.ListPriorities(ctx, store, flags.NoCache)
		if ferr != nil {
			return "", fmt.Errorf("fetching priorities: %w", ferr)
		}
	}

	if flags.JSON {
		var sb strings.Builder
		for _, p := range priorities {
			rec := struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{p.ID, p.Name}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		if fallbackUsed {
			note := struct {
				Note string `json:"note"`
			}{"no per-project priority scheme — showing global list"}
			data, _ := json.Marshal(note)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, p := range priorities {
		sb.WriteString(p.Name)
		sb.WriteByte('\n')
	}
	if len(priorities) == 0 {
		sb.WriteString("no priorities found\n")
	}
	if fallbackUsed {
		sb.WriteString("\n(note: no per-project priority scheme — showing global list)\n")
	}
	return sb.String(), nil
}
