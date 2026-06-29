package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupLinkTypesFlags struct {
	Profile string
	JSON    bool
	NoCache bool
}

func newLookupLinkTypesCmd() *cobra.Command {
	var flags lookupLinkTypesFlags
	c := &cobra.Command{
		Use:   "link-types",
		Short: "List issue link types",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := lookupLinkTypes(cmd.Context(), flags)
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

func lookupLinkTypes(ctx context.Context, flags lookupLinkTypesFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	types, err := client.ListLinkTypes(ctx, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching link types: %w", err)
	}

	if flags.JSON {
		var sb strings.Builder
		for _, t := range types {
			rec := struct {
				ID      string `json:"id"`
				Name    string `json:"name"`
				Inward  string `json:"inward"`
				Outward string `json:"outward"`
			}{t.ID, t.Name, t.Inward, t.Outward}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, t := range types {
		sb.WriteString(fmt.Sprintf("%-25s  (inward: %s / outward: %s)\n", t.Name, t.Inward, t.Outward))
	}
	if len(types) == 0 {
		sb.WriteString("no link types found\n")
	}
	return sb.String(), nil
}
