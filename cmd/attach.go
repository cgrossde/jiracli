package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// AttachFlags holds parsed flag values for the attach command.
type AttachFlags struct {
	Profile string
	Yes     bool
}

// NewAttachCmd builds the "attach" command.
func NewAttachCmd() *cobra.Command {
	var flags AttachFlags
	c := &cobra.Command{
		Use:   "attachment <KEY> <file...>",
		Short: "Upload one or more files as attachments (dry-run by default; use --yes to apply)",
		Example: `  jiracli add attachment ACME-123 screenshot.png
  jiracli add attachment ACME-123 a.log b.log --yes`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Attach(cmd.Context(), flags, args[0], args[1:])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	return c
}

// Attach is the Layer 1 implementation for uploading attachments.
func Attach(ctx context.Context, flags AttachFlags, key string, filePaths []string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Validate files exist and collect sizes.
	type fileInfo struct {
		path string
		name string
		size int64
	}
	var files []fileInfo
	var totalSize int64
	var validation []jira.ValidationRow

	for _, path := range filePaths {
		stat, err := os.Stat(path)
		if err != nil {
			validation = append(validation, jira.ValidationRow{Status: "✗", Message: fmt.Sprintf("%s: %v", path, err)})
			continue
		}
		files = append(files, fileInfo{path: path, name: stat.Name(), size: stat.Size()})
		totalSize += stat.Size()
		validation = append(validation, jira.ValidationRow{Status: "✓", Message: fmt.Sprintf("%s (%s)", stat.Name(), jira.FormatBytes(stat.Size()))})
	}

	const maxBytes = 100 * 1024 * 1024 // 100 MB warning threshold
	if totalSize > maxBytes {
		validation = append(validation, jira.ValidationRow{
			Status:  "⚠",
			Message: fmt.Sprintf("total size %s exceeds 100 MB — server may reject", jira.FormatBytes(totalSize)),
		})
	}

	// Build human-readable description.
	var fileNames []string
	for _, f := range files {
		fileNames = append(fileNames, fmt.Sprintf("%s (%s)", f.name, jira.FormatBytes(f.size)))
	}
	desc := fmt.Sprintf("Files: %s — upload to %s", strings.Join(fileNames, ", "), key)

	// Preview body is a summary (no binary content).
	bodyDesc := map[string]any{
		"issue": key,
		"files": fileNames,
	}

	p := jira.Preview{
		Method:      "POST",
		Path:        "/issue/" + key + "/attachments",
		Body:        bodyDesc,
		Description: desc,
		Validation:  validation,
	}

	// doUpload performs the actual uploads and returns the success lines.
	doUpload := func() (string, error) {
		var results []string
		for _, f := range files {
			meta, err := client.UploadAttachment(ctx, key, f.path)
			if err != nil {
				return "", fmt.Errorf("uploading %s: %w", f.name, err)
			}
			results = append(results, fmt.Sprintf("✓ attached %s as %s:attach:%s (%s)",
				meta.Filename, key, meta.ID, jira.FormatBytes(meta.Size)))
		}
		results = append(results, fmt.Sprintf("  → jiracli show %s", key))
		return strings.Join(results, "\n") + "\n", nil
	}

	// Use HandleWrite for the standard dry-run / --yes / TTY prompt lifecycle.
	// onSuccess is not used here because we need to upload each file individually
	// rather than calling p.Execute once. We replicate the lifecycle manually.
	//
	// Check validation ✗ rows (HandleWrite step 1).
	var errs []string
	for _, v := range validation {
		if v.Status == "✗" {
			errs = append(errs, v.Message)
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	preview := p.Render(entry.URL)

	if flags.Yes {
		return doUpload()
	}

	apply, ttyAvailable := promptApply()
	if !ttyAvailable {
		return preview + "\nApply with: re-run with --yes\n", nil
	}
	if !apply {
		return preview + "\naborted\n", nil
	}
	result, err := doUpload()
	if err != nil {
		return "", err
	}
	return preview + "\n" + result, nil
}
