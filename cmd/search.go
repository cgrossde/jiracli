package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
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
	FieldsOnly  string
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
	c.Flags().StringVar(&flags.Fields, "fields", "", `Add/drop fields from the default set: "name" or "+name" to add, "-name" to drop`)
	c.Flags().StringVar(&flags.FieldsOnly, "fields-only", "", `Restrict fetched fields to exactly this comma-separated list (replaces defaults; mutually exclusive with --fields)`)
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

	// Resolve fields — mutex check first (no I/O needed to reject).
	if flags.FieldsOnly != "" && flags.Fields != "" {
		return "", fmt.Errorf("--fields and --fields-only are mutually exclusive — choose one")
	}
	var fields []string
	if flags.FieldsOnly != "" {
		parts := strings.Split(flags.FieldsOnly, ",")
		fields = make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				fields = append(fields, p)
			}
		}
	} else {
		fields = resolveSearchFields(flags.Fields)
	}

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
	return renderSearchPlain(resp, effectiveJQL, jql, page, limit, flags, fields)
}

// resolveSearchFields applies +add / -drop semantics to defaultSearchFields.
// Bare names add. "+name" is accepted as an alias for "name". "-name" drops.
// Replacement is no longer supported here — use the --fields-only flag.
func resolveSearchFields(spec string) []string {
	set := make([]string, len(defaultSearchFields))
	copy(set, defaultSearchFields)
	if spec == "" {
		return set
	}
	for _, t := range strings.Split(spec, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "-") {
			set = removeStr(set, t[1:])
			continue
		}
		name := strings.TrimPrefix(t, "+")
		if !containsStr(set, name) {
			set = append(set, name)
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
func renderSearchPlain(resp jira.SearchResponse, effectiveJQL, originalJQL string, page, limit int, flags SearchFlags, fields []string) (string, error) {
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
	// Extra fields: requested fields beyond the default set (excluding "description" which has its own line).
	var extraFields []string
	for _, f := range fields {
		if !containsStr(defaultSearchFields, f) && f != "description" {
			extraFields = append(extraFields, f)
		}
	}
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
		// Line 3 (optional): description preview
		if containsStr(fields, "description") {
			if preview := descPreview(raw.Fields.Description); preview != "" {
				fmt.Fprintf(&sb, "    %s\n", preview)
			}
		}
		// Line 4 (optional): extra fields — always shown when requested, "—" when absent.
		if len(extraFields) > 0 {
			var parts []string
			for _, f := range extraFields {
				v := extractFieldValue(raw, f)
				if v == "" {
					v = "—"
				}
				parts = append(parts, fmt.Sprintf("%s: %s", fieldLabel(f), v))
			}
			fmt.Fprintf(&sb, "    %s\n", strings.Join(parts, "  "))
		}
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

// fieldLabel converts a Jira field ID to a display label.
// Falls back to a title-cased version of the ID when no explicit mapping exists.
func fieldLabel(field string) string {
	switch field {
	case "resolution":
		return "Resolution"
	case "timeestimate":
		return "Remaining"
	case "timeoriginalestimate":
		return "Estimate"
	case "timespent":
		return "Spent"
	case "reporter":
		return "Reporter"
	case "fixVersions":
		return "Fix Version"
	case "labels":
		return "Labels"
	case "components":
		return "Components"
	case "duedate":
		return "Due"
	case "environment":
		return "Env"
	}
	return field
}

// extractFieldValue returns a display string for a named field on an IssueRaw.
// Returns "" when the field is absent, null, or has no meaningful value.
// Handles typed Fields struct fields and falls back to RawFields for the rest.
func extractFieldValue(raw jira.IssueRaw, field string) string {
	switch field {
	case "resolution":
		if raw.Fields.Resolution != nil {
			return raw.Fields.Resolution.Name
		}
		return ""
	case "reporter":
		if raw.Fields.Reporter != nil {
			return raw.Fields.Reporter.DisplayName
		}
		return ""
	case "fixVersions":
		names := make([]string, 0, len(raw.Fields.FixVersions))
		for _, fv := range raw.Fields.FixVersions {
			names = append(names, fv.Name)
		}
		return strings.Join(names, ", ")
	case "labels":
		return strings.Join(raw.Fields.Labels, ", ")
	case "components":
		names := make([]string, 0, len(raw.Fields.Components))
		for _, c := range raw.Fields.Components {
			names = append(names, c.Name)
		}
		return strings.Join(names, ", ")
	case "priority":
		if raw.Fields.Priority != nil {
			return raw.Fields.Priority.Name
		}
		return ""
	case "assignee":
		if raw.Fields.Assignee != nil {
			return raw.Fields.Assignee.DisplayName
		}
		return ""
	case "status":
		return raw.Fields.Status.Name
	case "issuetype":
		return raw.Fields.IssueType.Name
	case "summary":
		return raw.Fields.Summary
	case "description":
		return "" // rendered separately
	}

	// Fall back to RawFields for any untyped field (e.g. timeestimate, duedate).
	v, ok := raw.RawFields[field]
	if !ok || string(v) == "null" || string(v) == `""` {
		return ""
	}

	// Integer: Jira time fields are seconds — format as hours/minutes.
	var n int64
	if json.Unmarshal(v, &n) == nil {
		return formatSeconds(n)
	}

	// Plain string.
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}

	// Object with a "name" field (e.g. version, component, user).
	var obj struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Value       string `json:"value"`
	}
	if json.Unmarshal(v, &obj) == nil {
		if obj.DisplayName != "" {
			return obj.DisplayName
		}
		if obj.Name != "" {
			return obj.Name
		}
		if obj.Value != "" {
			return obj.Value
		}
	}

	// Array: collect "name" or "displayName" strings.
	var arr []json.RawMessage
	if json.Unmarshal(v, &arr) == nil && len(arr) > 0 {
		var names []string
		for _, item := range arr {
			var o struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			}
			if json.Unmarshal(item, &o) == nil {
				if o.Name != "" {
					names = append(names, o.Name)
				} else if o.DisplayName != "" {
					names = append(names, o.DisplayName)
				}
			} else {
				var s string
				if json.Unmarshal(item, &s) == nil && s != "" {
					names = append(names, s)
				}
			}
		}
		return strings.Join(names, ", ")
	}

	return ""
}

// formatSeconds converts a Jira time-in-seconds value to a human-readable string.
// Jira stores time in seconds; typical granularity is 1h = 3600s.
func formatSeconds(secs int64) string {
	if secs <= 0 {
		return ""
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

const descPreviewLen = 100

var (
	// descMacroRe strips ALL Jira {macro} and {macro:params} tags (panel, color, code, noformat, …).
	descMacroRe = regexp.MustCompile(`\{[a-zA-Z][^}]*\}`)
	// descFmtRe strips symmetric inline-formatting markers: *bold*, _italic_, +underline+, -strike-.
	descFmtRe = regexp.MustCompile(`[*_+\-]([^*_+\-\n]+)[*_+\-]`)
	// descStrayFmtRe strips bare unmatched opening markers (e.g. *Text with no closing *).
	// RE2 has no lookbehind; captures (space|^) and first word char, drops the marker.
	descStrayFmtRe = regexp.MustCompile(`(^|\s)[*_+](\S)`)
	descLineMarkerRe = regexp.MustCompile(`(?m)^\s*[*#+\-]+\s+`)
	descLinkRe       = regexp.MustCompile(`\[([^\]|]+)\|[^\]]+\]|\[([^\]]+)\]`)
	whitespaceRunRe  = regexp.MustCompile(`  +`)
)

// descPreview produces a single-line preview of a Jira wiki-markup description.
// Strips block delimiters, collapses whitespace, drops leading list bullets,
// rewrites [text|url] / [url] link forms to their text equivalent, and
// truncates to descPreviewLen runes with an ellipsis when clipped.
// Returns "" if the result is empty after stripping.
func descPreview(s string) string {
	if s == "" {
		return ""
	}
	// Strip ALL Jira {macro} / {macro:params} tags (panel, color, code, noformat, …).
	s = descMacroRe.ReplaceAllString(s, "")
	// Strip inline formatting markers (*bold*, _italic_, +underline+, -strike-) — keep text.
	s = descFmtRe.ReplaceAllString(s, "$1")
	// Strip bare unmatched opening markers left after macro removal (e.g. *Text without closing).
	s = descStrayFmtRe.ReplaceAllString(s, "$1$2")
	// Strip leading list-marker runs (*, #, -, +) at line start.
	s = descLineMarkerRe.ReplaceAllString(s, "")
	// Resolve wiki links: [text|url] → "text"; [url] → "url".
	s = descLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := descLinkRe.FindStringSubmatch(m)
		if sub[1] != "" {
			return sub[1]
		}
		return sub[2]
	})
	// Normalise whitespace.
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ", "\u00a0", " ").Replace(s)
	s = whitespaceRunRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return jira.TruncateString(s, descPreviewLen)
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
	if flags.FieldsOnly != "" {
		parts = append(parts, fmt.Sprintf("--fields-only %q", flags.FieldsOnly))
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
