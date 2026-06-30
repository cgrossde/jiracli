package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupBoardsFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
	Type    string // "" | "scrum" | "kanban"
	Limit   int
	Page    int
}

func newLookupBoardsCmd() *cobra.Command {
	var flags lookupBoardsFlags
	c := &cobra.Command{
		Use:   "boards",
		Short: "List Jira Agile boards",
		Long: `List boards for a project. Use --type to filter by scrum or kanban.

Examples:
  jiracli lookup boards --project ACME
  jiracli lookup boards --project ACME --type scrum
  jiracli lookup boards --project ACME --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := lookupBoards(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Skip cache")
	c.Flags().StringVar(&flags.Project, "project", "", "Project key (required)")
	c.Flags().StringVar(&flags.Type, "type", "", "Filter by type: scrum or kanban")
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max results per page (1-100)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

func lookupBoards(ctx context.Context, flags lookupBoardsFlags) (string, error) {
	if flags.Project == "" {
		return "", fmt.Errorf("--project required — run: jiracli lookup projects")
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	store := cache.NewStore(entry)
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

	boards, total, err := client.ListBoardsCached(ctx, flags.Project, page, limit, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("listing boards: %w", err)
	}

	// Filter by type client-side.
	var matched []jira.Board
	for _, b := range boards {
		if flags.Type == "" || strings.EqualFold(b.Type, flags.Type) {
			matched = append(matched, b)
		}
	}

	if flags.JSON {
		var sb strings.Builder
		for _, b := range matched {
			rec := struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			}{b.ID, b.Name, b.Type}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		hasMore := total > page*limit
		if hasMore {
			type paginationBlock struct {
				Page     int  `json:"page"`
				Pages    int  `json:"pages"`
				Total    int  `json:"total"`
				NextPage int  `json:"next_page"`
				HasMore  bool `json:"has_more"`
			}
			trailer := struct {
				Pagination paginationBlock `json:"_pagination"`
			}{
				Pagination: paginationBlock{
					Page:     page,
					Pages:    -1,
					Total:    -1,
					NextPage: page + 1,
					HasMore:  true,
				},
			}
			data, _ := json.Marshal(trailer)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, b := range matched {
		fmt.Fprintf(&sb, "  %-8d  %-40s  %s\n", b.ID, b.Name, b.Type)
	}
	if len(matched) == 0 {
		sb.WriteString("no boards found\n")
	} else {
		sb.WriteByte('\n')
		// Drill-down hints using first scrum/kanban board.
		var firstScrum, firstKanban int
		for _, b := range matched {
			if firstScrum == 0 && strings.EqualFold(b.Type, "scrum") {
				firstScrum = b.ID
			}
			if firstKanban == 0 && strings.EqualFold(b.Type, "kanban") {
				firstKanban = b.ID
			}
		}
		if firstScrum > 0 {
			fmt.Fprintf(&sb, "→ jiracli sprint current --board %d\n", firstScrum)
		}
		if firstKanban > 0 {
			fmt.Fprintf(&sb, "→ jiracli board issues %d\n", firstKanban)
		}
		if firstScrum > 0 {
			fmt.Fprintf(&sb, "→ jiracli sprint list --board %d\n", firstScrum)
		}
	}
	if total > page*limit {
		fmt.Fprintf(&sb, "--- page %d | next: jiracli lookup boards --project %s --page %d --limit %d ---\n",
			page, flags.Project, page+1, limit)
	}
	return sb.String(), nil
}
