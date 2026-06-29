package cmd

import (
	"github.com/spf13/cobra"
)

// NewAddCmd builds the "add" command group: attach a new sub-object to an
// existing issue.
func NewAddCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "add",
		Short: "Add a comment, link, or attachment to an issue",
		Long: `Attach a new sub-object to an existing issue. All subcommands dry-run by default; pass --yes to apply.

  add comment <KEY> [<body...>]                 post a comment
  add link <source> <target> [--type ...]       link two issues
  add attachment <KEY> <file...>                upload files as attachments`,
	}
	c.AddCommand(
		NewCommentCmd(), // Use:"comment"
		NewLinkCmd(),    // Use:"link"
		NewAttachCmd(),  // Use:"attachment"
	)
	return c
}
