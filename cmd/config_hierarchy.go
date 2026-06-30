package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// ConfigHierarchyFlags holds parsed flag values for `config hierarchy`.
type ConfigHierarchyFlags struct {
	Profile    string
	JSON       bool
	Portfolio  string // field id or name, "none" to clear, "" to leave unchanged
	Rediscover bool   // re-run Tier 1+2 auto-detect (interactive portfolio selection)
}

// NewConfigHierarchyCmd builds the "config hierarchy" subcommand.
// rawOut is real stdout so interactive prompts are visible immediately.
func NewConfigHierarchyCmd(rawOut io.Writer) *cobra.Command {
	var flags ConfigHierarchyFlags
	c := &cobra.Command{
		Use:   "hierarchy",
		Short: "View or update hierarchy field configuration for a profile",
		Long: `View or update the hierarchy field IDs stored per profile.

Without flags, prints the current configuration.
  --rediscover                 Re-resolve Epic Link, Parent Link, and scan for
                               portfolio candidates (interactive selection).
  --portfolio <field-id|none>  Set or clear the Portfolio field directly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := ConfigHierarchy(cmd.Context(), rawOut, flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.JSON, "json", false, "Output NDJSON")
	c.Flags().StringVar(&flags.Portfolio, "portfolio", "", "Set Portfolio field id or name ('none' to clear)")
	c.Flags().BoolVar(&flags.Rediscover, "rediscover", false, "Re-run Tier 1+2 auto-detect (prompts for portfolio field)")
	return c
}

// ConfigHierarchy is the Layer 1 implementation.
// out is real stdout, used for interactive prompts during --rediscover.
func ConfigHierarchy(ctx context.Context, out io.Writer, flags ConfigHierarchyFlags) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	changed := false

	if flags.Rediscover {
		client := jira.New(entry)
		store := cache.NewStore(entry)

		// Tier 1: resolve Epic Link and Parent Link by name.
		if fid, _, err := client.ResolveFieldID(ctx, "Epic Link", store, true); err == nil {
			entry.Hierarchy.EpicLinkField = fid
			fmt.Fprintf(out, "✓ Epic Link = %s\n", fid)
			changed = true
		} else {
			fmt.Fprintf(out, "  (Epic Link field not found — Jira Software not installed?)\n")
		}
		if fid, _, err := client.ResolveFieldID(ctx, "Parent Link", store, true); err == nil {
			entry.Hierarchy.ParentLinkField = fid
			fmt.Fprintf(out, "✓ Parent Link = %s\n", fid)
			changed = true
		} else {
			fmt.Fprintf(out, "  (Parent Link field not found — Advanced Roadmaps not installed?)\n")
		}
		if fid, _, err := client.ResolveFieldID(ctx, "Story Points", store, true); err == nil {
			entry.Hierarchy.StoryPointsField = fid
			fmt.Fprintf(out, "✓ Story Points = %s\n", fid)
			changed = true
		} else {
			fmt.Fprintf(out, "  (Story Points field not found — Jira Software not installed?)\n")
		}

		// Tier 2: portfolio candidate scan + interactive selection.
		// Skipped when --portfolio is also set (explicit flag wins).
		if flags.Portfolio == "" {
			all, err := client.ListFields(ctx, store, true)
			if err != nil {
				fmt.Fprintf(out, "  (could not list fields for portfolio scan: %v — skipping)\n", err)
			} else {
				cands := jira.PortfolioCandidates(all, entry.Hierarchy.EpicLinkField, entry.Hierarchy.ParentLinkField)
				switch len(cands) {
				case 0:
					fmt.Fprintf(out, "  (no portfolio-level custom fields found)\n")
				default:
					fmt.Fprintf(out, "\nPortfolio field candidates:\n")
					for i, f := range cands {
						mark := "   "
						if f.ID == entry.Hierarchy.PortfolioField {
							mark = " ✓ "
						}
						fmt.Fprintf(out, "%s[%d] %-30s (%s)\n", mark, i+1, f.Name, f.ID)
					}
					fmt.Fprintf(out, "   [s] Skip / keep current (%s)\n", orDash(entry.Hierarchy.PortfolioFieldName))
					pick := authPrompt("Pick one [1-" + fmt.Sprintf("%d", len(cands)) + " / s]: ")
					pick = strings.TrimSpace(pick)
					if pick != "" && pick != "s" && pick != "S" {
						if n, perr := strconv.Atoi(pick); perr == nil && n >= 1 && n <= len(cands) {
							entry.Hierarchy.PortfolioField = cands[n-1].ID
							entry.Hierarchy.PortfolioFieldName = cands[n-1].Name
							fmt.Fprintf(out, "✓ Portfolio = %s (%s)\n", cands[n-1].Name, cands[n-1].ID)
							changed = true
						}
					}
				}
			}
		}
	}

	if flags.Portfolio != "" {
		switch strings.ToLower(flags.Portfolio) {
		case "none":
			entry.Hierarchy.PortfolioField = ""
			entry.Hierarchy.PortfolioFieldName = ""
		default:
			client := jira.New(entry)
			store := cache.NewStore(entry)
			fid, f, err := client.ResolveFieldID(ctx, flags.Portfolio, store, false)
			if err != nil {
				return "", fmt.Errorf("unknown field %q — run: jiracli lookup fields", flags.Portfolio)
			}
			entry.Hierarchy.PortfolioField = fid
			entry.Hierarchy.PortfolioFieldName = f.Name
		}
		changed = true
	}

	client := jira.New(entry)
	hf := jira.HierarchyFieldIDs{
		EpicLink:   entry.Hierarchy.EpicLinkField,
		ParentLink: entry.Hierarchy.ParentLinkField,
		Portfolio:  entry.Hierarchy.PortfolioField,
	}

	if changed {
		entry.Hierarchy.DiscoveredAt = time.Now().UTC()
		if err := keychain.Save(entry); err != nil {
			return "", fmt.Errorf("saving hierarchy config: %w", err)
		}
		// Run live diagnostics immediately after save and stream to real stdout.
		fmt.Fprintf(out, "\nRunning live field diagnostics…\n")
		probes := jira.ProbeHierarchy(ctx, client, hf, entry.Hierarchy.PortfolioFieldName)
		for _, p := range probes {
			fmt.Fprint(out, formatProbe(p))
		}
		fmt.Fprintln(out)
	}

	if flags.JSON {
		data, err := json.Marshal(entry.Hierarchy)
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Hierarchy config for profile %q:\n", entry.Profile)
	fmt.Fprintf(&sb, "  Epic Link        : %s\n", orDash(entry.Hierarchy.EpicLinkField))
	fmt.Fprintf(&sb, "  Parent Link      : %s\n", orDash(entry.Hierarchy.ParentLinkField))
	fmt.Fprintf(&sb, "  Portfolio        : %s\n", orDash(entry.Hierarchy.PortfolioField))
	fmt.Fprintf(&sb, "  Portfolio (name) : %s\n", orDash(entry.Hierarchy.PortfolioFieldName))
	fmt.Fprintf(&sb, "  Story Points     : %s\n", orDash(entry.Hierarchy.StoryPointsField))
	if !entry.Hierarchy.DiscoveredAt.IsZero() {
		fmt.Fprintf(&sb, "  Discovered at    : %s\n", entry.Hierarchy.DiscoveredAt.Format(time.RFC3339))
	}
	// Live diagnostics in read-only display (no changes made).
	if !changed {
		sb.WriteString("\nField diagnostics (live):\n")
		probes := jira.ProbeHierarchy(ctx, client, hf, entry.Hierarchy.PortfolioFieldName)
		for _, p := range probes {
			sb.WriteString(formatProbe(p))
		}
	}
	return sb.String(), nil
}

// orDash returns s if non-empty, otherwise "—".
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// formatProbe renders a single FieldProbe as a status line.
func formatProbe(p jira.FieldProbe) string {
	icon := "✓"
	if !p.OK {
		icon = "✗"
	}
	if p.FieldID == "" {
		icon = "–"
	}
	fid := p.FieldID
	if fid == "" {
		fid = "(none)"
	}
	return fmt.Sprintf("  %s %-14s %-24s %s\n", icon, p.Label, fid, p.Note)
}
