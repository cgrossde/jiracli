package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/cgrossde/jiracli/internal/jira"
)

// AttachmentDownloadFlags holds parsed flag values for the attachment command.
type AttachmentDownloadFlags struct {
	Profile  string
	Stdout   bool   // -o: stream content to stdout
	FilePath string // -f: save to this path
}

// attachmentToStdout streams the attachment body directly to w (the real stdout,
// bypassing the presenter's in-memory buffer).
// Returns ErrAlreadyPresented so the presenter skips footer/error output.
func attachmentToStdout(ctx context.Context, flags AttachmentDownloadFlags, ref string, w io.Writer) error {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefAttachment {
		return fmt.Errorf("ref must be KEY:attach:ID form — run: jiracli show attachments %s", extractKeyFromRef(ref))
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return err
	}
	client := jira.New(entry)
	meta, err := client.GetAttachmentMeta(ctx, parsed.AttachmentID)
	if err != nil {
		return fmt.Errorf("fetch attachment metadata: %w", err)
	}
	if err := client.StreamAttachment(ctx, meta, w); err != nil {
		return fmt.Errorf("stream attachment: %w", err)
	}
	return ErrAlreadyPresented
}

// AttachmentDownload is the Layer 1 implementation for metadata display and file download.
func AttachmentDownload(ctx context.Context, flags AttachmentDownloadFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefAttachment {
		return "", fmt.Errorf("ref must be KEY:attach:ID form — run: jiracli show attachments %s", extractKeyFromRef(ref))
	}

	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	meta, err := client.GetAttachmentMeta(ctx, parsed.AttachmentID)
	if err != nil {
		return "", fmt.Errorf("fetch attachment metadata: %w", err)
	}

	// No flags: print metadata only.
	if flags.FilePath == "" {
		return fmt.Sprintf(
			"%s:attach:%s  %s\n  mime: %s  size: %d bytes\n  created: %s by %s (%s)\n  url: %s\nTo print contents:\n  → jiracli show %s -o\nTo download:\n  → jiracli show %s -f %s\n",
			parsed.Key, parsed.AttachmentID, meta.Filename,
			meta.MimeType, meta.Size,
			meta.Created, meta.Author.DisplayName, meta.Author.Name,
			meta.Content,
			ref,
			ref, meta.Filename,
		), nil
	}

	path, err := client.DownloadAttachment(ctx, meta, flags.FilePath)
	if err != nil {
		return "", fmt.Errorf("download attachment: %w", err)
	}
	return fmt.Sprintf("✓ saved %s\n", path), nil
}

// extractKeyFromRef attempts to pull an issue key from a ref string for use in
// error hints. Returns the raw ref if no key can be identified.
func extractKeyFromRef(ref string) string {
	// Try parsing as a plain issue ref to surface a useful key in the hint.
	if r, err := jira.ParseRef(ref); err == nil {
		return r.Key
	}
	// Ref may be malformed; return it as-is so the hint still runs.
	if len(ref) > 20 {
		return ref[:20]
	}
	return ref
}
