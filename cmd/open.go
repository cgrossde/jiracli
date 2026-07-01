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
		Use:   "open <ref>",
		Short: "Open an issue, comment, or attachment in the browser",
		Long: `Open resolves a Jira reference and launches it in the default browser.

Supported reference forms:
  ACME-123                        issue
  ACME-123:comment:NNN            specific comment
  ACME-123:attach:NNN             attachment (fetches filename for URL)
  https://host/browse/ACME-123    browse URL

Use --print-url to print the resolved URL instead of opening it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Open(cmd.Context(), flags, args[0], cmd)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
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
