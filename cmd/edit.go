package cmd

import (
	"github.com/spf13/cobra"
)

// NewEditCmd builds the "edit" command group: scalar mutations on an existing
// issue. Subcommands cover status (workflow transition), assignee, field, and sprint.
func NewEditCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "edit",
		Short: "Modify an existing issue (status, assignee, field, sprint)",
		Long: `Mutate an existing issue. All subcommands dry-run by default; pass --yes to apply.

  edit status <KEY> <name-or-id>      transition to a new workflow status
  edit assignee <KEY> <user|me|->     assign or unassign the issue
  edit field <KEY> <spec...>          set/add/remove one or more fields
  edit sprint <KEY...> <target>       move to a sprint or the backlog`,
	}
	c.AddCommand(
		NewTransitionCmd(), // Use:"status"
		NewAssignCmd(),     // Use:"assignee"
		NewEditFieldCmd(),  // Use:"field"
		NewEditSprintCmd(), // Use:"sprint"
	)
	return c
}
