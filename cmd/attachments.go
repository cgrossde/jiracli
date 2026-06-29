package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// AttachmentsFlags holds parsed flag values for the attachments command.
type AttachmentsFlags struct {
	Profile string
	JSON    bool
}

// NewAttachmentsCmd builds the "attachments" command (list).
func NewAttachmentsCmd() *cobra.Command {
	var flags AttachmentsFlags
	c := &cobra.Command{
		Use:   "attachments <KEY>",
		Short: "List attachments on an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Attachments(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	return c
}

// Attachments is the Layer 1 implementation for the attachments list command.
func Attachments(ctx context.Context, flags AttachmentsFlags, key string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	raw, err := client.GetIssue(ctx, key, "attachment", false)
	if err != nil {
		return "", fmt.Errorf("fetch issue %s: %w", key, err)
	}

	if len(raw.Fields.Attachment) == 0 {
		return fmt.Sprintf("%s has no attachments.\n", key), nil
	}

	if flags.JSON {
		var sb strings.Builder
		for _, a := range raw.Fields.Attachment {
			rec := jira.AttachmentRecord{
				ID:       a.ID,
				Filename: a.Filename,
				MimeType: a.MimeType,
				Size:     a.Size,
				Uploaded: a.Created,
				Author:   a.Author.DisplayName,
			}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}
	var sb strings.Builder
	attachments := raw.Fields.Attachment
	for i, a := range attachments {
		dateStr := a.Created
		if t, err := time.Parse("2006-01-02T15:04:05.999-0700", a.Created); err == nil {
			dateStr = t.Format("2006-01-02")
		}
		fmt.Fprintf(&sb, "  %-3d  %-40s  %8s  %s  (id: %s:attach:%s)\n",
			i+1, a.Filename, jira.FormatBytes(a.Size), dateStr, key, a.ID)
	}
	last := attachments[len(attachments)-1]
	fmt.Fprintf(&sb, "\n  → jiracli show %s:attach:%s\n", key, last.ID)
	return sb.String(), nil
}
