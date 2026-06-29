package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewShowCmd builds the "show" command group.
//
// The root command dispatches on the shape of the argument:
//   - Plain issue key (ACME-123) or browse URL → full issue
//   - KEY:attach:ID compound ref             → attachment metadata (or download with -o)
//   - KEY:comment:ID compound ref            → single comment display
//
func NewShowCmd(rawOut io.Writer) *cobra.Command {
	// Merge issue flags and attachment-download flags onto the root command.
	var issueFlags IssueFlags
	var attachFlags AttachmentDownloadFlags

	root := &cobra.Command{
		Use:   "show <ref>",
		Short: "Show issues and their facets (default: full issue)",
		Long: `Read-only views of a Jira issue or attachment.

The argument is parsed as a reference and dispatched automatically:

  show ACME-123                   full issue (plain key or browse URL)
  show ACME-123:attach:42         attachment metadata
  show ACME-123:attach:42 -o      stream attachment content to stdout
  show ACME-123:attach:42 -f path download attachment to file

Subcommands drill into a specific facet by plain key:
  show comments ACME-123          comment thread
  show history ACME-123           changelog
  show transitions ACME-123       available workflow transitions
  show attachments ACME-123       attachment list
  show assigned                   issues assigned to the current user

Common flags (on the default issue view):
  --json                  NDJSON output
  --fields <spec>         Restrict/extend fields (e.g. "key,summary,description"
                          or "+timetracking,-priority")
  --no-history            Omit activity/changelog section
  --no-comments           Omit comments section
  --no-children           Skip the children list (saves one API call)
  --comments N            Inline last N comments (default 1, max 25)
  --parent                Show this issue's parent instead
                          (Parent Link → Parent → Epic Link, in that order)

Activity shows the newest 10 entries. Long-text fields (description) are
always suppressed; comment/summary/environment are truncated to 120 chars.
Use 'jiracli show history <KEY> --since 7d' for the full paginated changelog.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := jira.ParseRef(args[0])
			if err != nil {
				return fmt.Errorf("invalid ref %q: %w", args[0], err)
			}
			switch ref.Kind {
			case jira.RefIssue:
				if issueFlags.CommentsN > 25 {
					return fmt.Errorf("--comments max is 25 — use jiracli show comments %s --limit %d for a longer thread", args[0], issueFlags.CommentsN)
				}
				result, err := Issue(cmd.Context(), issueFlags, args[0])
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
			case jira.RefAttachment:
				if attachFlags.Stdout {
					return attachmentToStdout(cmd.Context(), attachFlags, args[0], rawOut)
				}
				result, err := AttachmentDownload(cmd.Context(), attachFlags, args[0])
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
			case jira.RefComment:
				result, err := ShowComment(cmd.Context(), ShowCommentFlags{Profile: issueFlags.Profile, JSON: issueFlags.JSON}, args[0])
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
			}
			return nil
		},
	}

	// Issue flags.
	root.Flags().StringVar(&issueFlags.Profile, "profile", "", "Profile name")
	root.Flags().BoolVar(&issueFlags.JSON, "json", false, "Output NDJSON")
	root.Flags().BoolVar(&issueFlags.NoHistory, "no-history", false, "Skip activity/changelog section")
	root.Flags().BoolVar(&issueFlags.NoComments, "no-comments", false, "Skip comments section")
	root.Flags().IntVar(&issueFlags.CommentsN, "comments", 1, "Number of latest comments to preview (max 25)")
	root.Flags().StringVar(&issueFlags.Fields, "fields", "", "Override field list (default/+add/-drop)")
	root.Flags().BoolVar(&issueFlags.NoChildren, "no-children", false, "Skip fetching the children list (one fewer API call)")
	root.Flags().BoolVar(&issueFlags.Parent, "parent", false, "Show the parent of <KEY> instead (Parent Link → Parent → Epic Link, in that order)")

	// Attachment flags (-o stdout, -f file; profile shared with issue flags).
	root.Flags().BoolVarP(&attachFlags.Stdout, "output", "o", false, "Stream attachment content to stdout (only used with KEY:attach:ID refs)")
	root.Flags().StringVarP(&attachFlags.FilePath, "file", "f", "", "Save attachment to this path (only used with KEY:attach:ID refs; default: /tmp/jiracli-attach/<id>-<filename>)")

	// Wire attachFlags.Profile to the same --profile flag.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		attachFlags.Profile = issueFlags.Profile
		return nil
	}

	root.AddCommand(
		NewCommentsCmd(),
		NewHistoryCmd(),
		NewTransitionsCmd(),
		NewAttachmentsCmd(),
		NewAssignedCmd(),
	)
	return root
}
