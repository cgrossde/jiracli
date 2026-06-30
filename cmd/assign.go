package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type AssignFlags struct {
	Profile string
	Yes     bool
}

func NewAssignCmd() *cobra.Command {
	var flags AssignFlags
	c := &cobra.Command{
		Use:   "assignee <KEY> [KEY...] <user-or-id>",
		Short: "Assign one or more issues to a user (dry-run by default; use --yes to apply)",
		Long: `Assign one or more issues to a user (dry-run by default; use --yes to apply).

The last argument is always the user or ID. All preceding arguments are issue
keys. With multiple keys the preview lists all planned assignments; execution is
sequential with per-key error collection rather than aborting on first failure.

See also:
  jiracli lookup users --project <KEY>    find a valid username`,
		Example: `  jiracli edit assignee ACME-123 me
  jiracli edit assignee ACME-123 ACME-124 ACME-125 me --yes
  jiracli edit assignee ACME-123 -          # unassign
  jiracli edit assignee ACME-123 alex --yes`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := args[:len(args)-1]
			userOrID := args[len(args)-1]
			if len(keys) == 1 {
				result, err := Assign(cmd.Context(), flags, keys[0], userOrID)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
				return nil
			}
			result, err := AssignBulk(cmd.Context(), flags, keys, userOrID)
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

func Assign(ctx context.Context, flags AssignFlags, key, userOrID string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	var resolvedName string
	var displayName string
	var validation []jira.ValidationRow

	switch userOrID {
	case "-":
		// unassign
		resolvedName = ""
		displayName = "(unassigned)"
		validation = []jira.ValidationRow{{Status: "✓", Message: "unassign " + key}}
	case "me":
		// resolve to current user
		u, err := client.Myself(ctx)
		if err != nil {
			return "", fmt.Errorf("resolving 'me': %w", err)
		}
		resolvedName = u.Name
		displayName = u.DisplayName
		validation = []jira.ValidationRow{{Status: "✓", Message: fmt.Sprintf("resolved 'me' to %s (%s)", u.DisplayName, u.Name)}}
	default:
		// Derive project key from issue key (everything before the last '-')
		parts := strings.Split(key, "-")
		projectKey := strings.Join(parts[:len(parts)-1], "-")
		users, err := client.SearchAssignableUsers(ctx, projectKey, userOrID, 10)
		if err != nil {
			return "", fmt.Errorf("searching users: %w", err)
		}

		switch len(users) {
		case 0:
			return "", fmt.Errorf("no user matching %q in project %s — run: jiracli lookup users %s --project %s", userOrID, projectKey, userOrID, projectKey)
		case 1:
			resolvedName = users[0].Name
			displayName = users[0].DisplayName
			validation = []jira.ValidationRow{{Status: "✓", Message: fmt.Sprintf("resolved %q to %s (%s)", userOrID, users[0].DisplayName, users[0].Name)}}
		default:
			var lines []string
			lines = append(lines, fmt.Sprintf("ambiguous user %q — %d matches:", userOrID, len(users)))
			max := len(users)
			if max > 8 {
				max = 8
			}
			for _, u := range users[:max] {
				lines = append(lines, fmt.Sprintf("  %s  %s", u.Name, u.DisplayName))
				lines = append(lines, fmt.Sprintf("  Try: jiracli edit assignee %s %s", key, u.Name))
			}
			return "", fmt.Errorf("%s", strings.Join(lines, "\n"))
		}
	}

	var bodyMap any
	if resolvedName == "" {
		bodyMap = map[string]any{"name": nil}
	} else {
		bodyMap = map[string]string{"name": resolvedName}
	}

	p := jira.Preview{
		Method:      "PUT",
		Path:        "/issue/" + key + "/assignee",
		Body:        bodyMap,
		Description: fmt.Sprintf("assign %s to %s", key, displayName),
		Validation:  validation,
	}

	successMsg := fmt.Sprintf("✓ assigned %s to %s (%s)\n  → jiracli show %s\n", key, displayName, resolvedName, key)
	if resolvedName == "" {
		successMsg = fmt.Sprintf("✓ unassigned %s\n  → jiracli show %s\n", key, key)
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return successMsg
	})
}

// AssignBulk assigns multiple issues to the same user.
//
// Dry-run: resolves the user once (using the first key for project context),
// builds a preview listing all keys, then prompts once (TTY) or exits with
// "Apply with: --yes". On --yes: executes sequentially with per-key error
// collection rather than aborting on first failure.
func AssignBulk(ctx context.Context, flags AssignFlags, keys []string, userOrID string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Resolve the user once (project context from first key).
	var resolvedName string
	var displayName string
	switch userOrID {
	case "-":
		resolvedName = ""
		displayName = "(unassigned)"
	case "me":
		u, err := client.Myself(ctx)
		if err != nil {
			return "", fmt.Errorf("resolving 'me': %w", err)
		}
		resolvedName = u.Name
		displayName = u.DisplayName
	default:
		parts := strings.Split(keys[0], "-")
		projectKey := strings.Join(parts[:len(parts)-1], "-")
		users, err := client.SearchAssignableUsers(ctx, projectKey, userOrID, 10)
		if err != nil {
			return "", fmt.Errorf("searching users: %w", err)
		}
		switch len(users) {
		case 0:
			return "", fmt.Errorf("no user matching %q in project %s — run: jiracli lookup users %s --project %s", userOrID, projectKey, userOrID, projectKey)
		case 1:
			resolvedName = users[0].Name
			displayName = users[0].DisplayName
		default:
			var lines []string
			lines = append(lines, fmt.Sprintf("ambiguous user %q — %d matches:", userOrID, len(users)))
			max := len(users)
			if max > 8 {
				max = 8
			}
			for _, u := range users[:max] {
				lines = append(lines, fmt.Sprintf("  %s  %s", u.Name, u.DisplayName))
			}
			return "", fmt.Errorf("%s", strings.Join(lines, "\n"))
		}
	}

	// Build preview.
	action := fmt.Sprintf("assign to %s (%s)", displayName, resolvedName)
	if resolvedName == "" {
		action = "unassign"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %d issue(s)\n\n", action, len(keys)))
	for _, key := range keys {
		sb.WriteString(fmt.Sprintf("  %s\n", key))
	}
	preview := sb.String()

	// Apply or return preview.
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

	// Execute sequentially, collect errors.
	var bodyMap any
	if resolvedName == "" {
		bodyMap = map[string]any{"name": nil}
	} else {
		bodyMap = map[string]string{"name": resolvedName}
	}
	var applyErrs []string
	var okKeys []string
	for _, key := range keys {
		p := jira.Preview{
			Method: "PUT",
			Path:   "/issue/" + key + "/assignee",
			Body:   bodyMap,
		}
		if _, err := p.Execute(ctx, client); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("  %s: %v", key, err))
		} else {
			okKeys = append(okKeys, key)
		}
	}

	var out strings.Builder
	out.WriteString(preview + "\n")
	for _, key := range okKeys {
		if resolvedName == "" {
			out.WriteString(fmt.Sprintf("✓ unassigned %s\n", key))
		} else {
			out.WriteString(fmt.Sprintf("✓ assigned %s → %s\n", key, displayName))
		}
	}
	if len(applyErrs) > 0 {
		out.WriteString("\nFailed:\n")
		for _, e := range applyErrs {
			out.WriteString(e + "\n")
		}
		return out.String(), fmt.Errorf("%d of %d assignments failed", len(applyErrs), len(keys))
	}
	return out.String(), nil
}
