package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// TransitionFlags holds parsed flag values for the transition write command.
type TransitionFlags struct {
	Profile string
	Comment string
	Yes     bool
}

// NewTransitionCmd builds the "transition" write command.
func NewTransitionCmd() *cobra.Command {
	var flags TransitionFlags
	c := &cobra.Command{
		Use:   "status <KEY> [KEY...] <name-or-id>",
		Short: "Move one or more issues through a workflow transition (dry-run by default; use --yes to apply)",
		Long: `Move one or more issues through a workflow transition (dry-run by default; use --yes to apply).

The last argument is always the transition name or numeric ID. All preceding
arguments are treated as issue keys. With multiple keys the preview lists every
planned operation; execution is sequential and per-key errors are collected
rather than aborting the whole batch.

See also:
  jiracli show transitions <KEY>    list valid target states for this issue`,
		Example: `  jiracli edit status ACME-123 Done
  jiracli edit status ACME-123 ACME-124 ACME-125 "In Review" --yes
  jiracli edit status ACME-123 Done --comment "shipped" --yes`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := args[:len(args)-1]
			nameOrID := args[len(args)-1]
			if len(keys) == 1 {
				result, err := Transition(cmd.Context(), flags, keys[0], nameOrID)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
				return nil
			}
			result, err := TransitionBulk(cmd.Context(), flags, keys, nameOrID)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().StringVar(&flags.Comment, "comment", "", "Comment to post atomically with the transition")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	return c
}

// Transition is the Layer 1 implementation for the transition write command.
func Transition(ctx context.Context, flags TransitionFlags, key, nameOrID string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Fetch current status for display and validation rows.
	raw, err := client.GetIssue(ctx, key, "status", false)
	if err != nil {
		return "", fmt.Errorf("fetch issue %s: %w", key, err)
	}
	currentStatus := raw.Fields.Status.Name

	// Fetch available transitions.
	transitions, err := client.GetTransitions(ctx, key)
	if err != nil {
		return "", fmt.Errorf("fetch transitions for %s: %w", key, err)
	}

	// Resolve name-or-id to a specific transition.
	var t *jira.TransitionRaw
	if _, numErr := strconv.Atoi(nameOrID); numErr == nil {
		// Numeric ID — look up by exact id.
		for i := range transitions {
			if transitions[i].ID == nameOrID {
				t = &transitions[i]
				break
			}
		}
	} else {
		// Name match — case-insensitive, exact.
		lower := strings.ToLower(nameOrID)
		var matches []jira.TransitionRaw
		for _, tr := range transitions {
			if strings.ToLower(tr.Name) == lower {
				matches = append(matches, tr)
			}
		}
		if len(matches) == 1 {
			t = &matches[0]
		}
	}

	if t == nil {
		// Build corrective error per proposal §4.3 lines 499-507.
		var lines []string
		lines = append(lines, fmt.Sprintf("no transition %q on %s (current: %s)", nameOrID, key, currentStatus))
		lines = append(lines, "Available transitions:")
		for _, tr := range transitions {
			lines = append(lines, fmt.Sprintf("  %-6s  %s", tr.ID, tr.Name))
		}
		lines = append(lines, fmt.Sprintf("Run: jiracli edit status %s <id>", key))
		return "", fmt.Errorf("%s", strings.Join(lines, "\n"))
	}

	targetStatus := t.To.Name

	body := map[string]any{
		"transition": map[string]string{"id": t.ID},
	}
	if flags.Comment != "" {
		body["update"] = map[string]any{
			"comment": []map[string]any{
				{"add": map[string]string{"body": flags.Comment}},
			},
		}
	}

	p := jira.Preview{
		Method:      "POST",
		Path:        "/issue/" + key + "/transitions",
		Body:        body,
		Description: fmt.Sprintf("%s: %s → %s", key, currentStatus, targetStatus),
		Validation: []jira.ValidationRow{
			{Status: "✓", Message: fmt.Sprintf("transition %s %q is available on %s", t.ID, t.Name, key)},
			{Status: "✓", Message: fmt.Sprintf("%s → %s", currentStatus, targetStatus)},
		},
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ transitioned %s: %s → %s\n  → jiracli show %s\n", key, currentStatus, targetStatus, key)
	})
}

// TransitionBulk moves multiple issues through the same transition.
//
// Dry-run: resolves the transition for every key (sequential API calls), prints
// a combined preview table, then either prompts once (TTY) or exits with
// "Apply with: --yes". On --yes: executes sequentially, collects per-key
// errors, and reports them all at the end rather than aborting on first failure.
func TransitionBulk(ctx context.Context, flags TransitionFlags, keys []string, nameOrID string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Phase 1: resolve transition for every key.
	type resolved struct {
		key           string
		currentStatus string
		targetStatus  string
		transitionID  string
	}
	var rows []resolved
	var lookupErrs []string
	for _, key := range keys {
		raw, err := client.GetIssue(ctx, key, "status", false)
		if err != nil {
			lookupErrs = append(lookupErrs, fmt.Sprintf("  %s: fetch failed: %v", key, err))
			continue
		}
		currentStatus := raw.Fields.Status.Name
		transitions, err := client.GetTransitions(ctx, key)
		if err != nil {
			lookupErrs = append(lookupErrs, fmt.Sprintf("  %s: fetch transitions failed: %v", key, err))
			continue
		}
		var t *jira.TransitionRaw
		if _, numErr := strconv.Atoi(nameOrID); numErr == nil {
			for i := range transitions {
				if transitions[i].ID == nameOrID {
					t = &transitions[i]
					break
				}
			}
		} else {
			lower := strings.ToLower(nameOrID)
			for i := range transitions {
				if strings.ToLower(transitions[i].Name) == lower {
					t = &transitions[i]
					break
				}
			}
		}
		if t == nil {
			lookupErrs = append(lookupErrs, fmt.Sprintf("  %s: no transition %q available (current: %s)", key, nameOrID, currentStatus))
			continue
		}
		rows = append(rows, resolved{
			key:           key,
			currentStatus: currentStatus,
			targetStatus:  t.To.Name,
			transitionID:  t.ID,
		})
	}
	if len(lookupErrs) > 0 && len(rows) == 0 {
		return "", fmt.Errorf("all keys failed resolution:\n%s", strings.Join(lookupErrs, "\n"))
	}

	// Phase 2: build preview.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("transition %d issue(s) → %s\n\n", len(rows), nameOrID))
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("  %-16s  %s → %s\n", r.key, r.currentStatus, r.targetStatus))
	}
	if len(lookupErrs) > 0 {
		sb.WriteString("\nSkipped (resolution failed):\n")
		for _, e := range lookupErrs {
			sb.WriteString(e + "\n")
		}
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

	// Phase 4: execute sequentially, collect errors.
	var applyErrs []string
	var okKeys []string
	for _, r := range rows {
		body := map[string]any{
			"transition": map[string]string{"id": r.transitionID},
		}
		if flags.Comment != "" {
			body["update"] = map[string]any{
				"comment": []map[string]any{
					{"add": map[string]string{"body": flags.Comment}},
				},
			}
		}
		p := jira.Preview{
			Method: "POST",
			Path:   "/issue/" + r.key + "/transitions",
			Body:   body,
		}
		if _, err := p.Execute(ctx, client); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("  %s: %v", r.key, err))
		} else {
			okKeys = append(okKeys, r.key)
		}
	}

	var out strings.Builder
	out.WriteString(preview + "\n")
	for _, key := range okKeys {
		out.WriteString(fmt.Sprintf("✓ transitioned %s\n", key))
	}
	if len(applyErrs) > 0 {
		out.WriteString("\nFailed:\n")
		for _, e := range applyErrs {
			out.WriteString(e + "\n")
		}
		return out.String(), fmt.Errorf("%d of %d transitions failed", len(applyErrs), len(rows))
	}
	return out.String(), nil
}
