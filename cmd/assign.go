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
		Use:   "assignee <KEY> <user-or-id>",
		Short: "Assign an issue to a user (dry-run by default; use --yes to apply)",
		Long: `Assign an issue to a user (dry-run by default; use --yes to apply).

See also:
  jiracli lookup users --project <KEY>    find a valid username`,
		Example: `  jiracli edit assignee ACME-123 me
  jiracli edit assignee ACME-123 -          # unassign
  jiracli edit assignee ACME-123 alex --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Assign(cmd.Context(), flags, args[0], args[1])
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
