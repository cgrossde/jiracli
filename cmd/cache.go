package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewCacheCmd builds the cache command group.
func NewCacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Inspect and clear the local metadata cache",
	}
	c.AddCommand(newCacheListCmd(), newCacheClearCmd())
	return c
}

// ── cache list ────────────────────────────────────────────────────────────────

type cacheListFlags struct {
	Profile string
	JSON    bool
}

func newCacheListCmd() *cobra.Command {
	var flags cacheListFlags
	c := &cobra.Command{
		Use:   "list",
		Short: "List cached metadata entries for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := cacheList(flags)
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

func cacheList(flags cacheListFlags) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}

	entries, err := store.List()
	if err != nil {
		return "", fmt.Errorf("listing cache: %w", err)
	}

	if len(entries) == 0 {
		profile := entry.Profile
		if profile == "" {
			profile = "default"
		}
		return fmt.Sprintf("no cached entries for profile %s\n", profile), nil
	}

	now := time.Now()

	if flags.JSON {
		var sb strings.Builder
		for _, e := range entries {
			rec := struct {
				Key       string `json:"key"`
				SavedAt   string `json:"savedAt"`
				TTL       string `json:"ttl,omitempty"`
				ExpiresAt string `json:"expiresAt,omitempty"`
				Expired   *bool  `json:"expired,omitempty"`
			}{Key: e.Key, SavedAt: e.SavedAt.UTC().Format(time.RFC3339)}
			if ttl, ok := cacheKeyTTL(e.Key); ok {
				expiresAt := e.SavedAt.Add(ttl)
				expired := now.After(expiresAt)
				rec.TTL = ttl.String()
				rec.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
				rec.Expired = &expired
			}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, e := range entries {
		age := jira.FormatRelative(e.SavedAt, now)
		line := fmt.Sprintf("%-40s  saved %s ago", e.Key, age)
		if ttl, ok := cacheKeyTTL(e.Key); ok {
			expiresAt := e.SavedAt.Add(ttl)
			if now.After(expiresAt) {
				line += fmt.Sprintf("    ttl %-10s    expired %s ago", ttl, jira.FormatRelative(expiresAt, now))
			} else {
				line += fmt.Sprintf("    ttl %-10s    expires in %s", ttl, expiresAt.Sub(now).Round(time.Second))
			}
		}
		sb.WriteString(line + "\n")
	}
	return sb.String(), nil
}

// cacheKeyTTL returns the TTL governing a cache key, mirroring the exact key
// patterns used by the internal/jira cache call sites. Order matters: more
// specific patterns (e.g. the "/priorityscheme" suffix under "project/") are
// checked before the more general prefix they'd otherwise also match.
// Returns (0, false) for a key with no known TTL mapping (e.g. a stale key
// left by a since-removed cache call site).
func cacheKeyTTL(key string) (time.Duration, bool) {
	switch key {
	case "fields":
		return jira.TTLFields, true
	case "projects":
		return jira.TTLProjects, true
	case "statuses":
		return jira.TTLStatuses, true
	case "issuetypes":
		return jira.TTLIssueTypes, true
	case "priorities":
		return jira.TTLPriorities, true
	case "linktypes":
		return jira.TTLLinkTypes, true
	case "myself":
		return jira.TTLMyself, true
	}
	switch {
	case strings.HasPrefix(key, "issue-summary/"):
		return jira.TTLIssueSummary, true
	case strings.HasPrefix(key, "issuetypes/"):
		return jira.TTLIssueTypes, true
	case strings.HasPrefix(key, "createmeta/"):
		return jira.TTLCreateMeta, true
	case strings.HasSuffix(key, "/priorityscheme"):
		return jira.TTLPrioritySchm, true
	case strings.HasPrefix(key, "project/"):
		return jira.TTLProject, true
	case strings.HasPrefix(key, "labels/"):
		return jira.TTLLabels, true
	case strings.HasPrefix(key, "boards/"):
		return jira.TTLBoards, true
	case strings.HasPrefix(key, "board/") && strings.HasSuffix(key, "/config"):
		return jira.TTLBoardConfig, true
	case strings.HasPrefix(key, "sprints/") && strings.HasSuffix(key, "/archive"):
		return jira.TTLSprintArchive, true
	case strings.HasPrefix(key, "sprints/") && strings.HasSuffix(key, "/active+future"):
		return jira.TTLSprintsActive, true
	case strings.HasPrefix(key, "sprints/") && strings.HasSuffix(key, "/names"):
		return jira.TTLSprintsActive, true
	case strings.HasPrefix(key, "sprints/") && strings.Contains(key, "/default-"):
		return jira.TTLSprintsActive, true
	case strings.HasPrefix(key, "sprints/") && (strings.HasSuffix(key, "/closed") || strings.HasSuffix(key, "/closed-all")):
		return jira.TTLSprintsClosed, true
	}
	return 0, false
}

// ── cache clear ───────────────────────────────────────────────────────────────

type cacheClearFlags struct {
	Profile string
	Key     string
	Yes     bool
}

func newCacheClearCmd() *cobra.Command {
	var flags cacheClearFlags
	c := &cobra.Command{
		Use:   "clear",
		Short: "Delete cached metadata entries for the active profile",
		Long: `Delete cached metadata entries for the active profile.

Use --key with a glob pattern to delete specific entries (e.g. 'project/*').
Omit --key to delete all entries.

See also:
  jiracli cache list    show cached keys and their TTLs`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := cacheClear(flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().StringVar(&flags.Key, "key", "", "Glob pattern of keys to delete (e.g. 'project/*'); omit to delete all")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Skip confirmation prompt")
	return c
}

func cacheClear(flags cacheClearFlags) (string, error) {
	_, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}

	entries, err := store.List()
	if err != nil {
		return "", fmt.Errorf("listing cache: %w", err)
	}

	// Count how many entries match the pattern.
	pattern := flags.Key
	if pattern == "" {
		pattern = "*"
	}
	var count int
	for _, e := range entries {
		matched, merr := filepath.Match(pattern, e.Key)
		if merr != nil {
			return "", fmt.Errorf("invalid key pattern %q: %w", pattern, merr)
		}
		if matched {
			count++
		}
	}

	if count == 0 {
		return "no cached entries match the pattern\n", nil
	}

	// TTY confirmation unless --yes was passed.
	if !flags.Yes {
		apply, ttyAvail := promptCacheDelete(count)
		if !ttyAvail {
			// Non-interactive: print what would be deleted and exit cleanly.
			return fmt.Sprintf("would delete %d cached %s — re-run with --yes to apply\n",
				count, pluralEntries(count)), nil
		}
		if !apply {
			return "aborted\n", nil
		}
	}

	if err := store.DeleteGlob(pattern); err != nil {
		return "", fmt.Errorf("deleting cache entries: %w", err)
	}

	return fmt.Sprintf("✓ deleted %d cached %s\n", count, pluralEntries(count)), nil
}

func pluralEntries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// promptCacheDelete opens /dev/tty and asks the user whether to delete N entries.
// Returns (decision, ttyAvailable). If /dev/tty cannot be opened (CI/container),
// returns (false, false) so the caller can handle the non-interactive path.
func promptCacheDelete(n int) (apply bool, ttyAvailable bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, false
	}
	defer tty.Close()
	fmt.Fprintf(tty, "Delete %d cached %s? [y/N]: ", n, pluralEntries(n))
	buf := make([]byte, 128)
	nr, _ := tty.Read(buf)
	line := strings.TrimSpace(string(buf[:nr]))
	return strings.EqualFold(line, "y") || strings.EqualFold(line, "yes"), true
}
