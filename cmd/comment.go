package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// CommentAddFlags holds parsed flag values for the comment command.
type CommentAddFlags struct {
	Profile string
	File    string
	Yes     bool
}

// NewCommentCmd builds the "comment" command.
func NewCommentCmd() *cobra.Command {
	var flags CommentAddFlags
	c := &cobra.Command{
		Use:   "comment <KEY> [<body...>]",
		Short: "Add a comment to an issue (dry-run by default; use --yes to apply)",
		Example: `  jiracli add comment ACME-123 "Fixed in latest build"
  jiracli add comment ACME-123 --file note.md --yes
  echo "lgtm" | jiracli add comment ACME-123 - --yes`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			bodyArgs := args[1:]
			result, err := CommentAdd(cmd.Context(), flags, key, bodyArgs, cmd.InOrStdin())
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().StringVar(&flags.File, "file", "", "Read comment body from file")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	return c
}

// CommentAdd is the Layer 1 implementation for adding a comment.
func CommentAdd(ctx context.Context, flags CommentAddFlags, key string, bodyArgs []string, stdin io.Reader) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Resolve body: positional args → --file → stdin
	var body string
	switch {
	case len(bodyArgs) > 0:
		body = strings.Join(bodyArgs, " ")
	case flags.File != "":
		data, err := os.ReadFile(flags.File)
		if err != nil {
			return "", fmt.Errorf("reading --file: %w", err)
		}
		body = strings.TrimSpace(string(data))
	default:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		body = strings.TrimSpace(string(data))
	}
	if body == "" {
		return "", fmt.Errorf("comment body is required")
	}

	// Validation: issue exists
	var validation []jira.ValidationRow
	_, issueErr := client.GetIssue(ctx, key, "key", false)
	if issueErr != nil {
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("%s not found — check the key or your browse permission", key),
		})
	} else {
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: key + " exists",
		})
	}

	// Current user for the effect description
	currentUser := entry.DisplayName
	if currentUser == "" {
		currentUser = entry.User
	}

	p := jira.Preview{
		Method:      "POST",
		Path:        "/issue/" + key + "/comment",
		Body:        map[string]string{"body": body},
		Description: fmt.Sprintf("+ 1 comment on %s by %s", key, currentUser),
		Validation:  validation,
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(resp []byte) string {
		var r struct {
			ID string `json:"id"`
		}
		json.Unmarshal(resp, &r)
		return fmt.Sprintf("✓ commented on %s (id %s)\n  → jiracli show %s\n", key, r.ID, key)
	})
}
