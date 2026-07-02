package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// SearchFlags holds parsed flag values for the search command.
type SearchFlags struct {
	Profile     string
	JSON        bool
	KeysOnly    bool
	Limit       int
	Page        int
	ExcludeDone bool
	Open        bool
	Fields      string
	FieldsOnly  string
	Assigned    bool
	State       string
	JQL         string // --jql: entire query as one string, bypasses arg joining
	Time        bool   // --time: shorthand for adding timeoriginalestimate, timeestimate, timespent columns
	CountBy     string // --count-by: aggregate matching issues by this field; replaces issue list with a count table
	All         bool   // --all: for --count-by, bypass the default safety cap and count every match

	// LimitSet records whether --limit was explicitly passed by the caller
	// (via cmd.Flags().Changed("limit")). --limit's default of 50 is
	// meaningless for --count-by (which otherwise paginates to exhaustion),
	// so we only treat --limit as an explicit cap for count-by when the user
	// actually typed it.
	LimitSet bool

	// Pagination-hint overrides. When PageCmdBase is set (e.g. by "show
	// assigned"), the next-page footer uses that base command and omits the
	// synthesised JQL, echoing --state from PageCmdState instead. These fields
	// never affect the JQL that is actually sent to Jira.
	PageCmdBase  string
	PageCmdState string
}

// defaultSearchFields is the default field set requested from the Jira API.
var defaultSearchFields = []string{
	"key", "status", "issuetype", "priority", "assignee",
	"updated", "summary", "labels", "components", "timetracking",
}

// updatedLayout is the Jira API date-time format for the updated field.
const updatedLayout = "2006-01-02T15:04:05.000-0700"

// NewSearchCmd builds the "search" command.
func NewSearchCmd() *cobra.Command {
	var flags SearchFlags
	c := &cobra.Command{
		Use:   "search [<jql...>]",
		Short: "Search Jira issues with JQL",
		Long: `Search Jira issues using JQL.

Positional arguments are joined with a space to form the JQL query. When the
query contains quoted string literals (e.g. text ~ "KSP"), shell quoting can
break the join. Use --jql to pass the entire query as a single string and bypass
the join entirely:

  jiracli search --jql 'text ~ "KSP" AND project = CAR'

Bare JQL and --jql queries return all issues by default, including Done. Use
--exclude-done (alias: --open) to hide the "Done" status category, or --state to
filter by category (todo, in-progress, done, all).

--assigned restricts results to the current user and, unless you also pass
--state, defaults to non-Done issues (i.e. it implies statusCategory != "Done").
Pass --state all together with --assigned to include Done.

Fields reference (--fields / --fields-only):
  Default columns: key, status, issuetype, priority, assignee, updated, summary
  Standard extras: description, reporter, labels, components, fixVersions,
                   resolution, duedate, timeestimate, timeoriginalestimate, timespent
  Any Jira field ID (e.g. customfield_10031) is also accepted.
  Syntax: "reporter" or "+reporter" to add, "-priority" to drop.
  Use jiracli lookup fields to list all available field IDs.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --open is an alias for --exclude-done (shared vocabulary with
			// hierarchy/effort); normalise early so every downstream path,
			// including the next-page footer, sees a single flag.
			if flags.Open {
				flags.ExcludeDone = true
			}
			if flags.JQL != "" && len(args) > 0 {
				return fmt.Errorf("--jql and positional JQL arguments are mutually exclusive")
			}
			flags.LimitSet = cmd.Flags().Changed("limit")
			if flags.CountBy != "" {
				switch flags.CountBy {
				case "status", "statusCategory", "priority", "assignee", "issueType", "resolution", "project":
					// ok
				default:
					return fmt.Errorf("--count-by: unsupported field %q (supported: status, statusCategory, priority, assignee, issueType, resolution, project)", flags.CountBy)
				}
				if flags.KeysOnly {
					return fmt.Errorf("--count-by and --keys-only are mutually exclusive")
				}
			}
			if !flags.Assigned && flags.JQL == "" && len(args) == 0 {
				return fmt.Errorf("requires at least 1 arg (JQL), --jql <query>, or use --assigned")
			}
			jql := flags.JQL
			if jql == "" {
				jql = strings.Join(args, " ")
			}
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
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Maximum results per page (1-100). For --count-by, an explicit --limit instead caps the total issues counted (any positive value; see --count-by)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	c.Flags().BoolVar(&flags.ExcludeDone, "exclude-done", false, "Exclude issues in the Done status category")
	c.Flags().BoolVar(&flags.Open, "open", false, "Exclude Done issues (alias for --exclude-done)")
	c.Flags().StringVar(&flags.Fields, "fields", "", "Add/drop display columns: \"name\" or \"+name\" to add, \"-name\" to drop. "+
		"Standard names: description, reporter, labels, components, fixVersions, resolution, duedate, timeestimate, timespent. "+
		"Any Jira field ID is also accepted. See --help for the full reference.")
	c.Flags().StringVar(&flags.FieldsOnly, "fields-only", "", `Restrict fetched fields to exactly this comma-separated list (replaces defaults; mutually exclusive with --fields)`)
	c.Flags().BoolVar(&flags.Assigned, "assigned", false, "Show only issues assigned to me (excludes Done unless --state is set; use --state all to include Done)")
	c.Flags().StringVar(&flags.State, "state", "", "Status category filter: todo, in-progress, done, all")
	c.Flags().StringVar(&flags.JQL, "jql", "", "Entire JQL query as one string — bypasses positional-arg joining; safe for queries with quoted literals like text ~ \"KSP\"")
	c.Flags().BoolVar(&flags.KeysOnly, "keys-only", false, "Print one issue key per line; ideal for piping into further commands (e.g. xargs jiracli show)")
	c.Flags().BoolVar(&flags.Time, "time", false, "Show time-tracking columns: Estimate, Remaining, Spent (shorthand for --fields +timeoriginalestimate,+timeestimate,+timespent; ignored when --fields-only is used)")
	c.Flags().StringVar(&flags.CountBy, "count-by", "",
		"Aggregate matching issues by this field; replaces the issue list with a count/percent table. "+
			"Supported: status, statusCategory, priority, assignee, issueType, resolution, project. "+
			"Paginates internally to exhaustion; --page is ignored. By default, refuses to run if more than "+
			"500 issues match (broad queries can otherwise take minutes to hours) — re-run with --all to count "+
			"every match, or pass an explicit --limit N to cap the count at N issues (reported as partial).")
	c.Flags().BoolVar(&flags.All, "all", false, "For --count-by: count every matching issue, bypassing the 500-issue safety cap (ignored otherwise)")
	return c
}

// buildEffectiveJQL combines the user-supplied JQL with the --assigned,
// --state, and --exclude-done flags into the query actually sent to Jira.
//
// Precedence mirrors the flag semantics shared with hierarchy/effort:
//   - --state takes priority over --exclude-done.
//   - --state all yields an empty status clause, so the query is used as-is
//     (no dangling "AND"); this is the bug-fixed path.
func buildEffectiveJQL(jql string, flags SearchFlags) (string, error) {
	if flags.Assigned {
		// --assigned overrides any JQL args when none provided; otherwise prepends.
		if jql == "" {
			return buildAssignedJQL(flags.State)
		}
		catClause, err := stateJQLClause(flags.State)
		if err != nil {
			return "", err
		}
		effective := `assignee = currentUser() AND (` + jql + `)`
		if catClause != "" {
			effective += ` AND ` + catClause
		}
		return effective, nil
	}

	if flags.State != "" {
		catClause, err := stateJQLClause(flags.State)
		if err != nil {
			return "", err
		}
		switch {
		case catClause == "":
			// --state all: no status filter, use the query as-is (includes Done).
			return jql, nil
		case jql == "":
			return catClause + ` ORDER BY updated DESC`, nil
		default:
			return `(` + jql + `) AND ` + catClause, nil
		}
	}

	if flags.ExcludeDone {
		return jira.DefaultOpenFilter(jql), nil
	}

	return jql, nil
}

// Search is the Layer 1 implementation for the search command.
func Search(ctx context.Context, flags SearchFlags, jql string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	effectiveJQL, err := buildEffectiveJQL(jql, flags)
	if err != nil {
		return "", err
	}

	// --count-by: paginate to exhaustion and return a histogram table.
	if flags.CountBy != "" {
		return runCountBy(ctx, client, effectiveJQL, flags)
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
			if p == "" {
				continue
			}
			// Map the "sprint" alias to the configured custom-field ID so the
			// API returns sprint data; drop it when the field is not configured.
			if p == "sprint" {
				if sf := entry.Agile.SprintField; sf != "" {
					fields = append(fields, sf)
				}
				continue
			}
			fields = append(fields, p)
		}
	} else {
		fields = resolveSearchFields(flags.Fields)
		// Append Story Points custom field when configured and not already present.
		if entry.Hierarchy.StoryPointsField != "" && !containsStr(fields, entry.Hierarchy.StoryPointsField) {
			fields = append(fields, entry.Hierarchy.StoryPointsField)
		}
		// Swap "sprint" alias for the configured sprint custom field ID.
		// Pattern: if caller wrote "--fields sprint" or "+sprint", replace with
		// the real field ID; remove it when sprint field is not configured.
		if sf := entry.Agile.SprintField; sf != "" {
			for i, f := range fields {
				if f == "sprint" {
					fields[i] = sf
				}
			}
		} else {
			fields = removeStr(fields, "sprint")
		}
		// --time: append the three time-tracking fields when not already present.
		// Silently ignored when --fields-only is set (branch above).
		if flags.Time {
			for _, f := range []string{"timeoriginalestimate", "timeestimate", "timespent"} {
				if !containsStr(fields, f) {
					fields = append(fields, f)
				}
			}
		}
	}
	// --keys-only needs only the key field; override whatever was resolved above
	// to avoid fetching and deserialising unnecessary data from the Jira API.
	if flags.KeysOnly {
		fields = []string{"key"}
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

	if flags.KeysOnly {
		return renderKeysOnly(resp, page, limit, jql, flags)
	}
	if flags.JSON {
		return renderSearchJSON(resp, page, limit, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
	}
	return renderSearchPlain(resp, effectiveJQL, jql, page, limit, flags, fields, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
}

// baseTemplateFields are rendered in the fixed two-line search layout, so they
// are never repeated on the extra-fields line.
var baseTemplateFields = []string{"key", "status", "issuetype", "priority", "assignee", "updated", "summary"}

// displayExtraFields returns the ordered list of fields the caller explicitly
// asked to see as extra columns, derived from --fields (+name) and --time.
//
// It is deliberately based on the request rather than the fetched set: labels
// and components are fetched by default (for --json completeness) but must only
// appear in plain output when the caller adds them via --fields.
func displayExtraFields(flags SearchFlags, spField, sprintField string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		// Map the "sprint" alias to the configured custom-field ID so the value
		// is looked up under the right RawFields key; drop when unconfigured.
		if name == "sprint" {
			if sprintField == "" {
				return
			}
			name = sprintField
		}
		if name == "" || name == "description" || containsStr(baseTemplateFields, name) {
			return
		}
		if spField != "" && name == spField {
			return // story points surface in --json only, not as a plain column
		}
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	if flags.Fields != "" {
		for _, t := range strings.Split(flags.Fields, ",") {
			t = strings.TrimSpace(t)
			if t == "" || strings.HasPrefix(t, "-") {
				continue // drops are not extra columns
			}
			add(strings.TrimPrefix(t, "+"))
		}
	}
	if flags.Time {
		add("timeoriginalestimate")
		add("timeestimate")
		add("timespent")
	}
	return out
}

// renderFieldsOnly renders each issue using exactly the fields listed in
// --fields-only, with no fixed template. The key anchors each entry; every other
// requested field is shown as "Label: value" ("—" when absent). "updated" and
// "created" are formatted as relative times; "description" as a preview.
func renderFieldsOnly(sb *strings.Builder, issues []jira.IssueRaw, startAt int, fields []string, now time.Time, sprintField string) {
	for i, raw := range issues {
		n := startAt + i + 1
		fmt.Fprintf(sb, "[%d] %s\n", n, raw.Key)
		var parts []string
		for _, f := range fields {
			if f == "key" {
				continue // already the anchor
			}
			var v string
			switch f {
			case "updated":
				v = parseUpdated(raw.Fields.Updated, now) + " ago"
			case "description":
				v = descPreview(raw.Fields.Description)
			default:
				v = extractFieldValue(raw, f, sprintField)
			}
			if v == "" {
				v = "—"
			}
			parts = append(parts, fmt.Sprintf("%s: %s", fieldLabel(f, sprintField), v))
		}
		if len(parts) > 0 {
			fmt.Fprintf(sb, "    %s\n", strings.Join(parts, "  "))
		}
		sb.WriteByte('\n')
	}
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

// renderKeysOnly emits one issue key per line — no header, no footer prose.
// Ideal for piping: jiracli search --keys-only ... | xargs -I{} jiracli show {}
// When more pages exist, the last line is a comment hint (# next: <cmd>) so
// scripts can detect and continue pagination.
func renderKeysOnly(resp jira.SearchResponse, page, limit int, originalJQL string, flags SearchFlags) (string, error) {
	var sb strings.Builder
	for _, raw := range resp.Issues {
		sb.WriteString(raw.Key)
		sb.WriteByte('\n')
	}
	returned := len(resp.Issues)
	startAt := (page - 1) * limit
	if resp.Total > startAt+returned {
		nextCmd := buildNextPageCmd(originalJQL, page+1, limit, flags)
		fmt.Fprintf(&sb, "# next: %s\n", nextCmd)
	}
	return sb.String(), nil
}

// renderSearchJSON emits one NDJSON record per issue, then a pagination trailer
// if more pages exist.
func renderSearchJSON(resp jira.SearchResponse, page, limit int, spField, sprintField string) (string, error) {
	var sb strings.Builder
	for _, raw := range resp.Issues {
		rec := jira.ToSearchRecord(raw, spField, sprintField)
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
func renderSearchPlain(resp jira.SearchResponse, effectiveJQL, originalJQL string, page, limit int, flags SearchFlags, fields []string, spField, sprintField string) (string, error) {
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

	if flags.FieldsOnly != "" {
		// --fields-only: render exactly the requested fields, no fixed template.
		// This avoids leaking empty scaffolding (e.g. a bare "Updated:  ago") for
		// fields the caller did not ask for.
		renderFieldsOnly(&sb, resp.Issues, startAt, fields, now, sprintField)
	} else {
		// Extra columns the caller explicitly asked for (via --fields +name or
		// --time). Derived from the request, not the fetched set: labels and
		// components are fetched by default but only rendered when asked for.
		extraFields := displayExtraFields(flags, spField, sprintField)
		for i, raw := range resp.Issues {
			n := startAt + i + 1
			statusName := raw.Fields.Status.Name
			statusCatKey := raw.Fields.Status.StatusCategory.Key
			issueType := raw.Fields.IssueType.Name

			// Line 1: type badge + key + cropped summary + status right-aligned at col 110
			sb.WriteString(titleLine(n, colorIssueType(issueType), raw.Key, raw.Fields.Summary,
				colorStatus(statusName, statusCatKey)))
			// Line 2: priority, assignee, updated — each column is omitted when
			// the caller dropped it via --fields "-priority"/"-assignee"/"-updated"
			// rather than rendered with an empty/dash value.
			var line2 []string
			if containsStr(fields, "priority") {
				priority := ""
				if raw.Fields.Priority != nil {
					priority = raw.Fields.Priority.Name
				}
				line2 = append(line2, fmt.Sprintf("Prio: %s", colorPriority(priority)))
			}
			if containsStr(fields, "assignee") {
				assignee := "—"
				if raw.Fields.Assignee != nil {
					assignee = raw.Fields.Assignee.DisplayName
				}
				line2 = append(line2, fmt.Sprintf("Assignee: %s", assignee))
			}
			if containsStr(fields, "updated") {
				line2 = append(line2, fmt.Sprintf("Updated: %s ago", parseUpdated(raw.Fields.Updated, now)))
			}
			if len(line2) > 0 {
				fmt.Fprintf(&sb, "    %s\n", strings.Join(line2, "  "))
			}
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
					v := extractFieldValue(raw, f, sprintField)
					if v == "" {
						v = "—"
					}
					parts = append(parts, fmt.Sprintf("%s: %s", fieldLabel(f, sprintField), v))
				}
				fmt.Fprintf(&sb, "    %s\n", strings.Join(parts, "  "))
			}
			sb.WriteByte('\n')
		}
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
func fieldLabel(field, sprintField string) string {
	if sprintField != "" && field == sprintField {
		return "Sprint"
	}
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
	case "key":
		return "Key"
	case "summary":
		return "Summary"
	case "status":
		return "Status"
	case "issuetype":
		return "Type"
	case "priority":
		return "Prio"
	case "assignee":
		return "Assignee"
	case "updated":
		return "Updated"
	case "created":
		return "Created"
	}
	return field
}

// extractFieldValue returns a display string for a named field on an IssueRaw.
// Returns "" when the field is absent, null, or has no meaningful value.
// Handles typed Fields struct fields and falls back to RawFields for the rest.
func extractFieldValue(raw jira.IssueRaw, field, sprintField string) string {
	// Sprint custom field: render names compactly rather than dumping the raw
	// GreenHopper object/toString (e.g. "Sprint 152, Sprint 153 (active)").
	if sprintField != "" && field == sprintField {
		if v, ok := raw.RawFields[field]; ok {
			return jira.FormatSprintField(v)
		}
		return ""
	}
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
		return jira.FormatSeconds(n)
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

const descPreviewLen = 100

var (
	// descMacroRe strips ALL Jira {macro} and {macro:params} tags (panel, color, code, noformat, …).
	descMacroRe = regexp.MustCompile(`\{[a-zA-Z][^}]*\}`)
	// descFmtRe strips symmetric inline-formatting markers: *bold*, _italic_, +underline+, -strike-.
	descFmtRe = regexp.MustCompile(`[*_+\-]([^*_+\-\n]+)[*_+\-]`)
	// descStrayFmtRe strips bare unmatched opening markers (e.g. *Text with no closing *).
	// RE2 has no lookbehind; captures (space|^) and first word char, drops the marker.
	descStrayFmtRe   = regexp.MustCompile(`(^|\s)[*_+](\S)`)
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
	// Assigned mode (and any other command that pre-builds its own JQL): the
	// footer must point back at the invoking command, not at "jiracli search"
	// with a synthesised query the user never typed.
	if flags.PageCmdBase != "" {
		var parts []string
		parts = append(parts, flags.PageCmdBase)
		parts = append(parts, fmt.Sprintf("--page %d", nextPage))
		parts = append(parts, fmt.Sprintf("--limit %d", limit))
		if flags.PageCmdState != "" {
			parts = append(parts, fmt.Sprintf("--state %s", flags.PageCmdState))
		}
		if flags.Profile != "" {
			parts = append(parts, fmt.Sprintf("--profile %s", flags.Profile))
		}
		if flags.KeysOnly {
			parts = append(parts, "--keys-only")
		}
		return strings.Join(parts, " ")
	}

	var parts []string
	parts = append(parts, "jiracli search")
	parts = append(parts, fmt.Sprintf("--page %d", nextPage))
	parts = append(parts, fmt.Sprintf("--limit %d", limit))
	// --exclude-done is superseded by --state; omit it from the footer when
	// a state filter is active to avoid confusing/redundant next-page commands.
	if flags.ExcludeDone && flags.State == "" {
		parts = append(parts, "--exclude-done")
	}
	if flags.State != "" {
		parts = append(parts, fmt.Sprintf("--state %s", flags.State))
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
	// Emit --jql when the original query came from that flag, or when the JQL
	// contains characters (double-quotes, parens, ~) that are unsafe to pass as
	// a bare positional argument. Skip entirely when jql is empty (e.g. --assigned
	// with no additional JQL — the search is fully determined by --assigned).
	if jql != "" {
		if flags.JQL != "" || strings.ContainsAny(jql, `"~()`) {
			parts = append(parts, fmt.Sprintf("--jql %q", jql))
		} else {
			parts = append(parts, fmt.Sprintf("%q", jql))
		}
	}
	if flags.KeysOnly {
		parts = append(parts, "--keys-only")
	}
	return strings.Join(parts, " ")
}

// totalPages computes the number of pages given a total count and page size.
func totalPages(total, limit int) int {
	if limit <= 0 {
		return 1
	}
	return int(math.Ceil(float64(total) / float64(limit)))
}

// ── count-by aggregation ─────────────────────────────────────────────────────

// countByDefaultCap is the maximum number of matching issues --count-by will
// paginate through when neither --all nor an explicit --limit is given.
// Broad, unscoped count-by queries can otherwise paginate to exhaustion for
// minutes or hours on a large instance with zero feedback in the interim —
// this mirrors the "truncation is an error" guardrail effort/hierarchy
// already use, just with a bigger cap suited to count-by's typically broader
// queries.
const countByDefaultCap = 500

// runCountBy fetches matching issues (up to a safety cap unless overridden)
// and emits a count/percent histogram table.
func runCountBy(ctx context.Context, client *jira.Client, effectiveJQL string, flags SearchFlags) (string, error) {
	capN := 0 // 0 = unlimited
	abortIfOverCap := false
	switch {
	case flags.All:
		// unlimited, never abort
	case flags.LimitSet:
		capN = flags.Limit
	default:
		capN = countByDefaultCap
		abortIfOverCap = true
	}
	return countByFromRawSearch(ctx, client, effectiveJQL, flags.CountBy, flags.JSON, capN, abortIfOverCap)
}

// errCountByTooLarge reports that a --count-by aggregation would need to
// paginate through more issues than the default safety cap allows.
func errCountByTooLarge(matched, capN int) error {
	return fmt.Errorf(
		"count-by aborted: %d issues match this query, which exceeds the default safety cap of %d — "+
			"counting that many could take a long time. Re-run with --all to count every matching issue, "+
			"or add --limit N to cap the count at N issues (the result will be reported as partial)",
		matched, capN)
}

// countByFromRawSearch pages through search results and aggregates by the
// chosen dimension.
//
// capN bounds how many issues are counted: 0 means unlimited (paginate to
// exhaustion). When capN > 0 and more issues match than capN:
//   - if abortIfOverCap is true, the aggregation is refused outright
//     (errCountByTooLarge) instead of silently running for a long time;
//   - otherwise (an explicit --limit was given), counting stops at capN and
//     the result is rendered with a note that the count is partial.
func countByFromRawSearch(ctx context.Context, client *jira.Client, jql, dim string, asJSON bool, capN int, abortIfOverCap bool) (string, error) {
	const pageSize = 100
	page := 1
	counts := map[string]int{}
	order := []string{}
	total := 0   // issues actually counted
	matched := 0 // server-reported total matches, from the first page
	note := ""
	for {
		resp, err := client.Search(ctx, jql, page, pageSize, countByFields(dim))
		if err != nil {
			if total > 0 {
				note = "Jira returned an error mid-paging; counts may be partial"
				break
			}
			return "", fmt.Errorf("search (count-by): %w", err)
		}
		if page == 1 {
			matched = resp.Total
			if capN > 0 && matched > capN && abortIfOverCap {
				return "", errCountByTooLarge(matched, capN)
			}
		}
		for _, raw := range resp.Issues {
			if capN > 0 && total >= capN {
				break
			}
			key := extractDimension(raw, dim)
			if _, seen := counts[key]; !seen {
				order = append(order, key)
			}
			counts[key]++
			total++
		}
		hitCap := capN > 0 && total >= capN
		startAt := (page-1)*pageSize + len(resp.Issues)
		if hitCap || startAt >= resp.Total || len(resp.Issues) == 0 {
			if hitCap && matched > total {
				note = fmt.Sprintf("capped at --limit %d; %d issues matched in total — re-run with --all to count every match", capN, matched)
			}
			break
		}
		page++
	}
	// Zero matches: fall through to the normal renderers (same header/table
	// shape as a non-empty result, just with a 0 total) rather than a
	// bespoke message — this matches plain `search`, which always shows its
	// "total: 0" header rather than swapping in different prose for the
	// empty case.
	if asJSON {
		return renderCountByJSON(dim, order, counts, total, matched, jql, note)
	}
	return renderCountByPlain(dim, order, counts, total, matched, jql, note), nil
}

// countByFields returns the minimal field set needed for the chosen dimension.
func countByFields(dim string) []string {
	switch dim {
	case "status", "statusCategory":
		return []string{"key", "status"}
	case "priority":
		return []string{"key", "priority"}
	case "assignee":
		return []string{"key", "assignee"}
	case "issueType":
		return []string{"key", "issuetype"}
	case "resolution":
		return []string{"key", "resolution"}
	case "project":
		return []string{"key", "project"}
	}
	return []string{"key", "status"}
}

// extractDimension pulls the dimension value from a raw issue.
func extractDimension(raw jira.IssueRaw, dim string) string {
	switch dim {
	case "status":
		return countByDefaultIfEmpty(raw.Fields.Status.Name)
	case "statusCategory":
		return countByDefaultIfEmpty(raw.Fields.Status.StatusCategory.Name)
	case "priority":
		if raw.Fields.Priority == nil {
			return "(none)"
		}
		return countByDefaultIfEmpty(raw.Fields.Priority.Name)
	case "assignee":
		if raw.Fields.Assignee == nil {
			return "Unassigned"
		}
		return countByDefaultIfEmpty(raw.Fields.Assignee.DisplayName)
	case "issueType":
		return countByDefaultIfEmpty(raw.Fields.IssueType.Name)
	case "resolution":
		if raw.Fields.Resolution == nil {
			return "(unresolved)"
		}
		return countByDefaultIfEmpty(raw.Fields.Resolution.Name)
	case "project":
		return countByDefaultIfEmpty(raw.Fields.Project.Key)
	}
	return "(unknown)"
}

func countByDefaultIfEmpty(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// countRow holds one aggregated dimension value for rendering.
type countRow struct {
	Label string
	Count int
}

// statusCountRowLess orders countRows by status meaning (delegates to statusRank in rollup.go).
func statusCountRowLess(rows []countRow, categoryMode bool) func(i, j int) bool {
	return func(i, j int) bool {
		ri, rj := statusRank(rows[i].Label, categoryMode), statusRank(rows[j].Label, categoryMode)
		if ri != rj {
			return ri < rj
		}
		return rows[i].Label < rows[j].Label
	}
}

// dimensionHeader returns the display header for a dimension name.
func dimensionHeader(dim string) string {
	switch dim {
	case "status":
		return "Status"
	case "statusCategory":
		return "Status Category"
	case "priority":
		return "Priority"
	case "assignee":
		return "Assignee"
	case "issueType":
		return "Issue Type"
	case "resolution":
		return "Resolution"
	case "project":
		return "Project"
	}
	return "Value"
}

// renderCountByPlain emits a 3-column table: dimension value, count, percent.
func renderCountByPlain(dim string, order []string, counts map[string]int, total, matched int, jql, note string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "search: %s  (count by %s)\n", jql, dim)
	if note != "" && matched > total {
		fmt.Fprintf(&sb, "total: %d of %d matched issues counted\n\n", total, matched)
	} else {
		fmt.Fprintf(&sb, "total: %d issues\n\n", total)
	}

	rows := make([]countRow, 0, len(order))
	for _, k := range order {
		rows = append(rows, countRow{Label: k, Count: counts[k]})
	}
	if dim == "status" || dim == "statusCategory" {
		sort.SliceStable(rows, statusCountRowLess(rows, dim == "statusCategory"))
	} else {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Count != rows[j].Count {
				return rows[i].Count > rows[j].Count
			}
			return rows[i].Label < rows[j].Label
		})
	}

	const labelW = 24
	const colW = 8
	rule := strings.Repeat("─", labelW+2+colW+2+colW) + "\n"
	fmt.Fprintf(&sb, "%-*s  %*s  %*s\n", labelW, dimensionHeader(dim), colW, "Count", colW, "Percent")
	sb.WriteString(rule)
	for _, r := range rows {
		pct := 0.0
		if total > 0 {
			pct = float64(r.Count) / float64(total) * 100
		}
		fmt.Fprintf(&sb, "%-*s  %*d  %*s\n",
			labelW, jira.TruncateString(r.Label, labelW),
			colW, r.Count,
			colW, fmt.Sprintf("%.1f%%", pct))
	}
	sb.WriteString(rule)
	fmt.Fprintf(&sb, "%-*s  %*d  %*s\n", labelW, "Total", colW, total, colW, "100.0%")
	if note != "" {
		fmt.Fprintf(&sb, "\n⚠ %s\n", note)
	}
	return sb.String()
}

// countByRecord is one aggregated dimension value in the --count-by NDJSON stream.
type countByRecord struct {
	Dimension string  `json:"dimension"`
	Value     string  `json:"value"`
	Count     int     `json:"count"`
	Percent   float64 `json:"percent"`
}

// countByMetaTrailer is the final NDJSON line of a --count-by response. Like the
// _pagination trailer, its sole top-level key is underscore-prefixed so consumers
// can detect a meta line without inspecting the payload. _meta carries the
// aggregation summary: how many issues were counted, how many actually matched
// the query (may exceed total when capped), the JQL, whether the count is
// partial, and — when partial — a human-readable reason.
type countByMetaTrailer struct {
	Meta struct {
		Total     int    `json:"total"`
		Matched   int    `json:"matched"`
		JQL       string `json:"jql"`
		Truncated bool   `json:"truncated"`
		Note      string `json:"note,omitempty"`
	} `json:"_meta"`
}

// renderCountByJSON emits one NDJSON record per dimension value, then a _meta trailer.
func renderCountByJSON(dim string, order []string, counts map[string]int, total, matched int, jql, note string) (string, error) {
	var sb strings.Builder
	for _, k := range order {
		pct := 0.0
		if total > 0 {
			pct = float64(counts[k]) / float64(total) * 100
		}
		rec := countByRecord{
			Dimension: dim,
			Value:     k,
			Count:     counts[k],
			Percent:   math.Round(pct*10) / 10,
		}
		b, err := json.Marshal(rec)
		if err != nil {
			return "", fmt.Errorf("marshal count-by record: %w", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	var meta countByMetaTrailer
	meta.Meta.Total = total
	meta.Meta.Matched = matched
	meta.Meta.JQL = jql
	meta.Meta.Truncated = note != ""
	meta.Meta.Note = note
	b, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal count-by meta trailer: %w", err)
	}
	sb.Write(b)
	sb.WriteByte('\n')
	return sb.String(), nil
}
