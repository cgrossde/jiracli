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
		Use:   "status <KEY> <name-or-id>",
		Short: "Move an issue through a workflow transition (dry-run by default; use --yes to apply)",
		Long: `Move an issue through a workflow transition (dry-run by default; use --yes to apply).

See also:
  jiracli show transitions <KEY>    list valid target states for this issue`,
		Example: `  jiracli edit status ACME-123 31
  jiracli edit status ACME-123 Done --yes
  jiracli edit status ACME-123 "In Review" --comment "PR up" --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Transition(cmd.Context(), flags, args[0], args[1])
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
