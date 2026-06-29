package cmd

import (
	"context"
	"fmt"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// DeleteCommentFlags holds flags for the delete comment operation.
type DeleteCommentFlags struct {
	Profile string
	Yes     bool
}

// DeleteComment is the Layer 1 implementation for deleting a comment.
func DeleteComment(ctx context.Context, flags DeleteCommentFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefComment {
		return "", fmt.Errorf("expected KEY:comment:ID, got %q", ref)
	}

	profile := flags.Profile
	if profile == "" {
		var perr error
		profile, perr = keychain.ResolveDefault()
		if perr != nil {
			return "", fmt.Errorf("no credentials — run: jiracli setup")
		}
	}
	entry, err := keychain.Load(profile)
	if err != nil {
		return "", fmt.Errorf("credentials not found for profile %q — run: jiracli setup", profile)
	}
	client := jira.New(entry)

	var validation []jira.ValidationRow
	cmt, cerr := client.GetComment(ctx, parsed.Key, parsed.CommentID)
	if cerr != nil {
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("comment %s on %s not found", parsed.CommentID, parsed.Key),
		})
	} else {
		snippet := cmt.Body
		if len([]rune(snippet)) > 60 {
			snippet = string([]rune(snippet)[:60]) + "…"
		}
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: fmt.Sprintf("comment by %s: %q", cmt.Author.DisplayName, snippet),
		})
	}

	p := jira.Preview{
		Method:      "DELETE",
		Path:        "/issue/" + parsed.Key + "/comment/" + parsed.CommentID,
		Description: fmt.Sprintf("− 1 comment on %s (id: %s)", parsed.Key, parsed.CommentID),
		Validation:  validation,
	}
	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ deleted comment %s on %s\n", parsed.CommentID, parsed.Key)
	})
}
