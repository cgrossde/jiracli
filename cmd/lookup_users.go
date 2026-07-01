package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupUsersFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
	Active  bool
	Limit   int
}

func newLookupUsersCmd() *cobra.Command {
	var flags lookupUsersFlags
	c := &cobra.Command{
		Use:   "users [<query>]",
		Short: "Search for Jira users",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			result, err := lookupUsers(cmd.Context(), flags, q)
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
	c.Flags().StringVar(&flags.Project, "project", "", "Restrict to assignable users for this project key")
	c.Flags().BoolVar(&flags.Active, "active", false, "Only return active users (no-project mode only)")
	c.Flags().IntVar(&flags.Limit, "limit", 20, "Maximum number of results")
	return c
}

func lookupUsers(ctx context.Context, flags lookupUsersFlags, q string) (string, error) {
	entry, _, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	var users []jira.UserRef
	if flags.Project != "" {
		users, err = client.SearchAssignableUsers(ctx, flags.Project, q, flags.Limit)
	} else {
		users, err = client.SearchUsers(ctx, q, flags.Limit, flags.Active)
	}
	if err != nil {
		return "", fmt.Errorf("searching users: %w", err)
	}

	// Jira DC's query param is a hint and may not filter server-side.
	// Apply client-side substring filter on name/displayName/email when a
	// query was provided. (SearchAssignableUsers already does this
	// internally with a broader candidate pool; re-applying here is a
	// no-op in that case and is required for the no-project SearchUsers
	// path, which does not.)
	users = jira.FilterUsersByQuery(users, q)

	if flags.JSON {
		var sb strings.Builder
		for _, u := range users {
			rec := struct {
				Name         string `json:"name"`
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
				Active       bool   `json:"active"`
			}{u.Name, u.DisplayName, u.EmailAddress, u.Active}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, u := range users {
		sb.WriteString(fmt.Sprintf("%-20s  %-30s  %s\n", u.Name, u.DisplayName, u.EmailAddress))
	}
	if len(users) == 0 {
		sb.WriteString("no users found\n")
	}
	return sb.String(), nil
}
