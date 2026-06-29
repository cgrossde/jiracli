package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupLabelsFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
}

func newLookupLabelsCmd() *cobra.Command {
	var flags lookupLabelsFlags
	c := &cobra.Command{
		Use:   "labels [<query>]",
		Short: "Search for Jira labels",
		Long: `Search for Jira labels. Without --project, queries the autocomplete endpoint (fast).
With --project, paginates all labelled issues to aggregate a complete label set — this can
take up to 60 seconds for large projects. Results are cached for 5 minutes.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			if flags.Project != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "Aggregating labels for project %s — this may take up to 60 seconds for large projects...\n", flags.Project)
			}
			result, err := lookupLabels(cmd.Context(), flags, q)
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
	c.Flags().StringVar(&flags.Project, "project", "", "Aggregate labels from a specific project (5 min cache)")
	return c
}

func lookupLabels(ctx context.Context, flags lookupLabelsFlags, q string) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	if flags.Project != "" {
		// Aggregation may require many paginated API calls; allow up to 60 seconds.
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		labels, err := client.AggregateLabelsByProject(ctx, flags.Project, store, flags.NoCache)
		if err != nil {
			return "", fmt.Errorf("aggregating labels for project %s: %w", flags.Project, err)
		}
		// prefix-filter by q (case-insensitive)
		ql := strings.ToLower(q)
		var filtered []string
		for _, l := range labels {
			if q == "" || strings.HasPrefix(strings.ToLower(l), ql) {
				filtered = append(filtered, l)
			}
		}

		if flags.JSON {
			var sb strings.Builder
			for _, l := range filtered {
				rec := struct {
					Label string `json:"label"`
				}{l}
				data, _ := json.Marshal(rec)
				sb.Write(data)
				sb.WriteByte('\n')
			}
			return sb.String(), nil
		}

		var sb strings.Builder
		for _, l := range filtered {
			sb.WriteString(l)
			sb.WriteByte('\n')
		}
		if len(filtered) == 0 {
			sb.WriteString("no labels found\n")
		}
		sb.WriteString(fmt.Sprintf("\nSourced from project %s (cached 5 min)\n", flags.Project))
		return sb.String(), nil
	}

	// Global label suggestions via autocomplete endpoint.
	suggestions, truncated, err := suggestLabels(ctx, client, q)
	if err != nil {
		return "", fmt.Errorf("fetching label suggestions: %w", err)
	}

	if flags.JSON {
		var sb strings.Builder
		for _, l := range suggestions {
			rec := struct {
				Label string `json:"label"`
			}{l}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, l := range suggestions {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	if len(suggestions) == 0 {
		sb.WriteString("no labels found\n")
	}
	if truncated {
		sb.WriteString("\n(results may be truncated — server limit is 15)\n")
	}
	return sb.String(), nil
}
