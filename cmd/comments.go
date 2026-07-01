package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// CommentsFlags holds parsed flag values for the comments command.
type CommentsFlags struct {
	Profile string
	JSON    bool
	Since   string
	Limit   int
	Page    int
}

// NewCommentsCmd builds the "comments" command.
func NewCommentsCmd() *cobra.Command {
	var flags CommentsFlags
	c := &cobra.Command{
		Use:   "comments <KEY>",
		Short: "List comments on a Jira issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Comments(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().StringVar(&flags.Since, "since", "", "Filter to comments on or after this date (RFC3339, YYYY-MM-DD, or relative like 7d/24h)")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max comments per page (1-200)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

// commentRecord is the NDJSON schema for one comment.
type commentRecord struct {
	ID     string `json:"id"`
	Author struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"author"`
	Created string `json:"created"`
	Updated string `json:"updated"`
	Body    string `json:"body"`
}

// paginationRecord is the NDJSON trailer emitted when more pages exist.
type paginationRecord struct {
	Pagination struct {
		StartAt    int `json:"startAt"`
		MaxResults int `json:"maxResults"`
		Total      int `json:"total"`
		NextPage   int `json:"nextPage,omitempty"`
	} `json:"_pagination"`
}

// parseSince parses a --since value as RFC3339, a plain date (YYYY-MM-DD treated
// as midnight UTC), or a relative duration like "7d" or "24h".
// Returns the zero time if the input is empty.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try plain date YYYY-MM-DD (midnight UTC).
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	// Try relative: ends in d (days) or h (hours).
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid --since value %q — use RFC3339 (e.g. 2026-01-01T00:00:00Z), a date (e.g. 2026-01-01), or relative (e.g. 7d, 24h)", s)
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("invalid --since value %q — use RFC3339 (e.g. 2026-01-01T00:00:00Z), a date (e.g. 2026-01-01), or relative (e.g. 7d, 24h)", s)
	}
	switch unit {
	case 'd':
		return time.Now().Add(-time.Duration(n) * 24 * time.Hour), nil
	case 'h':
		return time.Now().Add(-time.Duration(n) * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("invalid --since value %q — use RFC3339 (e.g. 2026-01-01T00:00:00Z), a date (e.g. 2026-01-01), or relative (e.g. 7d, 24h)", s)
	}
}

// parseJiraTime parses Jira's timestamp format (ISO 8601 with tz offset).
func parseJiraTime(s string) (time.Time, error) {
	// Jira returns e.g. "2026-06-24T10:30:00.000+0200"
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// formatJiraTimestamp formats a Jira timestamp as "YYYY-MM-DD HH:MM".
func formatJiraTimestamp(s string) string {
	t, err := parseJiraTime(s)
	if err != nil {
		return s
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// Comments is the Layer 1 implementation for the comments command.
func Comments(ctx context.Context, flags CommentsFlags, key string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	limit := flags.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	page := flags.Page
	if page < 1 {
		page = 1
	}

	sinceTime, err := parseSince(flags.Since)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	resp, err := client.GetComments(ctx, key, page, limit)
	if err != nil {
		return "", fmt.Errorf("fetching comments for %s: %w", key, err)
	}

	// Client-side since filtering.
	comments := resp.Comments
	if !sinceTime.IsZero() {
		filtered := comments[:0]
		for _, c := range comments {
			t, err := parseJiraTime(c.Created)
			if err != nil || !t.Before(sinceTime) {
				filtered = append(filtered, c)
			}
		}
		comments = filtered
	}

	startAt := resp.StartAt
	total := resp.Total
	hasMore := total > startAt+len(resp.Comments)
	nextPage := page + 1
	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	if totalPages < 1 {
		totalPages = 1
	}

	if flags.JSON {
		var sb strings.Builder
		for _, c := range comments {
			rec := commentRecord{}
			rec.ID = c.ID
			rec.Author.Name = c.Author.Name
			rec.Author.DisplayName = c.Author.DisplayName
			rec.Created = c.Created
			rec.Updated = c.Updated
			rec.Body = c.Body
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		if hasMore {
			trailer := paginationRecord{}
			trailer.Pagination.StartAt = startAt
			trailer.Pagination.MaxResults = limit
			trailer.Pagination.Total = total
			trailer.Pagination.NextPage = nextPage
			data, _ := json.Marshal(trailer)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	// Plain text.
	// Friendly empty-state, consistent with "show attachments".
	if len(comments) == 0 {
		if total == 0 {
			return fmt.Sprintf("%s has no comments.\n", key), nil
		}
		if flags.Since != "" {
			return fmt.Sprintf("%s has no comments on or after %s.\n", key, flags.Since), nil
		}
		// Non-empty thread but this page is past the end — fall through to the
		// pagination footer below so the page numbers are still shown.
	}

	var sb strings.Builder
	for i, c := range comments {
		n := startAt + i + 1
		ts := formatJiraTimestamp(c.Created)
		fmt.Fprintf(&sb, "[%d] %s  %s  %s (%s)\n", n, c.ID, ts, c.Author.DisplayName, c.Author.Name)
		// Body: word-wrap at 80 cols, 6-space indent on continuation lines.
		// Each wrapped line is prefixed with "    > ".
		const indent = 6 // "    > " = 6 chars
		const width = 80
		body := jira.WrapAt(c.Body, width, indent)
		for _, line := range strings.Split(body, "\n") {
			fmt.Fprintf(&sb, "    > %s\n", line)
		}
	}

	// Pagination footer.
	if hasMore {
		fmt.Fprintf(&sb, "--- page %d of %d | next: jiracli show comments --page %d --limit %d %s ---\n",
			page, totalPages, nextPage, limit, key)
	} else {
		fmt.Fprintf(&sb, "--- page %d of %d ---\n", page, totalPages)
	}

	return sb.String(), nil
}

// ShowCommentFlags holds flags for showing a single comment.
type ShowCommentFlags struct {
	Profile string
	JSON    bool
}

// ShowComment fetches and renders a single comment by ref (KEY:comment:ID).
func ShowComment(ctx context.Context, flags ShowCommentFlags, ref string) (string, error) {
	parsed, err := jira.ParseRef(ref)
	if err != nil || parsed.Kind != jira.RefComment {
		return "", fmt.Errorf("ref must be KEY:comment:ID form — e.g. jiracli show ACME-123:comment:9421")
	}

	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	client := jira.New(entry)

	c, err := client.GetComment(ctx, parsed.Key, parsed.CommentID)
	if err != nil {
		return "", fmt.Errorf("fetch comment %s on %s: %w", parsed.CommentID, parsed.Key, err)
	}

	if flags.JSON {
		rec := commentRecord{}
		rec.ID = c.ID
		rec.Author.Name = c.Author.Name
		rec.Author.DisplayName = c.Author.DisplayName
		rec.Created = c.Created
		rec.Updated = c.Updated
		rec.Body = c.Body
		data, _ := json.Marshal(rec)
		return string(data) + "\n", nil
	}

	// Plain text.
	ts := formatJiraTimestamp(c.Created)
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s:comment:%s  %s  %s (%s)\n", parsed.Key, c.ID, ts, c.Author.DisplayName, c.Author.Name)
	if c.Updated != c.Created {
		fmt.Fprintf(&sb, "  (edited %s)\n", formatJiraTimestamp(c.Updated))
	}
	sb.WriteByte('\n')
	const indent = 6
	const width = 80
	body := jira.WrapAt(c.Body, width, indent)
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(&sb, "    > %s\n", line)
	}
	fmt.Fprintf(&sb, "\n→ jiracli show comments %s  # full thread\n", parsed.Key)
	return sb.String(), nil
}
