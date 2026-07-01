package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// AssignedFlags holds parsed flag values for the assigned command.
type AssignedFlags struct {
	Profile  string
	JSON     bool
	KeysOnly bool
	State    string
	Limit    int
	Page     int
}

// NewAssignedCmd builds the "show assigned" subcommand.
func NewAssignedCmd() *cobra.Command {
	var flags AssignedFlags
	c := &cobra.Command{
		Use:   "assigned",
		Short: "List issues assigned to the current user",
		Long: `List issues assigned to the current user, ordered by last updated.

Non-Done issues are shown by default. Use --state to filter by status category.

  show assigned                 non-Done issues (default)
  show assigned --state all     all issues including Done
  show assigned --state done    only Done issues`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Assigned(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Maximum results per page (1-100)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	c.Flags().StringVar(&flags.State, "state", "", "Status category filter: todo, in-progress, done, all (default: non-Done)")
	c.Flags().BoolVar(&flags.KeysOnly, "keys-only", false, "Print one issue key per line; ideal for piping into further commands")
	return c
}

// Assigned is the Layer 1 implementation for the assigned command.
func Assigned(ctx context.Context, flags AssignedFlags) (string, error) {
	jql, err := buildAssignedJQL(flags.State)
	if err != nil {
		return "", err
	}
	return Search(ctx, SearchFlags{
		Profile:  flags.Profile,
		JSON:     flags.JSON,
		KeysOnly: flags.KeysOnly,
		Limit:    flags.Limit,
		Page:     flags.Page,
		// ExcludeDone is false: JQL is pre-built with its own status clause.
		// Next-page hints point back at "show assigned", not "search".
		PageCmdBase:  "jiracli show assigned",
		PageCmdState: flags.State,
	}, jql)
}

// stateJQLClause returns a JQL fragment for the given --state value.
// An empty state returns ("", nil) — caller decides what to do with no filter.
func stateJQLClause(state string) (string, error) {
	switch state {
	case "", "all":
		return "", nil
	case "default":
		return `statusCategory != "Done"`, nil
	case "todo":
		return `statusCategory = "To Do"`, nil
	case "in-progress":
		return `statusCategory = "In Progress"`, nil
	case "done":
		return `statusCategory = "Done"`, nil
	default:
		return "", fmt.Errorf(
			"unknown --state %q — valid values: todo, in-progress, done, all",
			state,
		)
	}
}

// buildAssignedJQL constructs the full JQL query for the assigned command.
// An empty state defaults to non-Done (same as "default").
func buildAssignedJQL(state string) (string, error) {
	if state == "" {
		state = "default"
	}
	clause, err := stateJQLClause(state)
	if err != nil {
		return "", err
	}
	if clause == "" {
		// "all" — no status filter, include Done.
		return `assignee = currentUser() ORDER BY updated DESC`, nil
	}
	return `assignee = currentUser() AND ` + clause + ` ORDER BY updated DESC`, nil
}
