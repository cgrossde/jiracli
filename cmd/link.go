package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/cache"
	"github.com/cgrossde/jiracli/internal/jira"
)

type LinkFlags struct {
	Profile string
	Type    string
	Yes     bool
	NoCache bool
}

func NewLinkCmd() *cobra.Command {
	var flags LinkFlags
	c := &cobra.Command{
		Use:   "link <source> <target>",
		Short: "Link two issues (dry-run by default; use --yes to apply)",
		Long: `Link two issues (dry-run by default; use --yes to apply).

See also:
  jiracli lookup link-types    list available link type names`,
		Example: `  jiracli add link ACME-123 ACME-456
  jiracli add link ACME-123 ACME-456 --type "Relates" --yes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Link(cmd.Context(), flags, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().StringVar(&flags.Type, "type", "Blocks", "Link type name")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Bypass local cache")
	return c
}

func Link(ctx context.Context, flags LinkFlags, source, target string) (string, error) {
	entry, err := resolveEntry(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)
	store := cache.NewStore(entry)

	// Validate link type
	var validation []jira.ValidationRow
	linkTypes, err := client.ListLinkTypes(ctx, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching link types: %w", err)
	}
	var resolvedType string
	var outwardVerb string
	for _, lt := range linkTypes {
		if strings.EqualFold(lt.Name, flags.Type) {
			resolvedType = lt.Name
			outwardVerb = lt.Outward
			break
		}
	}
	if resolvedType == "" {
		var names []string
		for _, lt := range linkTypes {
			names = append(names, lt.Name)
		}
		return "", fmt.Errorf("no link type %q — run: jiracli lookup link-types\nAvailable: %s", flags.Type, strings.Join(names, ", "))
	}
	validation = append(validation, jira.ValidationRow{Status: "✓", Message: fmt.Sprintf("link type %q exists (outward: %s)", resolvedType, outwardVerb)})

	// Validate both issues exist
	_, serr := client.GetIssue(ctx, source, "key", false)
	if serr != nil {
		validation = append(validation, jira.ValidationRow{Status: "✗", Message: fmt.Sprintf("%s not found", source)})
	} else {
		validation = append(validation, jira.ValidationRow{Status: "✓", Message: source + " exists"})
	}
	_, terr := client.GetIssue(ctx, target, "key", false)
	if terr != nil {
		validation = append(validation, jira.ValidationRow{Status: "✗", Message: fmt.Sprintf("%s not found", target)})
	} else {
		validation = append(validation, jira.ValidationRow{Status: "✓", Message: target + " exists"})
	}

	body := map[string]any{
		"type":         map[string]string{"name": resolvedType},
		"outwardIssue": map[string]string{"key": source},
		"inwardIssue":  map[string]string{"key": target},
	}

	p := jira.Preview{
		Method:      "POST",
		Path:        "/issueLink",
		Body:        body,
		Description: fmt.Sprintf("%s %s %s", source, outwardVerb, target),
		Validation:  validation,
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ linked %s %s %s\n", source, outwardVerb, target)
	})
}
