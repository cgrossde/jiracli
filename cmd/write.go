package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/cgrossde/jiracli/internal/jira"
)

// HandleWrite is the shared write lifecycle used by all write commands.
//
//  1. Any ✗ validation row → collect all ✗ messages, return as error (exit 1).
//  2. --yes → Execute → return onSuccess(body).
//  3. No --yes + TTY (promptApply returns ttyAvailable=true) → print preview →
//     prompt → on y: Execute + return preview+"\n"+onSuccess; on n: return preview+"\naborted\n".
//  4. No --yes + no TTY → return preview+"\nApply with: re-run with --yes\n" (exit 0).
func HandleWrite(
	ctx context.Context,
	c *jira.Client,
	baseURL string,
	p jira.Preview,
	yes bool,
	onSuccess func([]byte) string,
) (string, error) {
	// Step 1: block on any failing validation row.
	var errs []string
	for _, v := range p.Validation {
		if v.Status == "✗" {
			errs = append(errs, v.Message)
		}
	}
	if len(errs) > 0 {
		return "", fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	// Render the preview string (used in steps 3 and 4).
	preview := p.Render(baseURL)

	// Step 2: apply immediately when --yes is set.
	if yes {
		body, err := p.Execute(ctx, c)
		if err != nil {
			return "", fmt.Errorf("apply failed: %w", err)
		}
		return onSuccess(body), nil
	}

	// Steps 3 & 4: interactive or non-interactive preview path.
	// promptApply opens /dev/tty, prints "Apply? [y/N]: ", and reads the answer.
	apply, ttyAvailable := promptApply()
	if !ttyAvailable {
		// Non-interactive (CI, piped): print preview and exit 0 — do not apply.
		return preview + "\nApply with: re-run with --yes\n", nil
	}
	if !apply {
		return preview + "\naborted\n", nil
	}
	body, err := p.Execute(ctx, c)
	if err != nil {
		return "", fmt.Errorf("apply failed: %w", err)
	}
	return preview + "\n" + onSuccess(body), nil
}
