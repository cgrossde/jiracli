package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// HistoryFlags holds parsed flag values for the history command.
type HistoryFlags struct {
	Profile     string
	JSON        bool
	IncludeRank bool
	Since       string
	Limit       int
	Page        int
}

// NewHistoryCmd builds the "history" command.
func NewHistoryCmd() *cobra.Command {
	var flags HistoryFlags
	c := &cobra.Command{
		Use:   "history <KEY>",
		Short: "Show changelog history for a Jira issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := History(cmd.Context(), flags, args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.IncludeRank, "include-rank", false, "Include Rank_ field changes (hidden by default)")
	c.Flags().StringVar(&flags.Since, "since", "", "Filter to entries on or after this date (RFC3339, YYYY-MM-DD, or relative like 7d/24h)")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max history entries per page")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

// historyChangeRecord is one field-change item within a history NDJSON record.
type historyChangeRecord struct {
	Field        string `json:"field"`
	From         string `json:"from"`
	To           string `json:"to"`
	FromCategory string `json:"fromCategory"`
	ToCategory   string `json:"toCategory"`
}

// historyRecord is the NDJSON schema for one changelog entry.
type historyRecord struct {
	ID     string `json:"id"`
	Author struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"author"`
	Created string                `json:"created"`
	Changes []historyChangeRecord `json:"changes"`
}

// isRankField reports whether a field name is a Jira rank pseudo-field.
func isRankField(name string) bool {
	return strings.HasPrefix(name, "Rank_") || name == "Rank"
}

// History is the Layer 1 implementation for the history command.
func History(ctx context.Context, flags HistoryFlags, key string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}

	limit := flags.Limit
	if limit <= 0 {
		limit = 50
	}
	page := flags.Page
	if page < 1 {
		page = 1
	}

	client := jira.New(entry)
	store := cache.NewStore(entry)

	// Build status-name → category map for fromCategory/toCategory population.
	statusMap := make(map[string]string)
	if statuses, err := client.ListStatuses(ctx, store, false); err == nil {
		for _, s := range statuses {
			statusMap[s.Name] = s.StatusCategory.Name
		}
	}

	resp, wasTruncated, err := client.GetChangelog(ctx, key, page, limit)
	if err != nil {
		return "", fmt.Errorf("fetching history for %s: %w", key, err)
	}

	// Use Values (dedicated endpoint); fallback sets Values too via getChangelogFallback.
	entries := resp.Values

	// --since: parse cutoff and filter entries.
	if flags.Since != "" {
		cutoff, serr := parseSince(flags.Since)
		if serr != nil {
			return "", serr
		}
		if !cutoff.IsZero() {
			filtered := entries[:0]
			for _, e := range entries {
				t, terr := parseJiraTime(e.Created)
				if terr != nil || !t.Before(cutoff) {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}
	}

	startAt := resp.StartAt
	total := resp.Total
	hasMore := total > startAt+len(resp.Values)
	nextPage := page + 1
	totalPages := int(math.Ceil(float64(total) / float64(limit)))
	if totalPages < 1 {
		totalPages = 1
	}

	if flags.JSON {
		var sb strings.Builder
		for _, e := range entries {
			// Filter rank entries unless --include-rank.
			var changes []historyChangeRecord
			for _, item := range e.Items {
				if !flags.IncludeRank && isRankField(item.Field) {
					continue
				}
				cr := historyChangeRecord{
					Field: item.Field,
					From:  item.FromString,
					To:    item.ToString,
				}
				if item.Field == "status" {
					cr.FromCategory = statusMap[item.FromString]
					cr.ToCategory = statusMap[item.ToString]
				}
				changes = append(changes, cr)
			}
			if len(changes) == 0 {
				continue
			}
			rec := historyRecord{}
			rec.ID = e.ID
			rec.Author.Name = e.Author.Name
			rec.Author.DisplayName = e.Author.DisplayName
			rec.Created = e.Created
			rec.Changes = changes
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
		if wasTruncated {
			sb.WriteString("// note: older DC — changelog limited to server max; use --limit to page\n")
		}
		return sb.String(), nil
	}

	// Plain text.
	var sb strings.Builder
	for _, e := range entries {
		// Collect non-rank items.
		var parts []string
		for _, item := range e.Items {
			if !flags.IncludeRank && isRankField(item.Field) {
				continue
			}
			parts = append(parts, jira.AbbreviateChange(item.Field, item.FromString, item.ToString, ""))
		}
		if len(parts) == 0 {
			continue
		}
		ts := formatJiraTimestamp(e.Created)
		fmt.Fprintf(&sb, "%s  %s  %s\n", ts, e.Author.DisplayName, strings.Join(parts, ", "))
	}

	// Pagination footer.
	if hasMore {
		fmt.Fprintf(&sb, "--- page %d of %d | next: jiracli show history --page %d --limit %d %s ---\n",
			page, totalPages, nextPage, limit, key)
	} else {
		fmt.Fprintf(&sb, "--- page %d of %d ---\n", page, totalPages)
	}

	if wasTruncated {
		sb.WriteString("(note: older DC — changelog limited to server max; use --limit to page)\n")
	}

	return sb.String(), nil
}
