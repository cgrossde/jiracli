package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// DeleteIssueFlags holds flags for the delete issue operation.
type DeleteIssueFlags struct {
	Profile      string
	WithSubtasks bool
	Yes          bool
}

// DeleteIssue is the Layer 1 implementation for deleting an issue.
// It inlines the HandleWrite lifecycle because it needs a query parameter
// (deleteSubtasks) that Preview.Execute does not support.
func DeleteIssue(ctx context.Context, flags DeleteIssueFlags, key string) (string, error) {
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

	// Fetch the issue to validate it exists and inspect subtasks.
	var validation []jira.ValidationRow
	raw, gerr := client.GetIssue(ctx, key, "key,summary,subtasks", false)
	if gerr != nil {
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("%s not found", key),
		})
	} else {
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: fmt.Sprintf("%s: %q", key, raw.Fields.Summary),
		})
		if n := len(raw.Fields.Subtasks); n > 0 {
			if !flags.WithSubtasks {
				validation = append(validation, jira.ValidationRow{
					Status:  "✗",
					Message: fmt.Sprintf("%s has %d subtask(s) — pass --with-subtasks to cascade", key, n),
				})
			} else {
				validation = append(validation, jira.ValidationRow{
					Status:  "⚠",
					Message: fmt.Sprintf("will also delete %d subtask(s)", n),
				})
			}
		}
	}

	// Step 1: fail on any ✗ row.
	var errs []string
	for _, v := range validation {
		if v.Status == "✗" {
			errs = append(errs, v.Message)
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Build preview text matching Preview.Render shape.
	path := "/issue/" + key
	queryPart := ""
	if flags.WithSubtasks {
		queryPart = "?deleteSubtasks=true"
	}
	var sb strings.Builder
	sb.WriteString("DRY RUN — no changes made.\n\n")
	fmt.Fprintf(&sb, "DELETE %s/rest/api/2%s%s\n\n", entry.URL, path, queryPart)
	sb.WriteString("Effect:\n  − issue " + key)
	if flags.WithSubtasks && gerr == nil && len(raw.Fields.Subtasks) > 0 {
		fmt.Fprintf(&sb, " (and %d subtask(s))", len(raw.Fields.Subtasks))
	}
	sb.WriteString("\n\nValidation:\n")
	for _, v := range validation {
		fmt.Fprintf(&sb, "  %s %s\n", v.Status, v.Message)
	}
	preview := sb.String()

	// Step 2: --yes path.
	if flags.Yes {
		if err := client.DeleteIssue(ctx, key, flags.WithSubtasks); err != nil {
			return "", fmt.Errorf("apply failed: %w", err)
		}
		return fmt.Sprintf("✓ deleted issue %s\n", key), nil
	}

	// Steps 3 & 4: TTY or non-interactive preview.
	apply, ttyAvailable := promptApply()
	if !ttyAvailable {
		return preview + "\nApply with: re-run with --yes\n", nil
	}
	if !apply {
		return preview + "\naborted\n", nil
	}
	if err := client.DeleteIssue(ctx, key, flags.WithSubtasks); err != nil {
		return "", fmt.Errorf("apply failed: %w", err)
	}
	return preview + "\n" + fmt.Sprintf("✓ deleted issue %s\n", key), nil
}

// DeleteIssueBulk deletes multiple issues.
//
// Dry-run: validates every key exists (and checks for subtasks), prints a
// combined table, then prompts once (TTY) or exits with "Apply with: --yes".
// On --yes: executes sequentially, collects per-key errors, reports all at end.
func DeleteIssueBulk(ctx context.Context, flags DeleteIssueFlags, keys []string) (string, error) {
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

	// Phase 1: validate every key exists; collect summaries.
	type row struct {
		key      string
		summary  string
		subtasks int
		invalid  bool
		errMsg   string
	}
	rows := make([]row, len(keys))
	var blockErrs []string
	for i, key := range keys {
		raw, gerr := client.GetIssue(ctx, key, "key,summary,subtasks", false)
		if gerr != nil {
			rows[i] = row{key: key, invalid: true, errMsg: fmt.Sprintf("%s not found", key)}
			blockErrs = append(blockErrs, "  "+rows[i].errMsg)
			continue
		}
		n := len(raw.Fields.Subtasks)
		if n > 0 && !flags.WithSubtasks {
			rows[i] = row{key: key, invalid: true, errMsg: fmt.Sprintf("%s has %d subtask(s) — pass --with-subtasks to cascade", key, n)}
			blockErrs = append(blockErrs, "  "+rows[i].errMsg)
			continue
		}
		rows[i] = row{key: key, summary: raw.Fields.Summary, subtasks: n}
	}
	if len(blockErrs) > 0 {
		return "", fmt.Errorf("cannot proceed — resolve the following first:\n%s", strings.Join(blockErrs, "\n"))
	}

	// Phase 2: preview.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("delete %d issue(s):\n\n", len(rows)))
	for _, r := range rows {
		line := fmt.Sprintf("  − %-16s  %q", r.key, r.summary)
		if r.subtasks > 0 {
			line += fmt.Sprintf("  ⚠ +%d subtask(s)", r.subtasks)
		}
		sb.WriteString(line + "\n")
	}
	preview := sb.String()

	// Phase 3: apply or return preview.
	doApply := flags.Yes
	if !doApply {
		apply, ttyAvailable := promptApply()
		if !ttyAvailable {
			return preview + "\nApply with: re-run with --yes\n", nil
		}
		if !apply {
			return preview + "\naborted\n", nil
		}
		doApply = true
	}
	_ = doApply

	// Phase 4: execute sequentially, collect errors.
	var applyErrs []string
	var okKeys []string
	for _, r := range rows {
		if err := client.DeleteIssue(ctx, r.key, flags.WithSubtasks); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("  %s: %v", r.key, err))
		} else {
			okKeys = append(okKeys, r.key)
		}
	}

	var out strings.Builder
	out.WriteString(preview + "\n")
	for _, key := range okKeys {
		out.WriteString(fmt.Sprintf("✓ deleted %s\n", key))
	}
	if len(applyErrs) > 0 {
		out.WriteString("\nFailed:\n")
		for _, e := range applyErrs {
			out.WriteString(e + "\n")
		}
		return out.String(), fmt.Errorf("%d of %d deletes failed", len(applyErrs), len(rows))
	}
	return out.String(), nil
}
