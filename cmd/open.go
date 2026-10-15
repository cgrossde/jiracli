package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/browser"
	"github.com/cgrossde/jiracli/internal/jira"
)

// OpenFlags holds parsed flag values for the open command.
type OpenFlags struct {
	Profile  string
	PrintURL bool
}

// NewOpenCmd builds the "open" command.
func NewOpenCmd() *cobra.Command {
	var flags OpenFlags
	c := &cobra.Command{
		Use:   "open <ref> [ref...]",
		Short: "Open one or more issues, comments, or attachments in the browser",
		Long: `Open resolves one or more Jira references and launches each in the default browser.

Supported reference forms:
  ACME-123                        issue
  ACME-123:comment:NNN            specific comment
  ACME-123:attach:NNN             attachment (fetches filename for URL; single ref only)
  https://host/browse/ACME-123    browse URL

Use --print-url to print the resolved URLs instead of opening them.

Set JIRACLI_BROWSER to choose a specific browser, for example:
  JIRACLI_BROWSER=edge jiracli open ACME-123`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var out strings.Builder
			var errs []string
			for _, ref := range args {
				result, err := Open(cmd.Context(), flags, ref, cmd)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", ref, err))
					continue
				}
				out.WriteString(result)
			}
			if out.Len() > 0 {
				fmt.Fprint(cmd.OutOrStdout(), out.String())
			}
			if len(errs) > 0 {
				return fmt.Errorf("%s", strings.Join(errs, "\n"))
			}
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.PrintURL, "print-url", false, "Print URL instead of opening the browser")
	return c
}

// Open is the Layer 1 implementation for the open command.
// It resolves the ref to a URL, optionally launches the browser, and returns
// the URL string as a single line for the presenter.
func Open(ctx context.Context, flags OpenFlags, ref string, cmd *cobra.Command) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	parsed, err := jira.ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("invalid reference %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, or a browse URL", ref)
	}

	baseURL := strings.TrimRight(entry.URL, "/")
	targetURL, err := buildOpenURL(ctx, jira.New(entry), baseURL, parsed)
	if err != nil {
		return "", err
	}

	if flags.PrintURL {
		return targetURL + "\n", nil
	}

	// Attempt to open. On failure, return the URL with a parenthetical note.
	if openErr := browser.Open(targetURL); openErr != nil {
		return fmt.Sprintf("%s\n(could not auto-open: %s)\n", targetURL, openErr.Error()), nil
	}
	return targetURL + "\n", nil
}

// buildOpenURL constructs the full Jira URL for the given Ref.
func buildOpenURL(ctx context.Context, client *jira.Client, baseURL string, ref jira.Ref) (string, error) {
	switch ref.Kind {
	case jira.RefIssue:
		return baseURL + "/browse/" + ref.Key, nil

	case jira.RefComment:
		u := baseURL + "/browse/" + ref.Key +
			"?focusedCommentId=" + ref.CommentID +
			"&page=com.atlassian.jira.plugin.system.issuetabpanels:comment-tabpanel"
		return u, nil

	case jira.RefAttachment:
		meta, err := client.GetAttachmentMeta(ctx, ref.AttachmentID)
		if err != nil {
			return "", fmt.Errorf("fetch attachment metadata for open: %w", err)
		}
		return fmt.Sprintf("%s/secure/attachment/%s/%s",
			baseURL,
			ref.AttachmentID,
			url.PathEscape(meta.Filename),
		), nil

	default:
		return "", fmt.Errorf("unsupported ref kind: %d", ref.Kind)
	}
}
