package cmd

import (
	"context"
	"fmt"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// DeleteAttachmentFlags holds flags for the delete attachment operation.
type DeleteAttachmentFlags struct {
	Profile string
	Yes     bool
}

// DeleteAttachment is the Layer 1 implementation for deleting an attachment.
func DeleteAttachment(ctx context.Context, flags DeleteAttachmentFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefAttachment {
		return "", fmt.Errorf("expected KEY:attach:ID, got %q", ref)
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
	var filename string
	meta, merr := client.GetAttachmentMeta(ctx, parsed.AttachmentID)
	if merr != nil {
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("attachment %s on %s not found", parsed.AttachmentID, parsed.Key),
		})
	} else {
		filename = meta.Filename
		validation = append(validation, jira.ValidationRow{
			Status: "✓",
			Message: fmt.Sprintf("attachment %s (%s, %d bytes, uploaded by %s)",
				meta.Filename, meta.MimeType, meta.Size, meta.Author.DisplayName),
		})
	}

	desc := fmt.Sprintf("− attachment %s from %s (id: %s)", filename, parsed.Key, parsed.AttachmentID)
	if filename == "" {
		desc = fmt.Sprintf("− attachment from %s (id: %s)", parsed.Key, parsed.AttachmentID)
	}

	p := jira.Preview{
		Method:      "DELETE",
		Path:        "/attachment/" + parsed.AttachmentID,
		Description: desc,
		Validation:  validation,
	}
	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ deleted attachment %s from %s\n", parsed.AttachmentID, parsed.Key)
	})
}
