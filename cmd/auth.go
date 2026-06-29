package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// NewAuthCmd builds the auth command group.
func NewAuthCmd(rawOut io.Writer) *cobra.Command {
	c := &cobra.Command{
		Use:   "auth",
		Short: "Manage Jira credentials",
		Long:  "Manage Jira authentication profiles stored in the macOS Keychain.",
	}
	c.AddCommand(
		newAuthLoginCmd(),
		newAuthReauthCmd(),
		newAuthLogoutCmd(),
		NewStatusCmd(),
		newAuthDefaultCmd(),
	)
	return c
}

// ── auth login ──────────────────────────────────────────────────────────────

func newAuthLoginCmd() *cobra.Command {
	var flags struct {
		Profile  string
		URL      string
		PATFile  string
		Insecure bool
	}
	c := &cobra.Command{
		Use:   "login",
		Short: "Save PAT credentials for a profile",
		Long:  `Scriptable equivalent of setup: saves a PAT without wizard prompts or skill install.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.URL == "" {
				return fmt.Errorf("--url is required — run: jiracli auth login --url <jira-url>")
			}
			rawURL := strings.TrimRight(flags.URL, "/")
			if !strings.Contains(rawURL, "://") {
				rawURL = "https://" + rawURL
			}

			var pat string
			switch {
			case flags.PATFile != "":
				data, err := os.ReadFile(flags.PATFile)
				if err != nil {
					return fmt.Errorf("reading PAT file %q: %w", flags.PATFile, err)
				}
				pat = strings.TrimSpace(string(data))
			case os.Getenv("JIRACLI_PAT") != "":
				pat = os.Getenv("JIRACLI_PAT")
			default:
				pat = authPromptSecret("PAT (input hidden): ")
			}
			if pat == "" {
				return fmt.Errorf("PAT is required")
			}

			entry := keychain.Entry{
				Profile:  flags.Profile,
				URL:      rawURL,
				Kind:     "dc-pat",
				PAT:      pat,
				Insecure: flags.Insecure,
			}
			ctx := context.Background()
			client := jira.New(entry)
			u, err := client.Myself(ctx)
			if err != nil {
				return fmt.Errorf("PAT verification failed: %w", err)
			}
			entry.User = u.Name
			entry.DisplayName = u.DisplayName
			entry.SavedAt = time.Now().UTC()

			if err := keychain.Save(entry); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}
			if _, err := keychain.GetDefault(); errors.Is(err, keychain.ErrNotFound) {
				_ = keychain.SetDefault(entry.Profile)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ saved profile %q (user %s)\n", entry.Profile, entry.User)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "default", "Profile name")
	c.Flags().StringVar(&flags.URL, "url", "", "Jira server URL (required)")
	c.Flags().StringVar(&flags.PATFile, "pat-file", "", "Read PAT from file")
	c.Flags().BoolVar(&flags.Insecure, "insecure", false, "Skip TLS certificate verification")
	return c
}

// ── auth reauth ─────────────────────────────────────────────────────────────

func newAuthReauthCmd() *cobra.Command {
	var flags struct {
		Profile string
		URL     string
	}
	c := &cobra.Command{
		Use:   "reauth",
		Short: "Re-enter PAT for an existing profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profile := flags.Profile
			if profile == "" {
				var err error
				profile, err = keychain.ResolveDefault()
				if err != nil {
					return fmt.Errorf("no profile specified — run: jiracli auth reauth --profile <name>")
				}
			}

			existing, err := keychain.Load(profile)
			rawURL := flags.URL
			if rawURL == "" {
				if err == nil {
					rawURL = existing.URL
				} else {
					return fmt.Errorf("--url required (no existing entry for profile %q)", profile)
				}
			}

			patLink := rawURL + "/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats.pats-plugin:jira-user-personal-access-tokens"
			fmt.Fprintf(cmd.ErrOrStderr(), "Create / copy a PAT at:\n  %s\n\n", patLink)

			pat := authPromptSecret("New PAT (input hidden): ")
			if pat == "" {
				return fmt.Errorf("PAT is required")
			}

			entry := keychain.Entry{
				Profile:  profile,
				URL:      rawURL,
				Kind:     "dc-pat",
				PAT:      pat,
				Insecure: existing.Insecure,
			}
			ctx := context.Background()
			client := jira.New(entry)
			u, uerr := client.Myself(ctx)
			if uerr != nil {
				return fmt.Errorf("PAT verification failed: %w", uerr)
			}
			entry.User = u.Name
			entry.DisplayName = u.DisplayName
			entry.SavedAt = time.Now().UTC()

			if err := keychain.Save(entry); err != nil {
				return fmt.Errorf("saving credentials: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ re-authenticated profile %q (%s)\n", profile, u.DisplayName)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name (defaults to default profile)")
	c.Flags().StringVar(&flags.URL, "url", "", "Override Jira server URL")
	return c
}

// ── auth logout ─────────────────────────────────────────────────────────────

func newAuthLogoutCmd() *cobra.Command {
	var flags struct {
		Profile string
		All     bool
	}
	c := &cobra.Command{
		Use:   "logout",
		Short: "Remove saved credentials",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.All {
				entries, _, err := keychain.List()
				if err != nil {
					return fmt.Errorf("listing profiles: %w", err)
				}
				for _, e := range entries {
					if derr := keychain.Delete(e.Profile); derr != nil && derr != keychain.ErrNotFound {
						return fmt.Errorf("removing profile %q: %w", e.Profile, derr)
					}
				}
				_ = keychain.DeleteDefault()
				fmt.Fprintln(cmd.OutOrStdout(), "✓ removed all profiles")
				return nil
			}

			profile := flags.Profile
			if profile == "" {
				var err error
				profile, err = keychain.ResolveDefault()
				if err != nil {
					return fmt.Errorf("no profile specified — use --profile or --all")
				}
			}

			// TTY confirmation
			if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
				defer tty.Close()
				fmt.Fprintf(tty, "Remove profile %q? [y/N]: ", profile)
				var ans string
				fmt.Fscan(tty, &ans)
				if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
					fmt.Fprintln(cmd.OutOrStdout(), "aborted")
					return nil
				}
			}

			if err := keychain.Delete(profile); err != nil && err != keychain.ErrNotFound {
				return fmt.Errorf("removing profile %q: %w", profile, err)
			}
			// If deleted profile was default, clear it
			if def, err := keychain.GetDefault(); err == nil && def == profile {
				_ = keychain.DeleteDefault()
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ removed profile %q\n", profile)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.All, "all", false, "Remove all profiles")
	return c
}

// ── auth default ────────────────────────────────────────────────────────────

func newAuthDefaultCmd() *cobra.Command {
	var flags struct {
		Clear bool
		List  bool
	}
	c := &cobra.Command{
		Use:   "profile [name]",
		Short: "Get or set the active profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.Clear {
				if err := keychain.DeleteDefault(); err != nil && err != keychain.ErrNotFound {
					return fmt.Errorf("clearing default: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "✓ default cleared")
				return nil
			}
			if flags.List {
				entries, _, err := keychain.List()
				if err != nil {
					return fmt.Errorf("listing profiles: %w", err)
				}
				def, _ := keychain.GetDefault()
				for _, e := range entries {
					marker := "  "
					if e.Profile == def {
						marker = "* "
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%s%s\n", marker, e.Profile)
				}
				return nil
			}
			if len(args) == 1 {
				if err := keychain.SetDefault(args[0]); err != nil {
					return fmt.Errorf("setting default: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "✓ default profile set to %q\n", args[0])
				return nil
			}
			// No args: print current default
			def, err := keychain.GetDefault()
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "none")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), def)
			return nil
		},
	}
	c.Flags().BoolVar(&flags.Clear, "clear", false, "Clear the default profile")
	c.Flags().BoolVar(&flags.List, "list", false, "List all profiles (default marked with *)")
	return c
}
