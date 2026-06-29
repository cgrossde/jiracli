package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// MeFlags holds parsed flag values for the me command.
type MeFlags struct {
	Profile string
	JSON    bool
	NoCache bool
}

// NewStatusCmd builds the "auth status" command.
func NewStatusCmd() *cobra.Command {
	var flags MeFlags
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the authenticated user and credential status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Me(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Skip cache and fetch live data")
	return c
}

// meRecord is the NDJSON schema for the me command.
type meRecord struct {
	Profile       string  `json:"profile"`
	URL           string  `json:"url"`
	SavedAt       string  `json:"savedAt"`
	Name          string  `json:"name"`
	DisplayName   string  `json:"displayName"`
	EmailAddress  string  `json:"emailAddress"`
	Active        bool    `json:"active"`
	Authenticated bool    `json:"authenticated"`
	Error         *string `json:"error,omitempty"`
}

// Me is the Layer 1 implementation.
func Me(ctx context.Context, flags MeFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	var u jira.User
	cached := false
	if !flags.NoCache {
		if err := store.Get("myself", jira.TTLMyself, &u); err == nil {
			cached = true
		}
	}
	var fetchErr error
	if !cached {
		u, fetchErr = client.Myself(ctx)
		if fetchErr == nil && !flags.NoCache {
			_ = store.Put("myself", u)
		}
	}

	authenticated := fetchErr == nil
	savedAt := entry.SavedAt.Format("2006-01-02T15:04:05Z07:00")

	if flags.JSON {
		rec := meRecord{
			Profile:       entry.Profile,
			URL:           entry.URL,
			SavedAt:       savedAt,
			Name:          u.Name,
			DisplayName:   u.DisplayName,
			EmailAddress:  u.EmailAddress,
			Active:        u.Active,
			Authenticated: authenticated,
		}
		if fetchErr != nil {
			msg := fetchErr.Error()
			rec.Error = &msg
		}
		data, _ := json.Marshal(rec)
		return string(data) + "\n", nil
	}

	status := "✓ authenticated"
	if fetchErr != nil {
		status = "✗ " + fetchErr.Error()
	}
	return fmt.Sprintf("profile: %s\nurl:     %s\nuser:    %s (%s)\nemail:   %s\nsaved:   %s\nstatus:  %s\n",
		entry.Profile, entry.URL, u.DisplayName, u.Name, u.EmailAddress, savedAt, status), nil
}
