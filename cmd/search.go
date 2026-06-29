package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// SearchFlags holds parsed flag values for the search command.
type SearchFlags struct {
	Profile     string
	JSON        bool
	Limit       int
	Page        int
	ExcludeDone bool
	Fields      string
	Assigned    bool
	Category    string
}

// defaultSearchFields is the default field set requested from the Jira API.
var defaultSearchFields = []string{
	"key", "status", "issuetype", "priority", "assignee",
	"updated", "summary", "labels", "components",
}

// updatedLayout is the Jira API date-time format for the updated field.
const updatedLayout = "2006-01-02T15:04:05.000-0700"

// NewSearchCmd builds the "search" command.
func NewSearchCmd() *cobra.Command {
	var flags SearchFlags
	c := &cobra.Command{
		Use:   "search <jql...>",
		Short: "Search Jira issues with JQL",
		Long: `Search Jira issues using JQL. Multiple arguments are joined with a space.

All issues are returned by default, including Done. Use --exclude-done to hide
issues in the "Done" status category (equivalent to adding
statusCategory != "Done" to your query).

Use --category to filter by status category (todo, in-progress, done, all).
Use --assigned to restrict results to the current user.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !flags.Assigned && len(args) == 0 {
				return fmt.Errorf("requires at least 1 arg (JQL), or use --assigned")
			}
			jql := strings.Join(args, " ")
			result, err := Search(cmd.Context(), flags, jql)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Maximum results per page (1-100)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	c.Flags().BoolVar(&flags.ExcludeDone, "exclude-done", false, "Exclude issues in the Done status category")
	c.Flags().StringVar(&flags.Fields, "fields", "", `Field adjustments: "+field" to add, "-field" to drop, or "a,b,c" to replace`)
	c.Flags().BoolVar(&flags.Assigned, "assigned", false, "Show only issues assigned to me")
	c.Flags().StringVar(&flags.Category, "category", "", "Status category filter: todo, in-progress, done, all")
	return c
}

// Search is the Layer 1 implementation for the search command.
func Search(ctx context.Context, flags SearchFlags, jql string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Build effective JQL.
	effectiveJQL := jql
	if flags.Assigned {
		// --assigned overrides any JQL args when none provided; otherwise prepends.
		if jql == "" {
			var err error
			effectiveJQL, err = buildAssignedJQL(flags.Category)
			if err != nil {
				return "", err
			}
		} else {
			catClause, err := categoryJQLClause(flags.Category)
			if err != nil {
				return "", err
			}
			effectiveJQL = `assignee = currentUser() AND (` + jql + `)`
			if catClause != "" {
				effectiveJQL += ` AND ` + catClause
			}
		}
	} else if flags.Category != "" {
		catClause, err := categoryJQLClause(flags.Category)
		if err != nil {
			return "", err
		}
		if jql == "" {
			effectiveJQL = catClause + ` ORDER BY updated DESC`
		} else {
			effectiveJQL = `(` + jql + `) AND ` + catClause
		}
	} else if flags.ExcludeDone {
		effectiveJQL = jira.DefaultOpenFilter(jql)
	}

	// Resolve fields.
	fields := resolveSearchFields(flags.Fields)

	// Clamp page.
	page := flags.Page
	if page < 1 {
		page = 1
	}
	limit := flags.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	resp, err := client.Search(ctx, effectiveJQL, page, limit, fields)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	if flags.JSON {
		return renderSearchJSON(resp, page, limit)
	}
	return renderSearchPlain(resp, effectiveJQL, jql, page, limit, flags)
}

// resolveSearchFields applies the --fields spec to the default search field list.
// Spec forms:
//   - empty → default set
//   - "a,b,c" (no +/-) → replace entirely
//   - "+a,+b,-c" → add/remove from default
func resolveSearchFields(spec string) []string {
	if spec == "" {
		return defaultSearchFields
	}
	tokens := strings.Split(spec, ",")
	// Check if any token starts with + or -
	hasModifier := false
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
			hasModifier = true
			break
		}
	}
	if !hasModifier {
		// Replacement mode.
		out := make([]string, 0, len(tokens))
		for _, t := range tokens {
			t = strings.TrimSpace(t)
			if t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	// Add/remove mode: start from defaults.
	set := make([]string, len(defaultSearchFields))
	copy(set, defaultSearchFields)
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, "+") {
			field := strings.TrimPrefix(t, "+")
			if field != "" && !containsStr(set, field) {
				set = append(set, field)
			}
		} else if strings.HasPrefix(t, "-") {
			field := strings.TrimPrefix(t, "-")
			set = removeStr(set, field)
		}
	}
	return set
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	out := ss[:0:0]
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// renderSearchJSON emits one NDJSON record per issue, then a pagination trailer
// if more pages exist.
func renderSearchJSON(resp jira.SearchResponse, page, limit int) (string, error) {
	var sb strings.Builder
	for _, raw := range resp.Issues {
		rec := jira.ToSearchRecord(raw)
		data, err := json.Marshal(rec)
		if err != nil {
			return "", fmt.Errorf("marshal search record: %w", err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}

	returned := len(resp.Issues)
	startAt := (page - 1) * limit
	hasMore := resp.Total > startAt+returned
	if hasMore {
		pages := totalPages(resp.Total, limit)
		var trailer jira.SearchPaginationTrailer
		trailer.Pagination.Page = page
		trailer.Pagination.Pages = pages
		trailer.Pagination.Total = resp.Total
		trailer.Pagination.NextPage = page + 1
		trailer.Pagination.HasMore = true
		data, err := json.Marshal(trailer)
		if err != nil {
			return "", fmt.Errorf("marshal pagination trailer: %w", err)
		}
		sb.Write(data)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}
// ANSI constants — only what the local helpers below still need directly.
// colorIssueType, colorStatusName, colorsEnabled, and stripAnsi have been
// promoted to internal/jira; local helpers call them via the jira package.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiFgW   = "\x1b[97m" // bright white foreground

	// Background colors (256-color) — muted to avoid overwhelming dark terminals.
	ansiBgToDo       = "\x1b[48;5;238m"
	ansiBgInProgress = "\x1b[48;5;24m"
	ansiBgDone       = "\x1b[48;5;22m"

	// Foreground-only.
	ansiFgOrange = "\x1b[38;5;208m"
	ansiFgRed    = "\x1b[31m"
	ansiFgGrey   = "\x1b[38;5;246m"
)

// colorsEnabled delegates to jira.ColorsEnabled.
func colorsEnabled() bool { return jira.ColorsEnabled() }

// stripAnsi delegates to jira.StripAnsi.
func stripAnsi(s string) string { return jira.StripAnsi(s) }

// colorIssueType delegates to jira.ColorIssueType.
func colorIssueType(name string) string { return jira.ColorIssueType(name, colorsEnabled()) }

// colorStatusName delegates to jira.ColorStatusName.
func colorStatusName(name string) string { return jira.ColorStatusName(name, colorsEnabled()) }

const titleWidth = 110

// titleLine composes the 110-char-wide title line:
//
//	[N] <typeBadge> KEY  summary <padding> statusBadge
//
// Summary is cropped so the visible line fits exactly in titleWidth columns.
func titleLine(n int, typeBadge, key, summary, statusBadge string) string {
	// Visible widths of the fixed pieces.
	statusVis := len(stripAnsi(statusBadge)) // badge adds " text " padding itself
	prefixPlain := fmt.Sprintf("[%d] %s %s  ", n, stripAnsi(typeBadge), key)
	prefixVis := len(prefixPlain)

	// Available visible columns for summary: total − prefix − 1 gap − status
	availSummary := titleWidth - prefixVis - 1 - statusVis
	if availSummary < 0 {
		availSummary = 0
	}

	// Crop summary to availSummary runes.
	runes := []rune(summary)
	if len(runes) > availSummary {
		if availSummary >= 1 {
			runes = runes[:availSummary-1]
			summary = string(runes) + "…"
		} else {
			summary = ""
		}
	}

	// Recompute actual visible width after crop.
	summaryVis := len([]rune(summary)) // rune count == visible width for BMP text
	usedVis := prefixVis + summaryVis
	padding := titleWidth - usedVis - statusVis
	if padding < 1 {
		padding = 1
	}

	if colorsEnabled() {
		return fmt.Sprintf("[%d] %s "+ansiBold+"%s"+ansiReset+ansiFgW+"  %s"+ansiReset+"%s%s\n",
			n, typeBadge, key, summary, strings.Repeat(" ", padding), statusBadge)
	}
	return fmt.Sprintf("[%d] %s %s  %s%s%s\n",
		n, typeBadge, key, summary, strings.Repeat(" ", padding), statusBadge)
}

// badge wraps text in a bold-white-on-bg pill: " text ".
func badge(bg, text string) string {
	return bg + ansiFgW + ansiBold + " " + text + " " + ansiReset
}

func colorStatus(name, categoryKey string) string {
	if !colorsEnabled() {
		return name
	}
	switch categoryKey {
	case "indeterminate":
		return badge(ansiBgInProgress, name)
	case "done":
		return badge(ansiBgDone, name)
	default: // "new" — To Do, Open, Ready
		return badge(ansiBgToDo, name)
	}
}

func colorPriority(name string) string {
	if !colorsEnabled() {
		return name
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "critical"):
		return ansiFgRed + ansiBold + name + ansiReset
	case strings.Contains(lower, "high"):
		return ansiFgOrange + name + ansiReset
	case strings.Contains(lower, "low"):
		return ansiFgGrey + name + ansiReset
	}
	return name
}

// sectionLabel returns a bold section heading when colours are enabled.
func sectionLabel(s string) string {
	if !colorsEnabled() {
		return s
	}
	return ansiBold + s + ansiReset
}

// renderSearchPlain renders human-readable search output.
// The drill-down hint is shown once before the list (first item) and once in
// the footer (last item) so an LLM sees it whether it reads the head or tail.
func renderSearchPlain(resp jira.SearchResponse, effectiveJQL, originalJQL string, page, limit int, flags SearchFlags) (string, error) {
	var sb strings.Builder
	pages := totalPages(resp.Total, limit)
	if pages == 0 {
		pages = 1
	}
	startAt := (page - 1) * limit

	// Header lines.
	fmt.Fprintf(&sb, "search: %s\n", effectiveJQL)
	fmt.Fprintf(&sb, "total: %d  page: %d/%d\n", resp.Total, page, pages)

	// Show the drill-down pattern before the list using the first item as example.
	if len(resp.Issues) > 0 {
		fmt.Fprintf(&sb, "→ jiracli show %s  # (and any key below)\n", resp.Issues[0].Key)
	}
	sb.WriteByte('\n')

	now := time.Now()
	for i, raw := range resp.Issues {
		n := startAt + i + 1
		statusName := raw.Fields.Status.Name
		statusCatKey := raw.Fields.Status.StatusCategory.Key
		issueType := raw.Fields.IssueType.Name
		priority := ""
		if raw.Fields.Priority != nil {
			priority = raw.Fields.Priority.Name
		}
		assignee := "—"
		if raw.Fields.Assignee != nil {
			assignee = raw.Fields.Assignee.DisplayName
		}
		updated := parseUpdated(raw.Fields.Updated, now)

		// Line 1: type badge + key + cropped summary + status right-aligned at col 110
		sb.WriteString(titleLine(n, colorIssueType(issueType), raw.Key, raw.Fields.Summary,
			colorStatus(statusName, statusCatKey)))
		// Line 2: priority, assignee, updated
		fmt.Fprintf(&sb, "    Prio: %s  Assignee: %s  Updated: %s ago\n",
			colorPriority(priority),
			assignee, updated)
		sb.WriteByte('\n')
	}

	// Footer: repeat the drill hint with the last item as example, then pagination.
	returned := len(resp.Issues)
	if returned > 0 {
		fmt.Fprintf(&sb, "→ jiracli show %s  # (and any key above)\n", resp.Issues[returned-1].Key)
	}
	hasMore := resp.Total > startAt+returned
	if hasMore {
		nextCmd := buildNextPageCmd(originalJQL, page+1, limit, flags)
		fmt.Fprintf(&sb, "--- page %d of %d | next: %s ---\n", page, pages, nextCmd)
	} else {
		fmt.Fprintf(&sb, "--- page %d of %d ---\n", page, pages)
	}

	return sb.String(), nil
}

// parseUpdated parses a Jira updated timestamp and formats it as a relative time.
// Falls back to the raw string if parsing fails.
func parseUpdated(raw string, now time.Time) string {
	t, err := time.Parse(updatedLayout, raw)
	if err != nil {
		// Try without milliseconds.
		t, err = time.Parse("2006-01-02T15:04:05-0700", raw)
		if err != nil {
			return raw
		}
	}
	return jira.FormatRelative(t, now)
}

// buildNextPageCmd reconstructs the jiracli search command for the next page.
func buildNextPageCmd(jql string, nextPage, limit int, flags SearchFlags) string {
	var parts []string
	parts = append(parts, "jiracli search")
	parts = append(parts, fmt.Sprintf("--page %d", nextPage))
	parts = append(parts, fmt.Sprintf("--limit %d", limit))
	// --exclude-done is superseded by --category; omit it from the footer when
	// a category filter is active to avoid confusing/redundant next-page commands.
	if flags.ExcludeDone && flags.Category == "" {
		parts = append(parts, "--exclude-done")
	}
	if flags.Category != "" {
		parts = append(parts, fmt.Sprintf("--category %s", flags.Category))
	}
	if flags.Assigned {
		parts = append(parts, "--assigned")
	}
	if flags.Fields != "" {
		parts = append(parts, fmt.Sprintf("--fields %q", flags.Fields))
	}
	if flags.Profile != "" {
		parts = append(parts, fmt.Sprintf("--profile %s", flags.Profile))
	}
	parts = append(parts, fmt.Sprintf("%q", jql))
	return strings.Join(parts, " ")
}

// totalPages computes the number of pages given a total count and page size.
func totalPages(total, limit int) int {
	if limit <= 0 {
		return 1
	}
	return int(math.Ceil(float64(total) / float64(limit)))
}
