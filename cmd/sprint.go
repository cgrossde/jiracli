package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
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
	Profile      string
	JSON         bool
	Board        int
	State        string // csv: active,future,closed,all — empty = default recency view
	All          bool   // --all: show every sprint (all states, full history); disables the recency window
	ClosedWithin int    // --closed-within N: include closed sprints ending within N days (default view only)
	Limit        int
	Page         int
	NameContains string // --name-contains: case-insensitive substring filter on sprint Name
	After        string // --after YYYY-MM-DD: keep sprints whose endDate >= this date
	Before       string // --before YYYY-MM-DD: keep sprints whose startDate <= this date
	Sort         string // --sort: "asc" or "desc"; defaults to "desc" when --state is exactly "closed"
	NoCache      bool   // --no-cache: bypass local cache
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
	Sprint      int // override: use this sprint id instead of auto-resolving
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
		Long: `List sprints for a scrum board.

By default this shows only the sprints that matter right now: every active and
future sprint, plus closed sprints that ended within the last 7 days (tune with
--closed-within). This keeps the command fast on boards with years of history.
Use --all to fetch every sprint (all states, full history).

Sprints closed longer than 90 days ago are immutable and cached (near-)permanently,
so repeat runs never refetch them.

Examples:
  jiracli sprint list --board 1234
  jiracli sprint list --board 1234 --all
  jiracli sprint list --board 1234 --closed-within 30
  jiracli sprint list --board 1234 --state closed --all --json`,
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
	c.Flags().StringVar(&flags.State, "state", "", "Comma-separated states: active, future, closed, all. Empty = default view (active, future, and sprints closed within --closed-within days)")
	c.Flags().BoolVar(&flags.All, "all", false, "Show every sprint (all states, full history); disables the recency window")
	c.Flags().IntVar(&flags.ClosedWithin, "closed-within", 7, "Include closed sprints whose endDate is within this many days (default view only; ignored with --all, --state, or --after/--before)")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max results per page")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	c.Flags().StringVar(&flags.NameContains, "name-contains", "", "Case-insensitive substring filter on sprint name (client-side; fetches all sprints for the board)")
	c.Flags().StringVar(&flags.After, "after", "", "Keep sprints whose endDate is on/after this YYYY-MM-DD date (fetches full history)")
	c.Flags().StringVar(&flags.Before, "before", "", "Keep sprints whose startDate is on/before this YYYY-MM-DD date (fetches full history)")
	c.Flags().StringVar(&flags.Sort, "sort", "", "Sort by start date: 'asc' or 'desc'. Defaults to 'desc' when --state is exactly 'closed', else 'asc'.")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Bypass local cache")
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
	closedWithin := flags.ClosedWithin
	if closedWithin <= 0 {
		closedWithin = 7
	}

	// Validate date flags early — no I/O needed.
	var afterDate, beforeDate time.Time
	if flags.After != "" {
		afterDate, err = time.Parse("2006-01-02", flags.After)
		if err != nil {
			return "", fmt.Errorf("--after must be YYYY-MM-DD — got %q", flags.After)
		}
	}
	if flags.Before != "" {
		beforeDate, err = time.Parse("2006-01-02", flags.Before)
		if err != nil {
			return "", fmt.Errorf("--before must be YYYY-MM-DD — got %q", flags.Before)
		}
	}

	// Determine effective sort direction.
	// Default: "desc" when --state is exactly "closed", else "asc".
	sortDir := flags.Sort
	if sortDir == "" {
		if strings.EqualFold(strings.TrimSpace(flags.State), "closed") {
			sortDir = "desc"
		} else {
			sortDir = "asc"
		}
	}

	kanbanErr := func() error {
		return fmt.Errorf("board %d is kanban and does not support sprints — use: jiracli board issues %d", flags.Board, flags.Board)
	}

	// Explicitly check board type up front. The default view's fast path
	// (ListSprintNames, the legacy GreenHopper sprintquery endpoint) does not
	// reliably reject kanban boards the way the modern agile/1.0 sprint
	// endpoint does — it can return leftover/stale sprint records for a board
	// that is now kanban. A cheap, cached board-config lookup catches this
	// consistently on every path, matching `sprint current`'s behavior.
	if cfg, cfgErr := client.GetBoardConfigCached(ctx, flags.Board, store, flags.NoCache); cfgErr == nil && strings.EqualFold(cfg.Type, "kanban") {
		return "", kanbanErr()
	}

	hasDateFilter := flags.After != "" || flags.Before != ""
	// The recency window drives the default view (active + future + closed
	// within --closed-within days). It is disabled by --all, by a date-range
	// filter (which defines its own horizon), or by ANY explicit --state —
	// otherwise e.g. "--state closed" would silently be restricted to the
	// closed-within window instead of returning the board's full closed
	// history, which is misleading (it can report "no sprints found" on
	// boards whose closed sprints are all older than the window).
	windowDisabled := flags.All || hasDateFilter || strings.TrimSpace(flags.State) != ""

	// Fetch the working set of sprints.
	var allSprints []jira.Sprint
	switch {
	case hasDateFilter:
		// Date-range filters need every sprint's dates. Use the complete
		// GreenHopper set with archive-backed hydration — this also fixes the
		// historical under-reporting of the paged closed endpoint.
		allSprints, err = client.ListAllSprintsHydrated(ctx, flags.Board, store, flags.NoCache)
		if err != nil {
			if errors.Is(err, jira.ErrBoardNoSprints) {
				return "", kanbanErr()
			}
			return "", err
		}
	case windowDisabled:
		// --all / --state all (no date filter): full history, all states. Dates
		// are hydrated for the displayed page only (below), keeping this cheap.
		sprints, ghErr := client.ListSprintNames(ctx, flags.Board, store, flags.NoCache)
		if ghErr != nil {
			slog.Warn("sprintquery unavailable, falling back to paged listing", "board", flags.Board, "err", ghErr)
			sprints, ghErr = client.ListAllSprintsPaged(ctx, flags.Board, nil, store, flags.NoCache)
			if ghErr != nil {
				if errors.Is(ghErr, jira.ErrBoardNoSprints) {
					return "", kanbanErr()
				}
				return "", ghErr
			}
		}
		allSprints = sprints
	default:
		// Default view: active + future + closed-within-window, hydrated cheaply
		// via the early-stop scan and archive cache.
		allSprints, err = client.ListSprintsDefaultView(ctx, flags.Board, time.Duration(closedWithin)*24*time.Hour, store, flags.NoCache)
		if err != nil {
			if errors.Is(err, jira.ErrBoardNoSprints) {
				return "", kanbanErr()
			}
			return "", err
		}
	}

	// Apply state / name / date filters.
	filtered := make([]jira.Sprint, 0, len(allSprints))
	for _, s := range allSprints {
		if len(states) > 0 {
			matched := false
			for _, st := range states {
				if strings.EqualFold(s.State, st) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Apply --name-contains filter.
		if flags.NameContains != "" && !strings.Contains(strings.ToLower(s.Name), strings.ToLower(flags.NameContains)) {
			continue
		}
		// Apply --after filter (endDate >= afterDate).
		if !afterDate.IsZero() {
			if s.EndDate == "" {
				// empty endDate treated as match (ongoing/upcoming sprint)
			} else {
				end := parseDateOnly(s.EndDate)
				if !end.IsZero() && end.Before(afterDate) {
					continue
				}
			}
		}
		// Apply --before filter (startDate <= beforeDate).
		if !beforeDate.IsZero() {
			if s.StartDate == "" {
				// empty startDate: cannot prove it falls before cutoff, skip
				continue
			}
			start := parseDateOnly(s.StartDate)
			if !start.IsZero() && start.After(beforeDate) {
				continue
			}
		}
		filtered = append(filtered, s)
	}

	// Sort by StartDate; fall back to ID when StartDate is absent.
	sort.SliceStable(filtered, func(i, j int) bool {
		si := parseDateOnly(filtered[i].StartDate)
		sj := parseDateOnly(filtered[j].StartDate)
		if si.IsZero() && sj.IsZero() {
			// both missing dates: sort by ID
			if sortDir == "desc" {
				return filtered[i].ID > filtered[j].ID
			}
			return filtered[i].ID < filtered[j].ID
		}
		if si.IsZero() {
			return sortDir != "desc" // no date → sort after dated entries in asc, before in desc
		}
		if sj.IsZero() {
			return sortDir == "desc"
		}
		if sortDir == "desc" {
			return si.After(sj)
		}
		return si.Before(sj)
	})

	// Paginate the filtered result.
	total := len(filtered)
	start := (page - 1) * limit
	if start >= total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page1Sprints := filtered[start:end]
	isLast := end >= total

	// The GreenHopper names endpoint omits dates. Hydrate them for just the
	// displayed page (bounded-parallel, ~one round-trip per shown sprint) so the
	// date column is populated without paging the whole history. On the paged
	// date path the sprints already carry dates, so this is a no-op.
	page1Sprints = client.HydrateSprintDates(ctx, page1Sprints)

	return renderSprintList(flags, page1Sprints, isLast, page, limit, total), nil
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
		// GetSprint's error already carries "get sprint: ..." context.
		return "", err
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
	sf := SearchFlags{Limit: limit, Page: page, PageCmdBase: fmt.Sprintf("jiracli sprint issues %d", sprintID)}
	return renderSearchPlain(resp, header, "", page, limit, sf, fields, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
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

	// Embed issues (default limit 25).
	issueLimit := 25
	fields := defaultSearchFields
	resp, issErr := client.ListSprintIssues(ctx, spr.ID, 1, issueLimit, fields)

	// Apply client-side filters once, so plain text and --json return the same set.
	if issErr == nil {
		resp.Issues = filterCurrentSprintIssues(ctx, client, entry, flags, resp.Issues)
	}

	if flags.JSON {
		records := make([]jira.SearchIssueRecord, 0, len(resp.Issues))
		for _, raw := range resp.Issues {
			records = append(records, jira.ToSearchRecord(raw, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField))
		}
		if issErr != nil {
			notes = append(notes, fmt.Sprintf("sprint issues unavailable: %v", issErr))
		}
		// Single composite object: "the current sprint and its issues" is one
		// logical record, so it is emitted as one self-describing JSON object
		// rather than a heterogeneous stream of sprint + issue lines.
		out := struct {
			Sprint   jira.Sprint              `json:"sprint"`
			Issues   []jira.SearchIssueRecord `json:"issues"`
			Returned int                      `json:"returned"`
			Total    int                      `json:"total"`
			Notes    []string                 `json:"notes,omitempty"`
		}{
			Sprint:   spr,
			Issues:   records,
			Returned: len(records),
			Total:    resp.Total,
			Notes:    notes,
		}
		data, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("marshal sprint current: %w", err)
		}
		return string(data) + "\n", nil
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

	if issErr != nil {
		fmt.Fprintf(&sb, "[sprint issues unavailable: %v]\n", issErr)
		return sb.String(), nil
	}

	header := fmt.Sprintf("sprint: %d  %s", spr.ID, spr.Name)
	sf := SearchFlags{Limit: issueLimit, Page: 1, PageCmdBase: fmt.Sprintf("jiracli sprint issues %d", spr.ID)}
	issueText, _ := renderSearchPlain(resp, header, "", 1, issueLimit, sf, fields, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
	sb.WriteString(issueText)
	if resp.Total > issueLimit {
		fmt.Fprintf(&sb, "→ jiracli sprint issues %d --limit 100\n", spr.ID)
	}
	return sb.String(), nil
}

// filterCurrentSprintIssues applies the --assigned and --exclude-done client-side
// filters to a sprint's issue list. Both output modes call it so plain text and
// --json always return the same set.
func filterCurrentSprintIssues(ctx context.Context, client *jira.Client, entry keychain.Entry, flags sprintCurrentFlags, issues []jira.IssueRaw) []jira.IssueRaw {
	if flags.Assigned {
		// Resolve "me" without an extra round-trip when the profile knows it.
		me, meDisplay := entry.User, entry.DisplayName
		if me == "" && meDisplay == "" {
			if u, err := client.Myself(ctx); err == nil {
				me, meDisplay = u.Name, u.DisplayName
			}
		}
		filtered := make([]jira.IssueRaw, 0, len(issues))
		for _, issue := range issues {
			if issue.Fields.Assignee == nil {
				continue
			}
			if (me != "" && strings.EqualFold(issue.Fields.Assignee.Name, me)) ||
				(meDisplay != "" && strings.EqualFold(issue.Fields.Assignee.DisplayName, meDisplay)) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}
	if flags.ExcludeDone {
		filtered := make([]jira.IssueRaw, 0, len(issues))
		for _, issue := range issues {
			if !strings.EqualFold(issue.Fields.Status.StatusCategory.Key, "done") {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}
	return issues
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

// renderSprintList renders a list of sprints as plain text or NDJSON.
// total is the count of all sprints matching the query when known (filtered
// path), or -1 when unknown (cached fast path). When total >= 0 the JSON
// pagination trailer carries real page/total figures; otherwise it falls back
// to a has_more-only trailer.
func renderSprintList(flags sprintListFlags, sprints []jira.Sprint, isLast bool, page, limit, total int) string {
	if flags.JSON {
		var sb strings.Builder
		for _, s := range sprints {
			data, _ := json.Marshal(s)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		if !isLast {
			if total >= 0 {
				fmt.Fprintf(&sb, "{\"_pagination\":{\"page\":%d,\"pages\":%d,\"total\":%d,\"next_page\":%d,\"has_more\":true}}\n",
					page, totalPages(total, limit), total, page+1)
			} else {
				fmt.Fprintf(&sb, "{\"_pagination\":{\"page\":%d,\"next_page\":%d,\"has_more\":true}}\n",
					page, page+1)
			}
		}
		return sb.String()
	}
	var sb strings.Builder
	if len(sprints) == 0 {
		sb.WriteString("no sprints found\n")
		return sb.String()
	}
	fmt.Fprintf(&sb, "Board %d — use: jiracli sprint issues <id>\n\n", flags.Board)
	for _, s := range sprints {
		dates := sprintDateRange(s)
		fmt.Fprintf(&sb, "  %-8d  %-8s  %-40s  %s\n", s.ID, s.State, s.Name, dates)
	}
	if !isLast {
		next := buildSprintListNextCmd(flags, page+1, limit)
		fmt.Fprintf(&sb, "\n--- %s ---\n", next)
	} else {
		fmt.Fprintf(&sb, "\n→ jiracli sprint issues <id>\n")
	}
	return sb.String()
}

// buildSprintListNextCmd reconstructs the jiracli sprint list command for the next page,
// including any non-default flags.
func buildSprintListNextCmd(flags sprintListFlags, nextPage, limit int) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("jiracli sprint list --board %d --page %d --limit %d", flags.Board, nextPage, limit))
	if flags.All {
		parts = append(parts, "--all")
	}
	if flags.State != "" {
		parts = append(parts, fmt.Sprintf("--state %s", flags.State))
	}
	if flags.ClosedWithin != 0 && flags.ClosedWithin != 7 {
		parts = append(parts, fmt.Sprintf("--closed-within %d", flags.ClosedWithin))
	}
	if flags.NameContains != "" {
		parts = append(parts, fmt.Sprintf("--name-contains %q", flags.NameContains))
	}
	if flags.After != "" {
		parts = append(parts, fmt.Sprintf("--after %s", flags.After))
	}
	if flags.Before != "" {
		parts = append(parts, fmt.Sprintf("--before %s", flags.Before))
	}
	if flags.Sort != "" {
		parts = append(parts, fmt.Sprintf("--sort %s", flags.Sort))
	}
	if flags.NoCache {
		parts = append(parts, "--no-cache")
	}
	return "next: " + strings.Join(parts, " ")
}

// parseDateOnly parses a Jira date/datetime string to a time.Time at midnight UTC.
// Returns zero time if parsing fails or s is empty.
func parseDateOnly(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Truncate(24 * time.Hour)
		}
	}
	return time.Time{}
}
