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
	Category string
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

Non-Done issues are shown by default. Use --category to filter by status category.

  show assigned                    non-Done issues (default)
  show assigned --category all     all issues including Done
  show assigned --category done    only Done issues`,
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
	c.Flags().StringVar(&flags.Category, "category", "", "Status category filter: todo, in-progress, done, all (default: non-Done)")
	return c
}


// Assigned is the Layer 1 implementation for the assigned command.
func Assigned(ctx context.Context, flags AssignedFlags) (string, error) {
	jql, err := buildAssignedJQL(flags.Category)
	if err != nil {
		return "", err
	}
	return Search(ctx, SearchFlags{
		Profile: flags.Profile,
		JSON:    flags.JSON,
		Limit:   flags.Limit,
		Page:    flags.Page,
		// ExcludeDone is false: JQL is pre-built with its own status clause.
	}, jql)
}

// categoryJQLClause returns a JQL fragment for the given category value.
// An empty category returns ("", nil) — caller decides what to do with no filter.
func categoryJQLClause(category string) (string, error) {
	switch category {
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
			"unknown category %q — valid values: todo, in-progress, done, all",
			category,
		)
	}
}

// buildAssignedJQL constructs the full JQL query for the assigned command.
// An empty category defaults to non-Done (same as "default").
func buildAssignedJQL(category string) (string, error) {
	if category == "" {
		category = "default"
	}
	clause, err := categoryJQLClause(category)
	if err != nil {
		return "", err
	}
	if clause == "" {
		// "all" — no status filter, include Done.
		return `assignee = currentUser() ORDER BY updated DESC`, nil
	}
	return `assignee = currentUser() AND ` + clause + ` ORDER BY updated DESC`, nil
}
