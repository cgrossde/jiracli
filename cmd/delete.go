package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewDeleteCmd builds the "delete" command. It dispatches on the ref shape:
//
//	delete ACME-123                    → delete issue
//	delete ACME-123:comment:NNN        → delete comment
//	delete ACME-123:attach:NNN         → delete attachment
//	delete ACME-123:link:NNN           → delete issue link
//
// Aliased as "rm".
func NewDeleteCmd() *cobra.Command {
	var profile string
	var yes bool
	var withSubtasks bool

	c := &cobra.Command{
		Use:     "delete <ref> [ref...]",
		Aliases: []string{"rm"},
		Short:   "Delete issues, comments, attachments, or links",
		Long: `Delete one or more issues, or a single sub-object. Dry-run by default; pass --yes to apply.

The type is inferred from the ref shape:

  delete ACME-123                    delete an issue (single)
  delete ACME-123 ACME-124 ACME-125  delete multiple issues
  delete ACME-123:comment:NNN        delete one comment
  delete ACME-123:attach:NNN         delete one attachment
  delete ACME-123:link:NNN           delete one issue link

Multi-key mode only applies when every argument is a bare issue key. Comment,
attachment, and link refs always require a single argument.

Link IDs appear as (id: NNN) on each link line in: jiracli show ACME-123`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Multi-key path: all args must be plain issue keys.
			if len(args) > 1 {
				for _, arg := range args {
					ref, err := jira.ParseRef(arg)
					if err != nil || ref.Kind != jira.RefIssue {
						return fmt.Errorf("multi-key delete only supports bare issue keys — %q is not a plain key (use a single arg for comment/attach/link refs)", arg)
					}
				}
				result, err := DeleteIssueBulk(cmd.Context(), DeleteIssueFlags{
					Profile:      profile,
					WithSubtasks: withSubtasks,
					Yes:          yes,
				}, args)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
				return nil
			}

			// Single-ref path: unchanged behaviour.
			ref, err := jira.ParseRef(args[0])
			if err != nil {
				return err
			}

			var result string

			switch ref.Kind {
			case jira.RefIssue:
				result, err = DeleteIssue(cmd.Context(), DeleteIssueFlags{
					Profile:      profile,
					WithSubtasks: withSubtasks,
					Yes:          yes,
				}, ref.Key)

			case jira.RefComment:
				result, err = DeleteComment(cmd.Context(), DeleteCommentFlags{
					Profile: profile,
					Yes:     yes,
				}, args[0])

			case jira.RefAttachment:
				result, err = DeleteAttachment(cmd.Context(), DeleteAttachmentFlags{
					Profile: profile,
					Yes:     yes,
				}, args[0])

			case jira.RefLink:
				result, err = DeleteLink(cmd.Context(), DeleteLinkFlags{
					Profile: profile,
					Yes:     yes,
				}, args[0])

			default:
				return fmt.Errorf("unrecognised ref kind %d", ref.Kind)
			}

			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}

	c.Flags().StringVar(&profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&yes, "yes", false, "Apply without confirmation")
	c.Flags().BoolVar(&withSubtasks, "with-subtasks", false, "Cascade delete to subtasks (issues only)")
	return c
}
