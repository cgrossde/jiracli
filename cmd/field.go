package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewEditFieldCmd builds the "edit field" leaf command (was: field set).
func NewEditFieldCmd() *cobra.Command {
	return newFieldSetCmd()
}

type fieldSetFlags struct {
	Profile  string
	AllowNew bool
	Yes      bool
	NoCache  bool
}

func newFieldSetCmd() *cobra.Command {
	var flags fieldSetFlags
	c := &cobra.Command{
		Use:   "field <KEY> [KEY...] <spec...>",
		Short: "Set one or more fields on one or more issues (dry-run by default; use --yes to apply)",
		Long: `Spec format: name=value  name+=value  name-=value

Operators:
  =    replace (single-value or whole list)
  +=   add to list (multi-value fields only)
  -=   remove from list (multi-value fields only)

Value prefix @ reads from file: description=@desc.md

Keys vs specs: leading args without = are treated as issue keys; the first
arg containing = starts the spec list. At least one key and one spec required.

  edit field ACME-123 "priority=High"
  edit field ACME-123 ACME-124 ACME-125 "labels+=triage" --yes

See also:
  jiracli lookup fields --project <KEY>    discover field names and IDs
  jiracli lookup priorities --project <KEY> valid priority values
  jiracli lookup users --project <KEY>     valid assignee values
  jiracli lookup labels --project <KEY>    valid label values`,
		Example: `  jiracli edit field ACME-123 "priority=High"
  jiracli edit field ACME-123 ACME-124 "labels+=regression" --yes
  jiracli edit field ACME-123 "description=@desc.md" --yes
  jiracli edit field ACME-123 "labels-=stale" "labels+=current" --yes`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Split: leading args without '=' are keys; remainder are specs.
			splitAt := len(args)
			for i, a := range args {
				if strings.ContainsAny(a, "=") {
					splitAt = i
					break
				}
			}
			keys := args[:splitAt]
			specs := args[splitAt:]
			if len(keys) == 0 {
				return fmt.Errorf("at least one issue key required before the first spec")
			}
			if len(specs) == 0 {
				return fmt.Errorf("at least one field spec required (e.g. priority=High)")
			}
			if len(keys) == 1 {
				result, err := FieldSet(cmd.Context(), flags, keys[0], specs)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), result)
				return nil
			}
			result, err := FieldSetBulk(cmd.Context(), flags, keys, specs)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().BoolVar(&flags.AllowNew, "allow-new", false, "Allow creating new labels/versions (skips client-side validation)")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Bypass local cache")
	return c
}

// multiValueFields is the set of well-known field names that accept list ops (+=/-=).
var multiValueFields = map[string]bool{
	"labels": true, "components": true, "fixversions": true, "versions": true,
}

// FieldSet is the Layer 1 implementation of the field set command.
func FieldSet(ctx context.Context, flags fieldSetFlags, key string, tokens []string) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// Parse all specs.
	var specs []jira.FieldSpec
	for _, tok := range tokens {
		spec, parseErr := jira.ParseFieldSpec(tok)
		if parseErr != nil {
			return "", parseErr
		}
		specs = append(specs, spec)
	}

	// Resolve @path values (only valid with = operator).
	for i, spec := range specs {
		if strings.HasPrefix(spec.Raw, "@") {
			if spec.Op != jira.OpReplace {
				return "", fmt.Errorf("@path values only supported with = operator (not += or -=)")
			}
			path := spec.Raw[1:]
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", fmt.Errorf("reading %s: %w", path, readErr)
			}
			specs[i].Raw = string(data)
		}
	}

	// Derive project key from issue key (everything before the last "-NNN").
	parts := strings.Split(key, "-")
	projectKey := strings.Join(parts[:len(parts)-1], "-")

	fieldsBlock := map[string]any{}
	updateBlock := map[string]any{}
	var validation []jira.ValidationRow
	var effectLines []string

	for _, spec := range specs {
		fieldName := spec.Name
		value := spec.Raw
		isMulti := multiValueFields[strings.ToLower(fieldName)]

		if !isMulti && spec.Op != jira.OpReplace {
			opSym := map[jira.FieldOp]string{jira.OpAdd: "+", jira.OpRemove: "-"}[spec.Op]
			return "", fmt.Errorf("field %q is single-value — use %s=%s, not %s%s=%s",
				fieldName, fieldName, value, fieldName, opSym, value)
		}

		switch strings.ToLower(fieldName) {
		case "priority":
			if spec.Op != jira.OpReplace {
				return "", fmt.Errorf("priority is single-value — use priority=<value>")
			}
			priorities, _, _ := client.ListProjectPriorities(ctx, projectKey, store, flags.NoCache)
			found := false
			for _, p := range priorities {
				if strings.EqualFold(p.Name, value) {
					found = true
					break
				}
			}
			if !found && !flags.AllowNew {
				var names []string
				var suggestions []string
				ql := strings.ToLower(value)
				for _, p := range priorities {
					names = append(names, p.Name)
					// Fuzzy match: priority name contains the user's input as a substring.
					// e.g. "High" matches "2 - High".
					if strings.Contains(strings.ToLower(p.Name), ql) {
						suggestions = append(suggestions, p.Name)
					}
				}
				msg := fmt.Sprintf("unknown priority %q for project %s\nAvailable: %s\nRun: jiracli lookup priorities --project %s",
					value, projectKey, strings.Join(names, ", "), projectKey)
				if len(suggestions) > 0 {
					msg += fmt.Sprintf("\nDid you mean: %s?", strings.Join(suggestions, ", "))
				}
				return "", fmt.Errorf("%s", msg)
			}
			fieldsBlock["priority"] = map[string]string{"name": value}
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("priority %q is valid for %s", value, projectKey),
			})
			effectLines = append(effectLines, fmt.Sprintf("priority → %s", value))

		case "labels":
			if !flags.AllowNew {
				labels, labErr := client.AggregateLabelsByProject(ctx, projectKey, store, flags.NoCache)
				if labErr == nil {
					found := false
					for _, l := range labels {
						if strings.EqualFold(l, value) {
							found = true
							break
						}
					}
					if !found {
						// Auto-invalidate cache and retry once.
						_ = store.Delete("labels/" + projectKey)
						labels, labErr = client.AggregateLabelsByProject(ctx, projectKey, store, true)
						if labErr == nil {
							found = false
							for _, l := range labels {
								if strings.EqualFold(l, value) {
									found = true
									break
								}
							}
						}
						if !found {
							return "", fmt.Errorf(
								"unknown label %q in project %s — use --allow-new to create it\nRun: jiracli lookup labels %s --project %s",
								value, projectKey, value, projectKey)
						}
					}
				}
			}
			switch spec.Op {
			case jira.OpReplace:
				existing, _ := fieldsBlock["labels"].([]string)
				fieldsBlock["labels"] = append(existing, value)
			case jira.OpAdd:
				existing, _ := updateBlock["labels"].([]map[string]any)
				updateBlock["labels"] = append(existing, map[string]any{"add": value})
			case jira.OpRemove:
				existing, _ := updateBlock["labels"].([]map[string]any)
				updateBlock["labels"] = append(existing, map[string]any{"remove": value})
			}
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("label %q applied", value),
			})
			effectLines = append(effectLines, fmt.Sprintf("labels %s %s", spec.Op.String(), value))

		case "components":
			if !flags.AllowNew {
				proj, pErr := client.GetProject(ctx, projectKey, store, flags.NoCache)
				if pErr == nil {
					found := false
					for _, comp := range proj.Components {
						if strings.EqualFold(comp.Name, value) {
							found = true
							break
						}
					}
					if !found {
						return "", fmt.Errorf("unknown component %q in project %s\nRun: jiracli lookup components --project %s",
							value, projectKey, projectKey)
					}
				}
			}
			switch spec.Op {
			case jira.OpReplace:
				fieldsBlock["components"] = []map[string]string{{"name": value}}
			case jira.OpAdd:
				existing, _ := updateBlock["components"].([]map[string]any)
				updateBlock["components"] = append(existing, map[string]any{"add": map[string]string{"name": value}})
			case jira.OpRemove:
				existing, _ := updateBlock["components"].([]map[string]any)
				updateBlock["components"] = append(existing, map[string]any{"remove": map[string]string{"name": value}})
			}
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("component %q", value),
			})
			effectLines = append(effectLines, fmt.Sprintf("components %s %s", spec.Op.String(), value))

		case "fixversions", "versions":
			if !flags.AllowNew {
				proj, pErr := client.GetProject(ctx, projectKey, store, flags.NoCache)
				if pErr == nil {
					found := false
					for _, v := range proj.Versions {
						if strings.EqualFold(v.Name, value) {
							found = true
							break
						}
					}
					if !found {
						return "", fmt.Errorf("unknown version %q in project %s — use --allow-new to add it\nRun: jiracli lookup versions --project %s",
							value, projectKey, projectKey)
					}
				}
			}
			// Jira uses "fixVersions" as the canonical field name.
			jiraFieldName := "fixVersions"
			switch spec.Op {
			case jira.OpReplace:
				fieldsBlock[jiraFieldName] = []map[string]string{{"name": value}}
			case jira.OpAdd:
				existing, _ := updateBlock[jiraFieldName].([]map[string]any)
				updateBlock[jiraFieldName] = append(existing, map[string]any{"add": map[string]string{"name": value}})
			case jira.OpRemove:
				existing, _ := updateBlock[jiraFieldName].([]map[string]any)
				updateBlock[jiraFieldName] = append(existing, map[string]any{"remove": map[string]string{"name": value}})
			}
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("version %q", value),
			})
			effectLines = append(effectLines, fmt.Sprintf("fixVersions %s %s", spec.Op.String(), value))

		default:
			// Generic scalar field — place in fields block as a plain string.
			fieldsBlock[fieldName] = value
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("field %q = %s", fieldName, value),
			})
			effectLines = append(effectLines, fmt.Sprintf("%s = %s", fieldName, value))
		}
	}

	// Assemble PUT body.
	body := map[string]any{}
	if len(fieldsBlock) > 0 {
		body["fields"] = fieldsBlock
	}
	if len(updateBlock) > 0 {
		body["update"] = updateBlock
	}

	n := len(specs)
	p := jira.Preview{
		Method:      "PUT",
		Path:        "/issue/" + key,
		Body:        body,
		Description: strings.Join(effectLines, "; "),
		Validation:  validation,
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(_ []byte) string {
		return fmt.Sprintf("✓ updated %s (%d field(s))\n  → jiracli show %s\n", key, n, key)
	})
}

// FieldSetBulk applies the same field specs to multiple issues.
//
// Dry-run: calls FieldSet in dry-run mode for the first key to produce a
// representative preview, then lists all target keys. On --yes: executes
// FieldSet sequentially with per-key error collection.
func FieldSetBulk(ctx context.Context, flags fieldSetFlags, keys []string, tokens []string) (string, error) {
	// Build a representative dry-run preview using the first key.
	dryFlags := flags
	dryFlags.Yes = false
	samplePreview, err := FieldSet(ctx, dryFlags, keys[0], tokens)
	if err != nil {
		// Error on first key (e.g. invalid spec) — abort before touching anything.
		return "", fmt.Errorf("spec validation failed on %s: %w", keys[0], err)
	}

	// Build combined preview: sample effect + full key list.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("apply to %d issue(s):\n", len(keys)))
	for _, key := range keys {
		sb.WriteString(fmt.Sprintf("  %s\n", key))
	}
	sb.WriteString("\nSpec preview (from " + keys[0] + "):\n")
	// Indent the sample preview for readability.
	for _, line := range strings.Split(strings.TrimRight(samplePreview, "\n"), "\n") {
		sb.WriteString("  " + line + "\n")
	}
	preview := sb.String()

	// Apply or return preview.
	doApply := flags.Yes
	if !doApply {
		apply, ttyAvailable := promptApply()
		if !ttyAvailable {
			return preview + "\nApply with: re-run with --yes\n", nil
		}
		if !apply {
			return preview + "\naborted\n", nil
		}
		doApply = true
	}
	_ = doApply

	// Execute sequentially with per-key error collection.
	applyFlags := flags
	applyFlags.Yes = true
	var applyErrs []string
	var okKeys []string
	for _, key := range keys {
		if _, err := FieldSet(ctx, applyFlags, key, tokens); err != nil {
			applyErrs = append(applyErrs, fmt.Sprintf("  %s: %v", key, err))
		} else {
			okKeys = append(okKeys, key)
		}
	}

	var out strings.Builder
	out.WriteString(preview + "\n")
	for _, key := range okKeys {
		out.WriteString(fmt.Sprintf("✓ updated %s\n", key))
	}
	if len(applyErrs) > 0 {
		out.WriteString("\nFailed:\n")
		for _, e := range applyErrs {
			out.WriteString(e + "\n")
		}
		return out.String(), fmt.Errorf("%d of %d updates failed", len(applyErrs), len(keys))
	}
	return out.String(), nil
}
