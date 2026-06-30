package cmd

import (
	"io"

	"github.com/spf13/cobra"
)

// NewConfigCmd builds the "config" command group.
// rawOut is real stdout — interactive prompts in subcommands write directly to it.
func NewConfigCmd(rawOut io.Writer) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Per-profile non-credential configuration",
	}
	c.AddCommand(NewConfigHierarchyCmd(rawOut), NewConfigAgileCmd(rawOut))
	return c
}
