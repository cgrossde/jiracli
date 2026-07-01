package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewShowCmd builds the "show" command group.
//
// The root command dispatches on the shape of the argument:
//   - Plain issue key (ACME-123) or browse URL → full issue
//   - KEY:attach:ID compound ref             → attachment metadata (or download with -o)
//   - KEY:comment:ID compound ref            → single comment display
func NewShowCmd(rawOut io.Writer) *cobra.Command {
	// Merge issue flags and attachment-download flags onto the root command.
	var issueFlags IssueFlags
	var attachFlags AttachmentDownloadFlags

	root := &cobra.Command{
		Use:   "show <ref> [ref...]",
		Short: "Show issues and their facets (default: full issue)",
		Long: `Read-only views of a Jira issue or attachment.

The argument is parsed as a reference and dispatched automatically:

  show ACME-123                   full issue (plain key or browse URL)
  show ACME-123 ACME-124          multiple issues, separated by a rule
  show -                          read keys from stdin (one per line)
  show ACME-123:attach:42         attachment metadata
  show ACME-123:attach:42 -o      stream attachment content to stdout
  show ACME-123:attach:42 -f path download attachment to file

Stdin mode: pass - as the sole argument to read one issue key per line from
stdin. Blank lines and lines starting with # are ignored. This composes
directly with --keys-only:

  jiracli search --keys-only --assigned | jiracli show -

Subcommands drill into a specific facet by plain key:
  show comments ACME-123          comment thread
  show history ACME-123           changelog
  show transitions ACME-123       available workflow transitions
  show attachments ACME-123       attachment list
  show assigned                   issues assigned to the current user

Common flags (on the default issue view):
  --json                  NDJSON output
  --fields <spec>         Add/drop fields. Syntax: "name" or "+name" to add,
                          "-name" to drop. Standard names: reporter, description,
                          labels, components, fixVersions, resolution, duedate,
                          timeestimate, timespent. Any Jira field ID accepted.
  --fields-only <list>    Restrict to exactly this comma-separated list
                          (replaces defaults; mutex with --fields)
  --no-history            Omit activity/changelog section
  --no-comments           Omit comments section
  --no-children           Skip the children list (saves one API call)
  --parent                Show this issue's parent instead
                          (Parent Link → Parent → Epic Link, in that order)

The issue view inlines the single latest comment; run 'jiracli show comments
<KEY>' for the full, paginated thread.

Activity shows the newest 10 entries. Long-text fields (description) are
always suppressed; comment/summary/environment are truncated to 120 chars.
Use 'jiracli show history <KEY> --since 7d' for the full paginated changelog.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the effective list of refs: stdin mode, multi, or single.
			refs := args
			if len(args) == 1 && args[0] == "-" {
				// Stdin mode: read one key per line, skip blanks and comments.
				scanner := bufio.NewScanner(os.Stdin)
				refs = nil
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					refs = append(refs, line)
				}
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				if len(refs) == 0 {
					return fmt.Errorf("no keys read from stdin")
				}
			}
			if len(refs) == 0 {
				return fmt.Errorf("requires at least one ref (issue key, browse URL, or compound ref) or - to read from stdin")
			}

			multi := len(refs) > 1
			for i, rawRef := range refs {
				ref, err := jira.ParseRef(rawRef)
				if err != nil {
					if multi {
						fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ %s (%d/%d)\n", rawRef, i+1, len(refs))
						fmt.Fprintf(cmd.OutOrStdout(), "error: invalid ref %q: %v\n\n", rawRef, err)
						continue
					}
					return fmt.Errorf("invalid ref %q: %w", rawRef, err)
				}
				if multi {
					fmt.Fprintf(cmd.OutOrStdout(), "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ %s (%d/%d)\n", ref.Key, i+1, len(refs))
				}
				switch ref.Kind {
				case jira.RefIssue:
					result, err := Issue(cmd.Context(), issueFlags, rawRef)
					if err != nil {
						if multi {
							fmt.Fprintf(cmd.OutOrStdout(), "error: %v\n\n", err)
							continue
						}
						return err
					}
					fmt.Fprint(cmd.OutOrStdout(), result)
					if multi {
						fmt.Fprintln(cmd.OutOrStdout())
					}
				case jira.RefAttachment:
					if multi {
						return fmt.Errorf("attachment refs cannot be used with multiple keys")
					}
					if attachFlags.Stdout {
						return attachmentToStdout(cmd.Context(), attachFlags, rawRef, rawOut)
					}
					result, err := AttachmentDownload(cmd.Context(), attachFlags, rawRef)
					if err != nil {
						return err
					}
					fmt.Fprint(cmd.OutOrStdout(), result)
				case jira.RefComment:
					if multi {
						return fmt.Errorf("comment refs cannot be used with multiple keys")
					}
					result, err := ShowComment(cmd.Context(), ShowCommentFlags{Profile: issueFlags.Profile, JSON: issueFlags.JSON}, rawRef)
					if err != nil {
						return err
					}
					fmt.Fprint(cmd.OutOrStdout(), result)
				}
			}
			return nil
		},
	}

	// Issue flags.
	root.Flags().StringVar(&issueFlags.Profile, "profile", "", "Profile name")
	root.Flags().BoolVar(&issueFlags.JSON, "json", false, "Output NDJSON")
	root.Flags().BoolVar(&issueFlags.NoHistory, "no-history", false, "Skip activity/changelog section")
	root.Flags().BoolVar(&issueFlags.NoComments, "no-comments", false, "Skip comments section")
	root.Flags().StringVar(&issueFlags.Fields, "fields", "", "Add/drop fields: \"name\" or \"+name\" to add, \"-name\" to drop. "+
		"Standard names: reporter, description, labels, components, fixVersions, resolution, duedate, timeestimate, timespent. "+
		"Any Jira field ID accepted. Run jiracli show --help for full reference.")
	root.Flags().StringVar(&issueFlags.FieldsOnly, "fields-only", "", "Restrict to exactly this comma-separated list (replaces defaults; mutex with --fields)")
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
