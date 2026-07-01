package cmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// editSprintIssueKeyRE matches a canonical Jira issue key.
var editSprintIssueKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)

// EditSprintFlags holds flag values for `edit sprint`.
type EditSprintFlags struct {
	Profile string
	Yes     bool
	Board   int // required for target "current" | "next"
}

// NewEditSprintCmd builds "edit sprint <KEY...> <target>".
func NewEditSprintCmd() *cobra.Command {
	var flags EditSprintFlags
	c := &cobra.Command{
		Use:   "sprint <KEY> [KEY...] <target>",
		Short: "Move one or more issues into a sprint or the backlog (dry-run by default; --yes to apply)",
		Long: `Move one or more issues into a sprint or the backlog.

The last positional argument is the target. Earlier positionals are issue keys.

Targets:
  <numeric-id>   move into the sprint with that id
  current        active sprint on --board <id>       (--board required)
  next           first future sprint on --board <id> (--board required)
  backlog        remove from any sprint (Agile backlog endpoint)

Closed sprints are rejected. Kanban boards cannot resolve "current"/"next".`,
		Example: `  jiracli edit sprint ACME-123 28037
  jiracli edit sprint ACME-1 ACME-2 ACME-3 current --board 1234 --yes
  jiracli edit sprint ACME-123 backlog --yes`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := args[:len(args)-1]
			target := args[len(args)-1]
			result, err := EditSprint(cmd.Context(), flags, keys, target)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	c.Flags().IntVar(&flags.Board, "board", 0, "Scrum board id (required for target=current|next)")
	return c
}

// EditSprint is the Layer 1 implementation.
func EditSprint(ctx context.Context, flags EditSprintFlags, keys []string, target string) (string, error) {
	// Validate keys.
	for _, k := range keys {
		if !editSprintIssueKeyRE.MatchString(k) {
			return "", fmt.Errorf("not a valid issue key: %q", k)
		}
	}

	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	// Resolve target.
	useBacklog := false
	var sprintID int
	var sprint jira.Sprint

	switch strings.ToLower(target) {
	case "backlog":
		useBacklog = true

	case "current":
		if flags.Board == 0 {
			return "", fmt.Errorf("--board required for target %q", target)
		}
		spr, err := resolveActiveSprint(ctx, client, store, flags.Board)
		if err != nil {
			return "", err
		}
		sprint = spr
		sprintID = spr.ID

	case "next":
		if flags.Board == 0 {
			return "", fmt.Errorf("--board required for target %q", target)
		}
		spr, err := resolveNextSprint(ctx, client, store, flags.Board)
		if err != nil {
			return "", err
		}
		sprint = spr
		sprintID = spr.ID

	default:
		sid, convErr := strconv.Atoi(target)
		if convErr != nil {
			return "", fmt.Errorf("invalid target %q: must be a numeric sprint id, \"current\", \"next\", or \"backlog\"", target)
		}
		spr, err := client.GetSprint(ctx, sid)
		if err != nil {
			return "", fmt.Errorf("resolving sprint %d: %w", sid, err)
		}
		if strings.EqualFold(spr.State, "closed") {
			return "", fmt.Errorf("cannot move issues into closed sprint %d (%s)", sid, spr.Name)
		}
		sprint = spr
		sprintID = sid
	}

	// Build dry-run preview.
	preview := buildSprintPreview(entry, keys, useBacklog, sprintID, sprint)

	// Determine whether to apply.
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

	// Execute.
	if useBacklog {
		if err := client.MoveIssuesToBacklog(ctx, keys); err != nil {
			return preview + "\n", fmt.Errorf("move to backlog: %w", err)
		}
		// Invalidate sprint caches.
		invalidateSprintCaches(store, keys)
		var sb strings.Builder
		sb.WriteString(preview + "\n")
		fmt.Fprintf(&sb, "✓ moved %d issue(s) to backlog\n", len(keys))
		for i, k := range keys {
			if i >= 5 {
				fmt.Fprintf(&sb, "  … and %d more\n", len(keys)-5)
				break
			}
			fmt.Fprintf(&sb, "  → jiracli show %s\n", k)
		}
		return sb.String(), nil
	}

	if err := client.MoveIssuesToSprint(ctx, sprintID, keys); err != nil {
		return preview + "\n", fmt.Errorf("move to sprint: %w", err)
	}
	invalidateSprintCaches(store, keys)
	var sb strings.Builder
	sb.WriteString(preview + "\n")
	fmt.Fprintf(&sb, "✓ moved %d issue(s) into sprint %d (%s)\n", len(keys), sprintID, sprint.Name)
	for i, k := range keys {
		if i >= 5 {
			fmt.Fprintf(&sb, "  … and %d more\n", len(keys)-5)
			break
		}
		fmt.Fprintf(&sb, "  → jiracli show %s\n", k)
	}
	return sb.String(), nil
}

// buildSprintPreview builds the dry-run block.
func buildSprintPreview(entry keychain.Entry, keys []string, useBacklog bool, sprintID int, sprint jira.Sprint) string {
	var sb strings.Builder
	sb.WriteString("DRY RUN — no changes made.\n\n")

	if useBacklog {
		fmt.Fprintf(&sb, "POST %s/rest/agile/1.0/backlog/issue\n\n", entry.URL)
	} else {
		fmt.Fprintf(&sb, "POST %s/rest/agile/1.0/sprint/%d/issue\n\n", entry.URL, sprintID)
	}

	sb.WriteString("Body:\n  {\n    \"issues\": [")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q", k)
	}
	sb.WriteString("]\n  }\n\n")

	sb.WriteString("Effect:\n  ")
	if useBacklog {
		fmt.Fprintf(&sb, "move %d issue(s) to backlog\n\n", len(keys))
	} else {
		fmt.Fprintf(&sb, "move %d issue(s) into sprint %d %q (%s)\n\n", len(keys), sprintID, sprint.Name, sprint.State)
	}

	sb.WriteString("Validation:\n")
	fmt.Fprintf(&sb, "  ✓ %d key(s) parsed\n", len(keys))
	if !useBacklog {
		fmt.Fprintf(&sb, "  ✓ sprint %d is %s\n", sprintID, sprint.State)
		// Late-add warning.
		if strings.EqualFold(sprint.State, "active") && sprint.StartDate != "" {
			if start, err := time.Parse("2006-01-02T15:04:05.999Z07:00", sprint.StartDate); err == nil {
				days := int(time.Since(start).Hours() / 24)
				if days > 0 {
					fmt.Fprintf(&sb, "  ⚠ sprint started %dd ago — late-add\n", days)
				}
			}
		}
	}

	return sb.String()
}

// resolveActiveSprint returns the best current active sprint for a board.
// Reuses the active+future cached result and delegates to pickCurrentSprint.
func resolveActiveSprint(ctx context.Context, client *jira.Client, store *cache.Store, boardID int) (jira.Sprint, error) {
	sprints, _, err := client.ListSprintsCached(ctx, boardID, []string{"active", "future"}, 1, 50, store, false)
	if err != nil {
		if errors.Is(err, jira.ErrBoardNoSprints) {
			return jira.Sprint{}, fmt.Errorf("board %d is kanban and does not support sprints", boardID)
		}
		return jira.Sprint{}, err
	}
	spr, notes, err := pickCurrentSprint(sprints, boardID)
	if err != nil {
		return jira.Sprint{}, err
	}
	// Surface notes as a warning prefix on the sprint name — callers show sprint in a dry-run preview.
	// We can't print here (no writer), so attach them to the sprint name for now.
	_ = notes // notes are informational; edit sprint shows a preview anyway
	return spr, nil
}

// resolveNextSprint returns the first future sprint for a board.
func resolveNextSprint(ctx context.Context, client *jira.Client, store *cache.Store, boardID int) (jira.Sprint, error) {
	sprints, _, err := client.ListSprintsCached(ctx, boardID, []string{"future"}, 1, 50, store, false)
	if err != nil {
		if errors.Is(err, jira.ErrBoardNoSprints) {
			return jira.Sprint{}, fmt.Errorf("board %d is kanban and does not support sprints", boardID)
		}
		return jira.Sprint{}, err
	}
	if len(sprints) == 0 {
		return jira.Sprint{}, fmt.Errorf("no future sprints for board %d", boardID)
	}
	return sprints[0], nil
}

// invalidateSprintCaches purges sprint-related cache entries after a mutation.
// The archive of immutable closed sprints (sprints/<board>/archive) is left
// intact: moving an issue never changes a long-closed sprint's metadata, and
// rebuilding the archive is expensive.
func invalidateSprintCaches(store *cache.Store, keys []string) {
	if store == nil {
		return
	}
	// filepath.Match's "*" does not span "/", so each live cache shape is
	// listed explicitly. "sprints/*" alone would match nothing.
	for _, glob := range []string{
		"sprints/*/active+future",
		"sprints/*/closed",
		"sprints/*/closed-all",
		"sprints/*/names",
		"sprints/*/default-*",
	} {
		_ = store.DeleteGlob(glob)
	}
	for _, k := range keys {
		_ = store.Delete("issue-summary/" + k)
	}
}
