package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// TransitionsFlags holds parsed flag values for the transitions command.
type TransitionsFlags struct {
	Profile string
	JSON    bool
}

// NewTransitionsCmd builds the "transitions" command.
func NewTransitionsCmd() *cobra.Command {
	var flags TransitionsFlags
	c := &cobra.Command{
		Use:   "transitions <KEY>",
		Short: "List available workflow transitions for an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Transitions(cmd.Context(), flags, args[0])
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

// transitionRecord is the NDJSON schema for one transition.
type transitionRecord struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ToStatus         string `json:"toStatus"`
	ToStatusCategory string `json:"toStatusCategory"`
}

// Transitions is the Layer 1 implementation for the transitions command.
func Transitions(ctx context.Context, flags TransitionsFlags, key string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	// Fetch current status for display.
	raw, err := client.GetIssue(ctx, key, "status", false)
	if err != nil {
		// GetIssue's error already carries "fetch issue <key>: ..." context.
		return "", err
	}
	currentStatus := raw.Fields.Status.Name

	// Fetch transitions.
	transitions, err := client.GetTransitions(ctx, key)
	if err != nil {
		return "", fmt.Errorf("fetch transitions %s: %w", key, err)
	}

	if flags.JSON {
		var sb strings.Builder
		for _, t := range transitions {
			rec := transitionRecord{
				ID:               t.ID,
				Name:             t.Name,
				ToStatus:         t.To.Name,
				ToStatusCategory: t.To.StatusCategory.Name,
			}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	// Plain text: hint before + list + hint after so both head and tail readers see it.
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s  current: %s\n\n", key, currentStatus)

	// Example hint before the list.
	if len(transitions) > 0 {
		first := transitions[0]
		arg := first.ID
		if !strings.Contains(first.Name, " ") {
			arg = first.Name
		}
		fmt.Fprintf(&sb, "→ jiracli edit status %s %s  # (use any id or name below)\n\n", key, arg)
	}

	var lastArg string
	for _, t := range transitions {
		arg := t.ID
		if !strings.Contains(t.Name, " ") {
			arg = t.Name
		}
		lastArg = arg
		fmt.Fprintf(&sb, "  %-6s  %s\n", t.ID, t.Name)
	}

	// Example hint after the list.
	if len(transitions) > 0 {
		fmt.Fprintf(&sb, "\n→ jiracli edit status %s %s  # (use any id or name above)\n", key, lastArg)
	}
	return sb.String(), nil
}
