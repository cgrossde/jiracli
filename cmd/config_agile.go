package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// ConfigAgileFlags holds parsed flag values for `config agile`.
type ConfigAgileFlags struct {
	Profile    string
	JSON       bool
	Rediscover bool
	Field      string // explicit override; "none" clears
}

// NewConfigAgileCmd builds the "config agile" subcommand.
// rawOut is real stdout so interactive prompts are visible immediately.
func NewConfigAgileCmd(rawOut io.Writer) *cobra.Command {
	var flags ConfigAgileFlags
	c := &cobra.Command{
		Use:   "agile",
		Short: "View or update Agile / Sprint field configuration",
		Long: `View or update the sprint custom-field ID for the current profile.

The sprint field is discovered automatically during setup. Use --rediscover to
re-run discovery (e.g. after installing Jira Software), or --field to set it
explicitly.

Examples:
  jiracli config agile
  jiracli config agile --rediscover
  jiracli config agile --field customfield_11002
  jiracli config agile --field none        # clear
  jiracli config agile --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ConfigAgile(cmd.Context(), rawOut, flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().BoolVar(&flags.Rediscover, "rediscover", false, "Re-run sprint field discovery")
	c.Flags().StringVar(&flags.Field, "field", "", "Set sprint field id or name ('none' to clear)")
	return c
}

// ConfigAgile is the Layer 1 implementation for `config agile`.
func ConfigAgile(ctx context.Context, out io.Writer, flags ConfigAgileFlags) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	changed := false

	if flags.Rediscover {
		client := jira.New(entry)
		store := cache.NewStore(entry)
		fid, err := client.ResolveSprintField(ctx, store, true)
		if err != nil {
			return "", fmt.Errorf("sprint field discovery: %w", err)
		}
		if fid != "" {
			if fid != entry.Agile.SprintField {
				entry.Agile.SprintField = fid
				changed = true
			}
			fmt.Fprintf(out, "✓ Sprint field = %s\n", fid)
		} else {
			fmt.Fprintln(out, "  (Sprint field not found — Jira Software not installed?)")
		}
	}

	if flags.Field != "" {
		switch strings.ToLower(flags.Field) {
		case "none":
			entry.Agile.SprintField = ""
			entry.Agile.DiscoveredAt = time.Time{}
			changed = true
		default:
			client := jira.New(entry)
			store := cache.NewStore(entry)
			fid, _, err := client.ResolveFieldID(ctx, flags.Field, store, false)
			if err != nil {
				return "", fmt.Errorf("unknown field %q — run: jiracli lookup fields", flags.Field)
			}
			entry.Agile.SprintField = fid
			changed = true
		}
	}

	if changed {
		entry.Agile.DiscoveredAt = time.Now().UTC()
		if err := keychain.Save(entry); err != nil {
			return "", fmt.Errorf("saving agile config: %w", err)
		}
	}

	if flags.JSON {
		data, err := json.Marshal(entry.Agile)
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Agile config for profile %q:\n", entry.Profile)
	fmt.Fprintf(&sb, "  Sprint field : %s\n", orDash(entry.Agile.SprintField))
	if !entry.Agile.DiscoveredAt.IsZero() {
		fmt.Fprintf(&sb, "  Discovered at: %s\n", entry.Agile.DiscoveredAt.Format(time.RFC3339))
	}

	// Live resolution diagnostic (always, when not in --rediscover mode which already printed)
	if !flags.Rediscover {
		client := jira.New(entry)
		store := cache.NewStore(entry)
		sb.WriteString("\nLive sprint field probe:\n")
		fid, probeErr := client.ResolveSprintField(ctx, store, true)
		if probeErr != nil {
			fmt.Fprintf(&sb, "  ✗ probe error: %v\n", probeErr)
		} else if fid == "" {
			sb.WriteString("  ✗ Sprint field not found in /field list\n")
		} else if fid == entry.Agile.SprintField {
			fmt.Fprintf(&sb, "  ✓ resolved: %s (matches config)\n", fid)
		} else {
			fmt.Fprintf(&sb, "  ⚠ resolved: %s (differs from config %s) — run --rediscover to update\n", fid, orDash(entry.Agile.SprintField))
		}
	}

	return sb.String(), nil
}
