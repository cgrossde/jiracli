package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

// NewBoardCmd builds the "board" command group.
func NewBoardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "board",
		Short: "Inspect Jira Agile boards (scrum and kanban)",
		Long: `Boards represent Scrum or Kanban work surfaces. Use "list" to find board IDs
and "issues" / "show" to inspect them. For sprints, see: jiracli sprint`,
	}
	c.AddCommand(NewBoardListCmd(), NewBoardShowCmd(), NewBoardIssuesCmd())
	return c
}

type boardShowFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Project string
	Details bool
}

type boardIssuesFlags struct {
	Profile  string
	JSON     bool
	KeysOnly bool
	Limit    int
	Page     int
}

// NewBoardListCmd builds "board list" — pure alias of "lookup boards".
func NewBoardListCmd() *cobra.Command {
	var flags lookupBoardsFlags
	c := &cobra.Command{
		Use:   "list",
		Short: "List boards (alias of: lookup boards)",
		Long:  `List Agile boards for a project. Identical to: jiracli lookup boards`,
		Args:  cobra.NoArgs,
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

// NewBoardShowCmd builds "board show <id-or-name>".
func NewBoardShowCmd() *cobra.Command {
	var flags boardShowFlags
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Show board configuration (columns, type, filter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := boardShow(cmd.Context(), flags, args[0])
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
	c.Flags().StringVar(&flags.Project, "project", "", "Project key (for name resolution)")
	c.Flags().BoolVar(&flags.Details, "details", false, "Fetch filter details: owner, JQL")
	return c
}

// NewBoardIssuesCmd builds "board issues <id>".
func NewBoardIssuesCmd() *cobra.Command {
	var flags boardIssuesFlags
	c := &cobra.Command{
		Use:   "issues <id>",
		Short: "List issues on a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := boardIssues(cmd.Context(), flags, args[0])
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
	c.Flags().IntVar(&flags.Limit, "limit", 50, "Max results per page (1-100)")
	c.Flags().IntVar(&flags.Page, "page", 1, "Page number (1-indexed)")
	return c
}

func boardShow(ctx context.Context, flags boardShowFlags, idOrName string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	boardID, atoiErr := strconv.Atoi(idOrName)
	if atoiErr != nil {
		// Name resolution requires --project.
		if flags.Project == "" {
			return "", fmt.Errorf("board name resolution requires --project <KEY>; or use the numeric board id")
		}
		boards, _, listErr := client.ListBoardsCached(ctx, flags.Project, 1, 100, store, flags.NoCache)
		if listErr != nil {
			return "", fmt.Errorf("listing boards for name resolution: %w", listErr)
		}
		lower := strings.ToLower(idOrName)
		var matches []jira.Board
		for _, b := range boards {
			if strings.EqualFold(b.Name, idOrName) || strings.Contains(strings.ToLower(b.Name), lower) {
				matches = append(matches, b)
			}
		}
		switch len(matches) {
		case 0:
			return "", fmt.Errorf("no board named %q found in project %s", idOrName, flags.Project)
		case 1:
			boardID = matches[0].ID
		default:
			var sb strings.Builder
			fmt.Fprintf(&sb, "ambiguous board name %q — candidates:\n", idOrName)
			for _, b := range matches {
				fmt.Fprintf(&sb, "  jiracli board show %d  # %s\n", b.ID, b.Name)
			}
			return "", fmt.Errorf("%s", sb.String())
		}
	}

	cfg, err := client.GetBoardConfigCached(ctx, boardID, store, flags.NoCache)
	if err != nil {
		// GetBoardConfig's error already carries "board config: ..." context.
		return "", err
	}

	if flags.JSON {
		var out struct {
			jira.BoardConfig
			Filter *jira.BoardFilter `json:"filter,omitempty"`
		}
		out.BoardConfig = cfg
		if flags.Details && cfg.FilterID != "" {
			if f, fErr := client.GetBoardFilter(ctx, cfg.FilterID); fErr == nil {
				out.Filter = &f
			}
		}
		data, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("marshal board config: %w", err)
		}
		return string(data) + "\n", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Board %d  %s  %s\n\n", cfg.ID, cfg.Name, cfg.Type)
	if flags.Details && cfg.FilterID != "" {
		f, fErr := client.GetBoardFilter(ctx, cfg.FilterID)
		if fErr != nil {
			fmt.Fprintf(&sb, "Filter: (error: %v)\n\n", fErr)
		} else {
			ownerStatus := ""
			if !f.OwnerActive {
				ownerStatus = "  [inactive]"
			}
			fmt.Fprintf(&sb, "Filter:  %s (id: %s)\n", f.Name, f.ID)
			fmt.Fprintf(&sb, "Owner:   %s%s\n", f.OwnerName, ownerStatus)
			fmt.Fprintf(&sb, "JQL:     %s\n", f.JQL)
			sb.WriteByte('\n')
		}
	}
	if len(cfg.Columns) > 0 {
		// Build id→name map from cached status list (7-day TTL — cheap).
		statusNames := make(map[string]string)
		if statuses, sErr := client.ListStatuses(ctx, store, false); sErr == nil {
			for _, s := range statuses {
				statusNames[s.ID] = s.Name
			}
		}
		sb.WriteString("Columns:\n")
		for _, col := range cfg.Columns {
			if len(col.StatusIDs) > 0 {
				parts := make([]string, 0, len(col.StatusIDs))
				for _, id := range col.StatusIDs {
					if name, ok := statusNames[id]; ok {
						parts = append(parts, name+" (id: "+id+")")
					} else {
						parts = append(parts, id)
					}
				}
				fmt.Fprintf(&sb, "  %-20s  %s\n", col.Name, strings.Join(parts, ", "))
			} else {
				fmt.Fprintf(&sb, "  %s\n", col.Name)
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("Drill in:\n")
	fmt.Fprintf(&sb, "  → jiracli board issues %d\n", cfg.ID)
	if !strings.EqualFold(cfg.Type, "kanban") {
		fmt.Fprintf(&sb, "  → jiracli sprint list --board %d\n", cfg.ID)
	}
	if !flags.Details && cfg.FilterID != "" {
		fmt.Fprintf(&sb, "  → jiracli board show %d --details  # filter owner & JQL\n", cfg.ID)
	}
	return sb.String(), nil
}

func boardIssues(ctx context.Context, flags boardIssuesFlags, idStr string) (string, error) {
	boardID, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("board id must be numeric, got %q", idStr)
	}
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

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

	// Get board name for header (cached).
	boardName := fmt.Sprintf("%d", boardID)
	if cfg, cfgErr := client.GetBoardConfigCached(ctx, boardID, store, false); cfgErr == nil {
		boardName = cfg.Name
	}

	fields := defaultSearchFields
	resp, err := client.ListBoardIssues(ctx, boardID, page, limit, fields)
	if err != nil {
		return "", fmt.Errorf("board issues: %w", err)
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

	header := fmt.Sprintf("board: %d  %s", boardID, boardName)
	sf := SearchFlags{Limit: limit, Page: page, PageCmdBase: fmt.Sprintf("jiracli board issues %d", boardID)}
	return renderSearchPlain(resp, header, "", page, limit, sf, fields, entry.Hierarchy.StoryPointsField, entry.Agile.SprintField)
}
