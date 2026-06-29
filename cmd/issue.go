package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// IssueFlags holds parsed flag values for the issue command.
type IssueFlags struct {
	Profile    string
	JSON       bool
	NoHistory  bool
	NoComments bool
	CommentsN  int
	Fields     string
	NoChildren bool
	Parent     bool
}

// NewIssueCmd builds the "issue" command.
func NewIssueCmd() *cobra.Command {
	var flags IssueFlags
	c := &cobra.Command{
		Use:   "show <KEY>",
		Short: "Show a Jira issue",
		Long:  "Fetch and display a single Jira issue. Accepts a bare key (ACME-123) or a browse URL.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.CommentsN > 25 {
				return fmt.Errorf("--comments max is 25 — use jiracli show comments %s --limit %d for a longer thread", args[0], flags.CommentsN)
			}
			result, err := Issue(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.NoHistory, "no-history", false, "Skip activity/changelog section")
	c.Flags().BoolVar(&flags.NoComments, "no-comments", false, "Skip comments section")
	c.Flags().IntVar(&flags.CommentsN, "comments", 1, "Number of latest comments to preview (max 25)")
	c.Flags().StringVar(&flags.Fields, "fields", "", "Override field list (default/+add/-drop)")
	c.Flags().BoolVar(&flags.NoChildren, "no-children", false, "Skip fetching the children list (one fewer API call)")
	c.Flags().BoolVar(&flags.Parent, "parent", false, "Show the parent of <KEY> instead (Parent Link → Parent → Epic Link, in that order)")
	return c
}

// Issue is the Layer 1 implementation for the issue command.
func Issue(ctx context.Context, flags IssueFlags, ref string) (string, error) {
	// Validate --comments bound before any I/O
	if flags.CommentsN > 25 {
		return "", fmt.Errorf("--comments max is 25 — use jiracli show comments %s --limit %d for a longer thread", ref, flags.CommentsN)
	}

	// Parse the reference — accepts KEY or browse URL
	parsed, err := jira.ParseRef(ref)
	if err != nil {
		return "", fmt.Errorf("%w\n[stderr] not a valid issue reference: %q — expected ACME-123 or a browse URL", err, ref)
	}
	if parsed.Kind != jira.RefIssue {
		if flags.Parent {
			return "", fmt.Errorf("--parent only applies to issue references, not comments/attachments")
		}
		return "", fmt.Errorf("issue command requires a plain issue key or browse URL, not a comment or attachment reference — run: jiracli show %s", parsed.Key)
	}

	// Resolve profile and build client
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	// --parent: resolve the parent key, then fall through to normal fetch+render.
	if flags.Parent {
		parentKey, err := resolveParentKey(ctx, client, store, parsed.Key)
		if err != nil {
			return "", err
		}
		parsed.Key = parentKey
		flags.Parent = false // prevent infinite recursion
	}

	// Build field list; track whether it's a custom replacement so the renderer
	// can skip sections whose fields were not fetched.
	fields := jira.DefaultIssueFields
	var fieldSet map[string]bool // nil = full default set
	if flags.Fields != "" {
		fields = resolveFieldList(jira.DefaultIssueFields, flags.Fields)
		// Only build a restriction set when it's a full replacement (no +/- modifiers).
		// Additive/subtractive specs stay close to the default and don't need this.
		isReplacement := true
		for _, t := range strings.Split(flags.Fields, ",") {
			t = strings.TrimSpace(t)
			if strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-") {
				isReplacement = false
				break
			}
		}
		if isReplacement {
			fieldSet = make(map[string]bool)
			for _, f := range strings.Split(fields, ",") {
				fieldSet[strings.TrimSpace(f)] = true
			}
		}
	}

	raw, err := client.GetIssue(ctx, parsed.Key, fields, !flags.NoHistory)
	if err != nil {
		return "", err
	}

	commentsN := flags.CommentsN
	if flags.NoComments {
		commentsN = 0
	}

	rec := jira.ToIssueRecord(raw, commentsN)

	// Resolve FromCategory/ToCategory for status transitions in the activity
	// timeline. Uses the cached status list — failure is non-fatal.
	if !flags.NoHistory && len(rec.ActivityTimeline) > 0 {
		if statuses, sErr := client.ListStatuses(ctx, store, false); sErr == nil {
			jira.ResolveActivityStatusCategories(rec.ActivityTimeline, statuses)
		}
	}

	// Backfill IssueType for linked issues via one bulk search.
	// The issuelinks API payload does not include issuetype; fetch it cheaply.
	if len(rec.Links) > 0 {
		keys := make([]string, 0, len(rec.Links))
		seen := make(map[string]bool, len(rec.Links))
		for _, l := range rec.Links {
			if l.Issue.Key != "" && !seen[l.Issue.Key] {
				keys = append(keys, l.Issue.Key)
				seen[l.Issue.Key] = true
			}
		}
		if len(keys) > 0 {
			quotedKeys := make([]string, len(keys))
			for i, k := range keys {
				quotedKeys[i] = `"` + k + `"`
			}
			bulkJQL := `key in (` + strings.Join(quotedKeys, ",") + `)`
			if bulkResp, bulkErr := client.Search(ctx, bulkJQL, 1, len(keys), []string{"issuetype"}); bulkErr == nil {
				typeByKey := make(map[string]string, len(bulkResp.Issues))
				for _, issue := range bulkResp.Issues {
					typeByKey[issue.Key] = issue.Fields.IssueType.Name
				}
				for i := range rec.Links {
					if t, ok := typeByKey[rec.Links[i].Issue.Key]; ok {
						rec.Links[i].Issue.IssueType = t
					}
				}
			}
		}
	}

	// Epic children: fetch via search if this is an Epic (subtasks come free from the raw response).
	if !flags.NoChildren && strings.EqualFold(rec.IssueType, "Epic") {
		epicJQL := `"Epic Link" = ` + parsed.Key
		epicFields := []string{"summary", "status", "assignee", "issuetype", "priority", "updated"}
		epicResp, epicErr := client.Search(ctx, epicJQL, 1, 100, epicFields)
		if epicErr != nil {
			rec.ChildrenError = epicErr.Error()
		} else {
			rec.ChildrenTotal = epicResp.Total
			rec.Children = make([]jira.ChildIssueRecord, 0, len(epicResp.Issues))
			for _, issue := range epicResp.Issues {
				assignee := ""
				if issue.Fields.Assignee != nil {
					assignee = issue.Fields.Assignee.DisplayName
				}
				rec.Children = append(rec.Children, jira.ChildIssueRecord{
					Key:            issue.Key,
					Summary:        issue.Fields.Summary,
					Status:         issue.Fields.Status.Name,
					StatusCategory: issue.Fields.Status.StatusCategory.Name,
					IssueType:      issue.Fields.IssueType.Name,
					Assignee:       assignee,
				})
			}
		}
	}

	if flags.JSON {
		data, err := json.Marshal(rec)
		if err != nil {
			return "", fmt.Errorf("marshal issue: %w", err)
		}
		return string(data) + "\n", nil
	}

	return renderIssue(rec, flags, fieldSet), nil
}

// resolveParentKey fetches the minimal fields of key and returns the parent key
// using the precedence: Parent Link (Portfolio) → Parent → Epic Link.
func resolveParentKey(ctx context.Context, client *jira.Client, store *cache.Store, key string) (string, error) {
	// Resolve Parent Link and Epic Link custom field ids (vary per instance).
	parentLinkFieldID := ""
	if fieldID, _, err := client.ResolveFieldID(ctx, "Parent Link", store, false); err == nil {
		parentLinkFieldID = fieldID
	}
	epicLinkFieldID := "customfield_10014" // default; override if resolvable
	if fieldID, _, err := client.ResolveFieldID(ctx, "Epic Link", store, false); err == nil {
		epicLinkFieldID = fieldID
	}

	baseFields := "summary,issuetype,parent," + epicLinkFieldID
	fetchFields := baseFields
	if parentLinkFieldID != "" && parentLinkFieldID != epicLinkFieldID {
		fetchFields = baseFields + "," + parentLinkFieldID
	}

	raw, err := client.GetIssue(ctx, key, fetchFields, false)
	if err != nil {
		return "", fmt.Errorf("fetching %s to resolve parent: %w", key, err)
	}

	// 1. Parent Link custom field.
	if parentLinkFieldID != "" {
		if pk := extractParentLink(raw.RawFields, parentLinkFieldID); pk != "" {
			return pk, nil
		}
	}

	// 2. Regular Parent field.
	if raw.Fields.Parent != nil && raw.Fields.Parent.Key != "" {
		return raw.Fields.Parent.Key, nil
	}

	// 3. Epic Link (resolved field id, falls back to customfield_10014).
	if pk := extractParentLink(raw.RawFields, epicLinkFieldID); pk != "" {
		return pk, nil
	}

	return "", fmt.Errorf("no parent found for %s — issue has no Parent Link, Parent, or Epic Link", key)
}

// extractParentLink reads a custom field by id from an IssueRaw.RawFields map.
// Returns "" if the field is missing, null, or its key cannot be resolved.
func extractParentLink(rawFields map[string]json.RawMessage, fieldID string) string {
	if rawFields == nil {
		return ""
	}
	raw, ok := rawFields[fieldID]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// Jira returns Parent Link as a string key or as an object {"key":"..."}.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return asString
	}
	var asObj struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil {
		return asObj.Key
	}
	return ""
}

// resolveFieldList applies +add / -drop / replace semantics to the default field list.
// If spec has no leading +/-, it is treated as a full replacement.
// Multiple comma-separated tokens may mix + and - with replacement segments.
func resolveFieldList(defaultFields, spec string) string {
	tokens := strings.Split(spec, ",")
	// If every token starts with + or -, it's incremental; otherwise full replace.
	allIncremental := true
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t != "" && t[0] != '+' && t[0] != '-' {
			allIncremental = false
			break
		}
	}
	if !allIncremental {
		return spec
	}
	// Build ordered set from defaults
	defaults := strings.Split(defaultFields, ",")
	set := make([]string, 0, len(defaults))
	inSet := make(map[string]bool, len(defaults))
	for _, f := range defaults {
		set = append(set, f)
		inSet[f] = true
	}
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "+") {
			name := t[1:]
			if !inSet[name] {
				set = append(set, name)
				inSet[name] = true
			}
		} else if strings.HasPrefix(t, "-") {
			name := t[1:]
			delete(inSet, name)
			filtered := set[:0]
			for _, s := range set {
				if s != name {
					filtered = append(filtered, s)
				}
			}
			set = filtered
		}
	}
	return strings.Join(set, ",")
}

// parseISODate parses an ISO8601/RFC3339 datetime string and returns "YYYY-MM-DD".
// Falls back to the first 10 chars of the raw string on error.
func parseISODate(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Jira sometimes uses "2006-01-02T15:04:05.000+0700" (no colon in offset)
		t, err = time.Parse("2006-01-02T15:04:05.000-0700", s)
		if err != nil {
			if len(s) >= 10 {
				return s[:10]
			}
			return s
		}
	}
	return t.Format("2006-01-02")
}

// parseISODateTime parses an ISO8601 string and returns a time.Time.
// Returns zero value on error.
func parseISODateTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000-0700", s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// fieldIn returns true when fieldSet is nil (full default) or contains the given field name.
func fieldIn(fieldSet map[string]bool, name string) bool {
	return fieldSet == nil || fieldSet[name]
}

// renderIssue produces the plain-text representation per proposal §5.1.
// fieldSet, when non-nil, is the set of fields that were actually fetched from
// the API (replacement --fields spec). Sections whose key fields are absent are
// skipped rather than rendered with empty values.
func renderIssue(rec jira.IssueRecord, flags IssueFlags, fieldSet map[string]bool) string {
	var sb strings.Builder

	// Header line: type-badge  KEY  status-badge · priority
	priority := rec.Priority
	if priority == "" {
		priority = "—"
	}
	typeBadge := colorIssueType(rec.IssueType)
	statusBadge := colorStatusName(rec.Status)
	if colorsEnabled() {
		fmt.Fprintf(&sb, "%s  %s%s%s  %s · %s\n",
			typeBadge,
			ansiBold, rec.Key, ansiReset,
			statusBadge,
			colorPriority(priority))
	} else {
		fmt.Fprintf(&sb, "%s  %s  %s · %s\n", typeBadge, rec.Key, rec.Status, priority)
	}
	if colorsEnabled() {
		fmt.Fprintf(&sb, "%s%s%s\n", ansiBold+ansiFgW, rec.Summary, ansiReset)
	} else {
		fmt.Fprintf(&sb, "%q\n", rec.Summary)
	}
	sb.WriteByte('\n')

	// People & dates — only show when those fields were fetched.
	showAssignee := fieldIn(fieldSet, "assignee")
	showReporter := fieldIn(fieldSet, "reporter")
	showDates := fieldIn(fieldSet, "created") || fieldIn(fieldSet, "updated")
	if showAssignee || showReporter || showDates {
		if showAssignee || showReporter {
			assignee := "(unassigned)"
			if rec.Assignee != nil {
				assignee = fmt.Sprintf("%s (%s)", rec.Assignee.DisplayName, rec.Assignee.Name)
			}
			reporter := ""
			if rec.Reporter != nil {
				reporter = fmt.Sprintf("%s (%s)", rec.Reporter.DisplayName, rec.Reporter.Name)
			}
			fmt.Fprintf(&sb, "%s %-30s %s %s\n",
				sectionLabel("Assignee:"), assignee,
				sectionLabel("Reporter:"), reporter)
		}
		if showDates {
			createdDate := parseISODate(rec.Created)
			updatedDate := parseISODate(rec.Updated)
			fmt.Fprintf(&sb, "%s %-30s %s %s\n",
				sectionLabel("Created:"), createdDate,
				sectionLabel("Updated:"), updatedDate)
		}
		sb.WriteByte('\n')
	}

	// Epic / Parent
	if rec.Epic != nil {
		if rec.Epic.Summary != "" {
			fmt.Fprintf(&sb, "Epic: %s  %q\n", rec.Epic.Key, rec.Epic.Summary)
		} else {
			fmt.Fprintf(&sb, "Epic: %s\n", rec.Epic.Key)
		}
		sb.WriteByte('\n')
	}
	if rec.Parent != nil {
		fmt.Fprintf(&sb, "Parent: %s  %q  (%s)\n", rec.Parent.Key, rec.Parent.Summary, rec.Parent.Status)
		sb.WriteByte('\n')
	}

	// Fix versions & Labels
	if len(rec.FixVersions) > 0 {
		fmt.Fprintf(&sb, "%s %s\n", sectionLabel("Fix Versions:"), strings.Join(rec.FixVersions, ", "))
	}
	if len(rec.Labels) > 0 {
		fmt.Fprintf(&sb, "%s %s\n", sectionLabel("Labels:"), strings.Join(rec.Labels, ", "))
	}
	if len(rec.FixVersions) > 0 || len(rec.Labels) > 0 {
		sb.WriteByte('\n')
	}

	// Description
	if rec.Description != "" {
		sb.WriteString(sectionLabel("Description:") + "\n")
		rendered := jira.RenderWikiMarkup(rec.Description)
		for _, line := range strings.Split(rendered, "\n") {
			fmt.Fprintf(&sb, "  %s\n", line)
		}
		sb.WriteByte('\n')
	}

	// Links — status column colored; use visible width for alignment
	if len(rec.Links) > 0 {
		fmt.Fprintf(&sb, "%s\n", sectionLabel(fmt.Sprintf("Links (%d):", len(rec.Links))))
		for _, l := range rec.Links {
			id := ""
			if l.ID != "" {
				id = fmt.Sprintf("(id: %s)", l.ID)
			}
			coloredStatus := colorStatusName(l.Issue.Status)
			// Pad the status column by visible width (strip ANSI for measurement).
			statusVis := len([]rune(jira.TruncateString(l.Issue.Status, 14)))
			statusPadded := coloredStatus + strings.Repeat(" ", 14-statusVis)
			linkTypeBadge := colorIssueType(l.Issue.IssueType)
			prefix := ""
			if linkTypeBadge != "" {
				prefix = linkTypeBadge + " "
			}
			summary := jira.TruncateString(l.Issue.Summary, 69)
			summaryVis := len([]rune(summary))
			summaryPadded := summary + strings.Repeat(" ", 70-summaryVis)
			fmt.Fprintf(&sb, "  %-18s  %s%-16s  %s  %s  %s\n",
				jira.TruncateString(l.Relationship, 18),
				prefix,
				jira.TruncateString(l.Issue.Key, 16),
				statusPadded,
				summaryPadded,
				id)
		}
		fmt.Fprintf(&sb, "  → jiracli delete %s:link:<id>\n", rec.Key)
		fmt.Fprintf(&sb, "  → jiracli add link %s OTHER-123 --type \"is related to\"\n", rec.Key)
		sb.WriteByte('\n')
	}

	// Components & Resolution
	if len(rec.Components) > 0 {
		fmt.Fprintf(&sb, "Components: %s\n", strings.Join(rec.Components, ", "))
	}
	if rec.Resolution != nil {
		fmt.Fprintf(&sb, "Resolution: %s\n", *rec.Resolution)
	}
	if len(rec.Components) > 0 || rec.Resolution != nil {
		sb.WriteByte('\n')
	}

	// Attachments
	if len(rec.Attachments) > 0 {
		fmt.Fprintf(&sb, "%s\n", sectionLabel(fmt.Sprintf("Attachments (%d):", len(rec.Attachments))))
		for i, a := range rec.Attachments {
			fmt.Fprintf(&sb, "  [%d] %s  %s  %s  (id: %s:attach:%s)\n",
				i+1, a.Filename, jira.FormatBytes(a.Size), parseISODate(a.Uploaded), rec.Key, a.ID)
		}
		last := rec.Attachments[len(rec.Attachments)-1]
		fmt.Fprintf(&sb, "  → jiracli show %s:attach:%s\n", rec.Key, last.ID)
		sb.WriteByte('\n')
	}

	// Children — type badge + status badge per row, Done-last, cap 15
	const childrenDisplayLimit = 15
	if !flags.NoChildren {
		if rec.ChildrenError != "" {
			fmt.Fprintf(&sb, "Children: (could not fetch — %s)\n\n", rec.ChildrenError)
		} else if len(rec.Children) > 0 {
			sorted := make([]jira.ChildIssueRecord, len(rec.Children))
			copy(sorted, rec.Children)
			sort.SliceStable(sorted, func(i, j int) bool {
				iDone := strings.EqualFold(sorted[i].StatusCategory, "Done")
				jDone := strings.EqualFold(sorted[j].StatusCategory, "Done")
				return !iDone && jDone
			})
			display := sorted
			truncated := false
			if len(sorted) > childrenDisplayLimit {
				display = sorted[:childrenDisplayLimit]
				truncated = true
			}
			heading := fmt.Sprintf("Children (%d of %d", len(display), rec.ChildrenTotal)
			if truncated {
				heading += " shown"
			}
			heading += "):"
			sb.WriteString(sectionLabel(heading) + "\n")
			for _, ch := range display {
				assignee := ch.Assignee
				if assignee == "" {
					assignee = "__Unassigned"
				}
				chTypeBadge := colorIssueType(ch.IssueType)
				chStatus := colorStatusName(ch.Status)
				// Align: key 12, status 14 visible, assignee 20, summary quoted
				statusVis := len([]rune(jira.TruncateString(ch.Status, 14)))
				statusPadded := chStatus + strings.Repeat(" ", 14-statusVis)
				fmt.Fprintf(&sb, "  %s  %-12s  %s  %-20s  %q\n",
					chTypeBadge,
					jira.TruncateString(ch.Key, 12),
					statusPadded,
					jira.TruncateString(assignee, 20),
					ch.Summary)
			}
			childJQL := `parent = ` + rec.Key
			if strings.EqualFold(rec.IssueType, "Epic") {
				childJQL = `"Epic Link" = ` + rec.Key
			}
			fmt.Fprintf(&sb, "  → jiracli search %q\n", childJQL)
			sb.WriteByte('\n')
		} else {
			sb.WriteString("Children: (none)\n\n")
		}
	}

	// Comments
	if !flags.NoComments {
		renderComments(&sb, rec)
	}

	// Activity / changelog — show newest 10 entries; color status transitions.
	if !flags.NoHistory && len(rec.ActivityTimeline) > 0 {
		timeline := rec.ActivityTimeline
		const maxActivity = 10
		hiddenOlder := 0
		if len(timeline) > maxActivity {
			hiddenOlder = len(timeline) - maxActivity
			timeline = timeline[len(timeline)-maxActivity:]
		}
		sb.WriteString(sectionLabel("Activity (newest 10):") + "\n")
		for _, act := range timeline {
			t := parseISODateTime(act.Created)
			dateStr := "—"
			if !t.IsZero() {
				dateStr = t.Format("2006-01-02 15:04")
			}
			changeStrs := make([]string, 0, len(act.Changes))
			for _, ch := range act.Changes {
				statusMarker := ""
				if ch.Field == "status" {
					fromRank := jira.StatusCategoryRank(ch.FromCategory)
					toRank := jira.StatusCategoryRank(ch.ToCategory)
					if toRank < fromRank {
						statusMarker = " ↩"
					}
				}
				changeStrs = append(changeStrs, jira.AbbreviateChange(ch.Field, ch.From, ch.To, statusMarker))
			}
			fmt.Fprintf(&sb, "  %s  %-20s  %s\n",
				dateStr,
				act.Author.DisplayName,
				strings.Join(changeStrs, ", "))
		}
		totalShown := len(timeline)
		totalEntries := rec.HistoryTotal
		if hiddenOlder > 0 || rec.HistoryTruncated {
			fmt.Fprintf(&sb, "  (showing %d newest of %d entries — jiracli show history %s for full log)\n",
				totalShown, totalEntries, rec.Key)
		}
		sb.WriteByte('\n')
	}

	// Drill-in footer
	sb.WriteString(sectionLabel("Drill in:") + "\n")
	fmt.Fprintf(&sb, "  → jiracli show comments     %s\n", rec.Key)
	fmt.Fprintf(&sb, "  → jiracli show history      %s\n", rec.Key)
	fmt.Fprintf(&sb, "  → jiracli show transitions  %s\n", rec.Key)

	return sb.String()
}

// renderComments writes the comments section to sb.
func renderComments(sb *strings.Builder, rec jira.IssueRecord) {
	c := rec.Comments
	if c.Total == 0 {
		return
	}
	if len(c.Items) == 0 {
		return
	}

	previewCount := len(c.Items)
	fmt.Fprintf(sb, "%s\n", sectionLabel(fmt.Sprintf("Latest comment (%d of %d):", previewCount, c.Total)))

	for _, item := range c.Items {
		dateStr := parseISODate(item.Created)
		fmt.Fprintf(sb, "  — %s (%s)  %s\n", item.Author.DisplayName, item.Author.Name, dateStr)
		// Wrap body at 74 cols (78 - 4 for "> ") with 4-space indent
		body := strings.TrimSpace(item.Body)
		if body != "" {
			wrapped := jira.WrapAt(body, 74, 4)
			for _, line := range strings.Split(wrapped, "\n") {
				fmt.Fprintf(sb, "  > %s\n", line)
			}
		}
	}

	if c.Total > 1 && c.Total <= 50 {
		fmt.Fprintf(sb, "  → jiracli show comments %s          # full thread (%d comments)\n", rec.Key, c.Total)
	} else if c.Total > 50 {
		fmt.Fprintf(sb, "  → 50+ comments — use jiracli show comments %s --page 1\n", rec.Key)
	}
	sb.WriteByte('\n')
}
