package cmd

import (
	"context"
	"fmt"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// DeleteLinkFlags holds flags for the delete link operation.
type DeleteLinkFlags struct {
	Profile string
	Yes     bool
}

// DeleteLink is the Layer 1 implementation for deleting an issue link.
// ref must be in the form KEY:link:ID.
func DeleteLink(ctx context.Context, flags DeleteLinkFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefLink {
		return "", fmt.Errorf("expected KEY:link:ID, got %q", ref)
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

	// Jira has no GET /issueLink/:id endpoint, so we cannot validate existence.
	validation := []jira.ValidationRow{
		{
			Status:  "⚠",
			Message: "link existence not checked — 404 on apply means already gone",
		},
	}

	p := jira.Preview{
		Method:      "DELETE",
		Path:        "/issueLink/" + parsed.LinkID,
		Description: fmt.Sprintf("− issue link %s from %s (id: %s)", parsed.LinkID, parsed.Key, parsed.LinkID),
		Validation:  validation,
	}
	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ deleted issue link %s from %s\n", parsed.LinkID, parsed.Key)
	})
}
