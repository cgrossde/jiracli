package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/browser"
	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// NewSetupCmd builds the setup wizard command.
// rawOut is real stdout — wizard output is visible immediately while prompts read stdin.
// skillContent is the //go:embed bytes from main.go.
func NewSetupCmd(rawOut io.Writer, skillContent []byte) *cobra.Command {
	var flags struct {
		Profile        string
		URL            string
		PATFile        string
		NoSkill        bool
		Reconfigure    bool
		NoBrowser      bool
		InstallSkill   bool
		UninstallSkill bool
	}

	c := &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup wizard (auth + skill install)",
		Long: `Guided setup in four steps:
  1. Verify the Jira server URL
  2. Save a Personal Access Token (PAT)
  3. Discover hierarchy custom-field IDs
  4. Install the Claude/OpenCode skill`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := rawOut
			ctx := context.Background()

			// Standalone skill flags short-circuit the wizard.
			if flags.InstallSkill {
				if len(skillContent) == 0 {
					return fmt.Errorf("no embedded skill content available")
				}
				return installSkillUnattended(out, skillContent)
			}
			if flags.UninstallSkill {
				return uninstallSkill(out)
			}

			fmt.Fprintln(out, "Welcome to jiracli — let's get you set up.")

			entry := keychain.Entry{
				Profile: flags.Profile,
				Kind:    "dc-pat",
			}

			// ── Step 1: Server URL ──────────────────────────────────────────────
			setupDivider(out, "Step 1 of 4 — Jira Server")

			// Idempotent skip
			if !flags.Reconfigure {
				if existing, err := keychain.Load(flags.Profile); err == nil && existing.URL != "" {
					fmt.Fprintf(out, "✓ Server already configured (%s)\n", existing.URL)
					entry = existing
					goto step2
				}
			}

			{
				rawURL := flags.URL
				insecure := false
				for attempt := 0; attempt < 3; attempt++ {
					if rawURL == "" {
						rawURL = authPrompt("Jira server URL (e.g. https://jira.example.com): ")
					}
					if rawURL == "" {
						return fmt.Errorf("server URL is required")
					}
					if !strings.Contains(rawURL, "://") {
						rawURL = "https://" + rawURL
					}
					rawURL = strings.TrimRight(rawURL, "/")

					info, err := probeServerInfo(ctx, rawURL, insecure)
					if err != nil {
						if isTLSError(err) && !insecure {
							fmt.Fprintf(out, "TLS error connecting to %s\n", rawURL)
							if authPromptYesNoDefault("Server may use a self-signed cert. Continue with --insecure? [y/N]: ", false) {
								insecure = true
								info, err = probeServerInfo(ctx, rawURL, true)
							}
						}
						if err != nil {
							fmt.Fprintf(out, "Cannot reach server: %v\n", err)
							rawURL = ""
							continue
						}
					}
					fmt.Fprintf(out, "✓ Connected to Jira %s %s\n", info.DeploymentType, info.Version)
					entry.URL = rawURL
					entry.Insecure = insecure
					break
				}
				if entry.URL == "" {
					return fmt.Errorf("failed to connect to Jira server after 3 attempts")
				}
			}

		step2:
			// ── Step 2: PAT ─────────────────────────────────────────────────────
			setupDivider(out, "Step 2 of 4 — Personal Access Token")

			// Idempotent skip
			if !flags.Reconfigure {
				if entry.PAT != "" {
					client := jira.New(entry)
					if u, err := client.Myself(ctx); err == nil {
						fmt.Fprintf(out, "✓ PAT still valid (%s)\n", u.DisplayName)
						goto step3hierarchy
					}
					fmt.Fprintln(out, "Stored PAT was rejected — re-entering.")
				}
			}

			{
				patLink := entry.URL + "/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats.pats-plugin:jira-user-personal-access-tokens"
				fmt.Fprintf(out, "Create a PAT at:\n  %s\n\n", patLink)
				if !flags.NoBrowser {
					if authPromptYesNoDefault("Open in browser now? [Y/n]: ", true) {
						if err := browser.Open(patLink); err != nil {
							fmt.Fprintf(out, "(could not open browser: %v)\n", err)
						}
					}
				}

				for attempt := 0; attempt < 3; attempt++ {
					var pat string
					if flags.PATFile != "" {
						data, err := os.ReadFile(flags.PATFile)
						if err != nil {
							return fmt.Errorf("reading PAT file: %w", err)
						}
						pat = strings.TrimSpace(string(data))
						flags.PATFile = "" // only read once
					} else {
						pat = authPromptSecret("Paste your PAT (input hidden): ")
					}
					if pat == "" {
						return fmt.Errorf("PAT is required")
					}
					entry.PAT = pat
					client := jira.New(entry)
					u, err := client.Myself(ctx)
					if err != nil {
						fmt.Fprintf(out, "Token rejected (%v). Paste again, or hit ENTER to abort.\n", err)
						entry.PAT = ""
						continue
					}
					entry.User = u.Name
					entry.DisplayName = u.DisplayName
					entry.SavedAt = time.Now().UTC()
					if err := keychain.Save(entry); err != nil {
						return fmt.Errorf("saving credentials: %w", err)
					}
					// Set default if not yet set
					if _, err := keychain.GetDefault(); err != nil {
						_ = keychain.SetDefault(entry.Profile)
					}
					fmt.Fprintf(out, "✓ Authenticated as %s (%s)\n", u.DisplayName, u.Name)
					break
				}
				if entry.PAT == "" {
					return fmt.Errorf("PAT verification failed — run: jiracli auth reauth")
				}
			}

		step3hierarchy:
			// ── Step 3: Hierarchy field discovery ──────────────────────────────
			setupDivider(out, "Step 3 of 4 — Hierarchy Fields")

			if !flags.Reconfigure && entry.Hierarchy.EpicLinkField != "" {
				fmt.Fprintf(out, "✓ Hierarchy already configured (Epic=%s Parent=%s Portfolio=%s)\n",
					entry.Hierarchy.EpicLinkField, entry.Hierarchy.ParentLinkField, entry.Hierarchy.PortfolioField)
				goto step4skill
			}

			{
				hClient := jira.New(entry)
				hStore := cache.NewStore(entry)

				var hc keychain.HierarchyConfig
				if fid, _, err := hClient.ResolveFieldID(ctx, "Epic Link", hStore, false); err == nil {
					hc.EpicLinkField = fid
					fmt.Fprintf(out, "✓ Epic Link = %s\n", fid)
				} else {
					fmt.Fprintf(out, "  (Epic Link field not found — Jira Software not installed?)\n")
				}
				if fid, _, err := hClient.ResolveFieldID(ctx, "Parent Link", hStore, false); err == nil {
					hc.ParentLinkField = fid
					fmt.Fprintf(out, "✓ Parent Link = %s\n", fid)
				} else {
					fmt.Fprintf(out, "  (Parent Link field not found — Advanced Roadmaps not installed?)\n")
				}

				all, err := hClient.ListFields(ctx, hStore, false)
				if err != nil {
					fmt.Fprintf(out, "  (could not list fields for portfolio scan: %v — skipping)\n", err)
				} else {
					cands := jira.PortfolioCandidates(all, hc.EpicLinkField, hc.ParentLinkField)
					switch len(cands) {
					case 0:
						fmt.Fprintf(out, "  (no portfolio-level custom fields found)\n")
					default:
						fmt.Fprintf(out, "\nPortfolio field candidates:\n")
						for i, f := range cands {
							fmt.Fprintf(out, "  [%d] %-30s (%s)\n", i+1, f.Name, f.ID)
						}
						fmt.Fprintf(out, "  [s] Skip (no portfolio field)\n")
						pick := authPrompt("Pick one [1-" + fmt.Sprintf("%d", len(cands)) + " / s]: ")
						pick = strings.TrimSpace(pick)
						if pick != "" && pick != "s" && pick != "S" {
							if n, perr := strconv.Atoi(pick); perr == nil && n >= 1 && n <= len(cands) {
								hc.PortfolioField = cands[n-1].ID
								hc.PortfolioFieldName = cands[n-1].Name
								fmt.Fprintf(out, "✓ Portfolio = %s (%s)\n", cands[n-1].Name, cands[n-1].ID)
							}
						}
					}
				}

				if fid, _, err := hClient.ResolveFieldID(ctx, "Story Points", hStore, false); err == nil {
					hc.StoryPointsField = fid
					fmt.Fprintf(out, "✓ Story Points = %s\n", fid)
				} else {
					fmt.Fprintf(out, "  (Story Points field not found — Jira Software not installed?)\n")
				}

				hc.DiscoveredAt = time.Now().UTC()
				entry.Hierarchy = hc
				if err := keychain.Save(entry); err != nil {
					return fmt.Errorf("saving hierarchy config: %w", err)
				}
			}

		step4skill:
			// ── Step 4: Skill install ───────────────────────────────────────────
			setupDivider(out, "Step 4 of 4 — Claude / OpenCode Skill")

			if flags.NoSkill || len(skillContent) == 0 {
				fmt.Fprintln(out, "Skill step skipped.")
			} else {
				if err := setupSkill(out, skillContent); err != nil {
					return err
				}
			}

			// ── Done ────────────────────────────────────────────────────────────
			setupDivider(out, "Done!")
			fmt.Fprintln(out, "Try:")
			fmt.Fprintln(out, "  jiracli auth me")
			fmt.Fprintln(out, "  jiracli show assigned")
			return ErrAlreadyPresented
		},
	}

	c.Flags().StringVar(&flags.Profile, "profile", "default", "Profile name")
	c.Flags().StringVar(&flags.URL, "url", "", "Jira server URL")
	c.Flags().StringVar(&flags.PATFile, "pat-file", "", "Read PAT from file instead of prompting")
	c.Flags().BoolVar(&flags.NoSkill, "no-skill", false, "Skip skill installation")
	c.Flags().BoolVar(&flags.Reconfigure, "reconfigure", false, "Force reconfiguration even if already set up")
	c.Flags().BoolVar(&flags.NoBrowser, "no-browser", false, "Do not auto-open browser for PAT link")
	c.Flags().BoolVar(&flags.InstallSkill, "install-skill", false, "Install the embedded skill non-interactively and exit")
	c.Flags().BoolVar(&flags.UninstallSkill, "uninstall-skill", false, "Remove the installed skill and exit")
	return c
}

// setupDivider prints a visual section divider.
func setupDivider(out io.Writer, title string) {
	fmt.Fprintf(out, "\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n%s\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n", title)
}

// setupSkill installs the embedded SKILL.md to ~/.claude/skills/jira/SKILL.md.
// Byte-compares to existing; only prompts when missing or outdated.
func setupSkill(out io.Writer, content []byte) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	skillDir := filepath.Join(home, ".claude", "skills", "jira")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if existing, rerr := os.ReadFile(skillPath); rerr == nil {
		if bytes.Equal(existing, content) {
			fmt.Fprintf(out, "✓ Skill already installed at %s (up to date).\n", skillPath)
			return nil
		}
		fmt.Fprintf(out, "Skill exists at %s but is outdated.\n", skillPath)
	} else {
		fmt.Fprintf(out, "Install the jira skill for Claude/OpenCode?\nTarget: %s\n\n", skillPath)
	}

	if !authPromptYesNoDefault("Install? [Y/n]: ", true) {
		fmt.Fprintln(out, "Skipped. Run: jiracli setup --install-skill   to install later.")
		return nil
	}

	if merr := os.MkdirAll(skillDir, 0o755); merr != nil {
		return fmt.Errorf("creating skill directory: %w", merr)
	}
	if werr := os.WriteFile(skillPath, content, 0o644); werr != nil {
		return fmt.Errorf("writing skill file: %w", werr)
	}
	fmt.Fprintln(out, "✓ Skill installed.")
	return nil
}

// probeServerInfo fetches /rest/api/2/serverInfo (anonymous endpoint).
func probeServerInfo(ctx context.Context, baseURL string, insecure bool) (jira.ServerInfo, error) {
	transport := http.DefaultTransport
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/rest/api/2/serverInfo", nil)
	if err != nil {
		return jira.ServerInfo{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return jira.ServerInfo{}, err
	}
	defer resp.Body.Close()
	var info jira.ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return jira.ServerInfo{}, fmt.Errorf("unexpected response from server: %w", err)
	}
	return info, nil
}

// isTLSError reports whether the error is a TLS certificate failure.
func isTLSError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "tls") ||
		strings.Contains(msg, "x509")
}
