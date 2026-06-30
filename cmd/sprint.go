package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// NewSprintCmd builds the "sprint" command group.
func NewSprintCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sprint",
		Short: "Inspect Jira Agile sprints (scrum boards only)",
		Long: `Sprints belong to Scrum boards. Use "list --board <id>" to find sprint IDs
and "issues" / "show" to inspect them. For Kanban work, see: jiracli board`,
	}
	c.AddCommand(NewSprintListCmd(), NewSprintShowCmd(), NewSprintIssuesCmd(), NewSprintCurrentCmd())
	return c
}

// ---------------------------------------------------------------------------
// Flag types
// ---------------------------------------------------------------------------

type sprintListFlags struct {
	Profile string
	JSON    bool
	Board   int
	State   string // csv: active,future,closed,all — default "active,future"
	Limit   int
	Page    int
}

type sprintShowFlags struct {
	Profile string
	JSON    bool
}

type sprintIssuesFlags struct {
	Profile  string
	JSON     bool
	KeysOnly bool
	Limit    int
	Page     int
}

type sprintCurrentFlags struct {
	Profile     string
	JSON        bool
	Board       int
	Sprint      int  // override: use this sprint id instead of auto-resolving
	Assigned    bool
	ExcludeDone bool
}

// ---------------------------------------------------------------------------
// Command constructors
// ---------------------------------------------------------------------------

func NewSprintListCmd() *cobra.Command {
	var flags sprintListFlags
	c := &cobra.Command{
		Use:   "list",
		Short: "List sprints for a scrum board",
		Long: `List sprints for a scrum board. Defaults to active and future sprints.

Examples:
  jiracli sprint list --board 1234
  jiracli sprint list --board 1234 --state closed
  jiracli sprint list --board 1234 --state all --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.Board == 0 {
				return fmt.Errorf("--board required")
			}
			result, err := sprintList(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().IntVar(&flags.Board, "board", 0, "Scrum board ID (required)")
	c.Flags().StringVar(&flags.State, "state", "active,future", "Comma-separated states: active, future, closed, all")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max results per page")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

func NewSprintShowCmd() *cobra.Command {
	var flags sprintShowFlags
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Show sprint details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := sprintShow(cmd.Context(), flags, args[0])
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

func NewSprintIssuesCmd() *cobra.Command {
	var flags sprintIssuesFlags
	c := &cobra.Command{
		Use:   "issues <id>",
		Short: "List issues in a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := sprintIssues(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.KeysOnly, "keys-only", false, "Print one key per line")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max results per page")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

func NewSprintCurrentCmd() *cobra.Command {
	var flags sprintCurrentFlags
	c := &cobra.Command{
		Use:   "current",
		Short: "Show the active sprint and its issues",
		Long: `Show the active sprint for a scrum board, with embedded issue list.
Supports --assigned and --exclude-done to filter the issue list.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.Board == 0 {
				return fmt.Errorf("--board required")
			}
			result, err := sprintCurrent(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().IntVar(&flags.Board, "board", 0, "Scrum board ID (required)")
	c.Flags().IntVar(&flags.Sprint, "sprint", 0, "Sprint ID override (skip auto-resolution)")
	c.Flags().BoolVar(&flags.Assigned, "assigned", false, "Show only issues assigned to me (client-side filter)")
	c.Flags().BoolVar(&flags.ExcludeDone, "exclude-done", false, "Exclude issues in the Done status category")
	return c
}

// ---------------------------------------------------------------------------
// Implementations
// ---------------------------------------------------------------------------

func sprintList(ctx context.Context, flags sprintListFlags) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	states := parseSprintStates(flags.State)
	limit := flags.Limit
	if limit <= 0 {
		limit = 50
	}
	page := flags.Page
	if page < 1 {
		page = 1
	}

	sprints, isLast, err := client.ListSprintsCached(ctx, flags.Board, states, page, limit, store, false)
	if err != nil {
		if errors.Is(err, jira.ErrBoardNoSprints) {
			return "", fmt.Errorf("board %d is kanban and does not support sprints — use: jiracli board issues %d", flags.Board, flags.Board)
		}
		return "", err
	}

	if flags.JSON {
		var sb strings.Builder
		for _, s := range sprints {
			data, _ := json.Marshal(s)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		if !isLast {
			fmt.Fprintf(&sb, "{\"_pagination\":{\"page\":%d,\"pages\":-1,\"total\":-1,\"next_page\":%d,\"has_more\":true}}\n",
				page, page+1)
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	if len(sprints) == 0 {
		sb.WriteString("no sprints found\n")
		return sb.String(), nil
	}
	fmt.Fprintf(&sb, "Board %d — use: jiracli sprint issues <id>\n\n", flags.Board)
	for _, s := range sprints {
		dates := sprintDateRange(s)
		fmt.Fprintf(&sb, "  %-8d  %-8s  %-40s  %s\n", s.ID, s.State, s.Name, dates)
	}
	if !isLast {
		fmt.Fprintf(&sb, "\n--- next: jiracli sprint list --board %d --page %d --limit %d ---\n",
			flags.Board, page+1, limit)
	} else {
		fmt.Fprintf(&sb, "\n→ jiracli sprint issues <id>\n")
	}
	return sb.String(), nil
}

func sprintShow(ctx context.Context, flags sprintShowFlags, idStr string) (string, error) {
	sprintID, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("sprint id must be numeric, got %q", idStr)
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	spr, err := client.GetSprint(ctx, sprintID)
	if err != nil {
		return "", fmt.Errorf("get sprint: %w", err)
	}

	if flags.JSON {
		data, err := json.Marshal(spr)
		if err != nil {
			return "", fmt.Errorf("marshal sprint: %w", err)
		}
		return string(data) + "\n", nil
	}

	return renderSprintDetail(spr), nil
}

func sprintIssues(ctx context.Context, flags sprintIssuesFlags, idStr string) (string, error) {
	sprintID, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("sprint id must be numeric, got %q", idStr)
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	limit := flags.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	page := flags.Page
	if page < 1 {
		page = 1
	}

	sprintName := fmt.Sprintf("%d", sprintID)
	if spr, sErr := client.GetSprint(ctx, sprintID); sErr == nil {
		sprintName = spr.Name
	}

	fields := defaultSearchFields
	resp, err := client.ListSprintIssues(ctx, sprintID, page, limit, fields)
	if err != nil {
		return "", fmt.Errorf("sprint issues: %w", err)
	}

	if flags.KeysOnly {
		var sb strings.Builder
		for _, issue := range resp.Issues {
			fmt.Fprintln(&sb, issue.Key)
		}
		return sb.String(), nil
	}

	if flags.JSON {
		return renderSearchJSON(resp, page, limit, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
	}

	header := fmt.Sprintf("sprint: %d  %s", sprintID, sprintName)
	sf := SearchFlags{Limit: limit, Page: page}
	return renderSearchPlain(resp, header, fmt.Sprintf("jiracli sprint issues %d", sprintID), page, limit, sf, fields, entry.Hierarchy.StoryPointsField)
}

func sprintCurrent(ctx context.Context, flags sprintCurrentFlags) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	var spr jira.Sprint
	var notes []string // informational lines printed before the sprint detail

	if flags.Sprint != 0 {
		// Explicit override — fetch directly, no resolution needed.
		spr, err = client.GetSprint(ctx, flags.Sprint)
		if err != nil {
			return "", fmt.Errorf("sprint %d: %w", flags.Sprint, err)
		}
	} else {
		// Reuse the active+future cached result; filter to active client-side.
		allSprints, _, err := client.ListSprintsCached(ctx, flags.Board, []string{"active", "future"}, 1, 50, store, false)
		if err != nil {
			if errors.Is(err, jira.ErrBoardNoSprints) {
				return "", fmt.Errorf("board %d is kanban and does not support sprints — use: jiracli board issues %d", flags.Board, flags.Board)
			}
			return "", err
		}
		spr, notes, err = pickCurrentSprint(allSprints, flags.Board)
		if err != nil {
			return "", err
		}
	}

	var sb strings.Builder
	for _, n := range notes {
		fmt.Fprintf(&sb, "note: %s\n", n)
	}
	if len(notes) > 0 {
		sb.WriteByte('\n')
	}
	sb.WriteString(renderSprintDetail(spr))
	sb.WriteByte('\n')

	// Embed issues (default limit 25).
	issueLimit := 25
	fields := defaultSearchFields
	resp, issErr := client.ListSprintIssues(ctx, spr.ID, 1, issueLimit, fields)
	if issErr != nil {
		fmt.Fprintf(&sb, "[sprint issues unavailable: %v]\n", issErr)
		return sb.String(), nil
	}

	if flags.JSON {
		sprintData, _ := json.Marshal(spr)
		issueJSON, jsonErr := renderSearchJSON(resp, 1, issueLimit, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
		if jsonErr != nil {
			return "", jsonErr
		}
		return string(sprintData) + "\n" + issueJSON, nil
	}

	// Client-side filters.
	if flags.ExcludeDone {
		var filtered []jira.IssueRaw
		for _, issue := range resp.Issues {
			if !strings.EqualFold(issue.Fields.Status.StatusCategory.Key, "done") {
				filtered = append(filtered, issue)
			}
		}
		resp.Issues = filtered
	}

	header := fmt.Sprintf("sprint: %d  %s", spr.ID, spr.Name)
	sf := SearchFlags{Limit: issueLimit, Page: 1}
	issueText, _ := renderSearchPlain(resp, header, fmt.Sprintf("jiracli sprint issues %d", spr.ID), 1, issueLimit, sf, fields, entry.Hierarchy.StoryPointsField)
	sb.WriteString(issueText)
	if resp.Total > issueLimit {
		fmt.Fprintf(&sb, "→ jiracli sprint issues %d --limit 100\n", spr.ID)
	}
	return sb.String(), nil
}

// pickCurrentSprint selects the best "current" sprint from a mixed active+future list.
//
// Algorithm:
//  1. Keep only state=="active" sprints.
//  2. Split into stale (endDate more than 30 days in the past) vs current.
//  3. If no current candidates remain → error listing stale ones with remediation hint.
//  4. If exactly one current candidate → return it, note stale count if any.
//  5. If multiple → pick the one with the latest startDate, add an ambiguity note.
//
// notes are informational lines for the caller to surface (not errors).
func pickCurrentSprint(sprints []jira.Sprint, boardID int) (jira.Sprint, []string, error) {
	const staleCutoff = -30 * 24 * time.Hour // endDate older than 30 days ago
	now := time.Now()

	var current, stale []jira.Sprint
	for _, s := range sprints {
		if !strings.EqualFold(s.State, "active") {
			continue
		}
		if s.EndDate != "" {
			if end, err := time.Parse("2006-01-02T15:04:05.999Z07:00", s.EndDate); err == nil {
				if end.Sub(now) < staleCutoff {
					stale = append(stale, s)
					continue
				}
			}
		}
		current = append(current, s)
	}

	if len(current) == 0 {
		if len(stale) > 0 {
			var sb strings.Builder
			fmt.Fprintf(&sb, "no current active sprint found for board %d\n", boardID)
			fmt.Fprintf(&sb, "%d sprint(s) appear active but ended >30 days ago (likely abandoned — close them in Jira):\n", len(stale))
			for _, s := range stale {
				fmt.Fprintf(&sb, "  %-8d  %s  (%s)\n", s.ID, s.Name, sprintDateRange(s))
			}
			fmt.Fprintf(&sb, "use: jiracli sprint list --board %d --state future\n", boardID)
			return jira.Sprint{}, nil, fmt.Errorf("%s", sb.String())
		}
		return jira.Sprint{}, nil, fmt.Errorf("no active sprint for board %d — list options with: jiracli sprint list --board %d --state future",
			boardID, boardID)
	}

	var notes []string
	if len(stale) > 0 {
		note := fmt.Sprintf("%d stale active sprint(s) skipped (ended >30 days ago)", len(stale))
		for _, s := range stale {
			note += fmt.Sprintf(" [%d: %s]", s.ID, s.Name)
		}
		notes = append(notes, note)
	}

	if len(current) == 1 {
		return current[0], notes, nil
	}

	// Multiple genuinely active sprints — pick the most recently started.
	best := current[0]
	for _, s := range current[1:] {
		if s.StartDate > best.StartDate {
			best = s
		}
	}
	var others []string
	for _, s := range current {
		if s.ID != best.ID {
			others = append(others, fmt.Sprintf("%d (%s)", s.ID, s.Name))
		}
	}
	notes = append(notes, fmt.Sprintf(
		"multiple active sprints — using most recent: %d %q. Others: %s. Pass --sprint <id> to override.",
		best.ID, best.Name, strings.Join(others, ", "),
	))
	return best, notes, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// renderSprintDetail renders a Sprint as plain text.
func renderSprintDetail(spr jira.Sprint) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Sprint %d  %s  %s\n", spr.ID, spr.Name, spr.State)
	dates := sprintDateRange(spr)
	activated := ""
	if spr.ActivatedDate != "" {
		activated = fmt.Sprintf("  (activated %s)", formatSprintDate(spr.ActivatedDate))
	}
	fmt.Fprintf(&sb, "Dates:  %s%s\n", dates, activated)
	fmt.Fprintf(&sb, "Board:  %d\n", spr.OriginBoardID)
	goal := spr.Goal
	if goal == "" {
		goal = "(none)"
	}
	fmt.Fprintf(&sb, "Goal:   %s\n\n", goal)
	sb.WriteString("Drill in:\n")
	fmt.Fprintf(&sb, "  → jiracli sprint issues %d\n", spr.ID)
	return sb.String()
}

// parseSprintStates parses a comma-separated state string.
// "all" or "" → nil (omit the state query param, returns all states).
func parseSprintStates(state string) []string {
	if state == "" || strings.EqualFold(state, "all") {
		return nil
	}
	var out []string
	for _, s := range strings.Split(state, ",") {
		s = strings.TrimSpace(s)
		if strings.EqualFold(s, "all") {
			return nil
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// sprintDateRange formats start→end dates for display.
func sprintDateRange(s jira.Sprint) string {
	start := formatSprintDate(s.StartDate)
	end := formatSprintDate(s.EndDate)
	if start == "" && end == "" {
		return "(no dates)"
	}
	if start == "" {
		return "? → " + end
	}
	if end == "" {
		return start + " → ?"
	}
	return start + " → " + end
}

// formatSprintDate parses a Jira sprint date string and returns YYYY-MM-DD.
func formatSprintDate(s string) string {
	if s == "" {
		return ""
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s // raw fallback if parse fails
}
