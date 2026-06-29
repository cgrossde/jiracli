package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupStatusesFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Query   string
}

func newLookupStatusesCmd() *cobra.Command {
	var flags lookupStatusesFlags
	c := &cobra.Command{
		Use:   "statuses",
		Short: "List workflow statuses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := lookupStatuses(cmd.Context(), flags)
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
	c.Flags().StringVar(&flags.Query, "query", "", "Filter statuses by name prefix")
	return c
}

func lookupStatuses(ctx context.Context, flags lookupStatusesFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	statuses, err := client.ListStatuses(ctx, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching statuses: %w", err)
	}

	ql := strings.ToLower(flags.Query)
	var matched []jira.Status
	for _, s := range statuses {
		if flags.Query == "" || strings.HasPrefix(strings.ToLower(s.Name), ql) {
			matched = append(matched, s)
		}
	}

	if flags.JSON {
		var sb strings.Builder
		for _, s := range matched {
			rec := struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				StatusCategory string `json:"statusCategory"`
			}{s.ID, s.Name, s.StatusCategory.Name}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, s := range matched {
		sb.WriteString(fmt.Sprintf("%-30s  [%s]\n", s.Name, s.StatusCategory.Name))
	}
	if len(matched) == 0 {
		sb.WriteString("no statuses found\n")
	}
	return sb.String(), nil
}
