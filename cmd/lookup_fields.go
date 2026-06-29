package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

type lookupFieldsFlags struct {
	Profile string
	JSON    bool
	NoCache bool
	Custom  bool
	ID      string
	Project string
	Type    string
}

func newLookupFieldsCmd() *cobra.Command {
	var flags lookupFieldsFlags
	c := &cobra.Command{
		Use:   "fields [<query>]",
		Short: "List or resolve Jira fields",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := ""
			if len(args) > 0 {
				q = args[0]
			}
			result, err := lookupFields(cmd.Context(), flags, q)
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
	c.Flags().BoolVar(&flags.Custom, "custom", false, "Show only custom fields")
	c.Flags().StringVar(&flags.ID, "id", "", "Resolve a single field by name or id")
	c.Flags().StringVar(&flags.Project, "project", "", "Project key (used with --id and --type for create meta)")
	c.Flags().StringVar(&flags.Type, "type", "", "Issue type id (used with --id and --project for create meta)")
	return c
}

func lookupFields(ctx context.Context, flags lookupFieldsFlags, q string) (string, error) {
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	// --id: resolve a single field, optionally enriched with create-meta allowed values.
	if flags.ID != "" {
		fieldID, field, err := client.ResolveFieldID(ctx, flags.ID, store, flags.NoCache)
		if err != nil {
			return "", fmt.Errorf("resolving field %q: %w", flags.ID, err)
		}

		type allowedValue struct {
			ID    string `json:"id,omitempty"`
			Name  string `json:"name,omitempty"`
			Value string `json:"value,omitempty"`
		}
		var allowedValues []allowedValue
		if flags.Project != "" && flags.Type != "" {
			meta, merr := client.GetCreateMeta(ctx, flags.Project, flags.Type, store, flags.NoCache)
			if merr == nil {
				for _, mf := range meta.Fields {
					if mf.FieldID == fieldID {
						for _, av := range mf.AllowedValues {
							allowedValues = append(allowedValues, allowedValue{av.ID, av.Name, av.Value})
						}
						break
					}
				}
			}
		}

		if flags.JSON {
			schemaType := ""
			if field.Schema != nil {
				schemaType = field.Schema.Type
			}
			rec := struct {
				ID            string         `json:"id"`
				Name          string         `json:"name"`
				Custom        bool           `json:"custom"`
				Type          string         `json:"type"`
				AllowedValues []allowedValue `json:"allowedValues,omitempty"`
			}{
				ID:            fieldID,
				Name:          field.Name,
				Custom:        field.Custom,
				Type:          schemaType,
				AllowedValues: allowedValues,
			}
			data, _ := json.Marshal(rec)
			return string(data) + "\n", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("id:     %s\n", fieldID))
		sb.WriteString(fmt.Sprintf("name:   %s\n", field.Name))
		sb.WriteString(fmt.Sprintf("custom: %v\n", field.Custom))
		if field.Schema != nil {
			sb.WriteString(fmt.Sprintf("type:   %s\n", field.Schema.Type))
			if field.Schema.Items != "" {
				sb.WriteString(fmt.Sprintf("items:  %s\n", field.Schema.Items))
			}
		}
		if len(allowedValues) > 0 {
			sb.WriteString("allowedValues:\n")
			for _, av := range allowedValues {
				label := av.Name
				if label == "" {
					label = av.Value
				}
				if av.ID != "" {
					sb.WriteString(fmt.Sprintf("  %s (%s)\n", label, av.ID))
				} else {
					sb.WriteString(fmt.Sprintf("  %s\n", label))
				}
			}
		}
		return sb.String(), nil
	}

	// List all fields with optional prefix-filter and --custom.
	fields, err := client.ListFields(ctx, store, flags.NoCache)
	if err != nil {
		return "", fmt.Errorf("fetching fields: %w", err)
	}

	ql := strings.ToLower(q)
	var matched []jira.Field
	for _, f := range fields {
		if flags.Custom && !f.Custom {
			continue
		}
		if q != "" && !strings.HasPrefix(strings.ToLower(f.Name), ql) && !strings.HasPrefix(strings.ToLower(f.ID), ql) {
			continue
		}
		matched = append(matched, f)
	}

	if flags.JSON {
		var sb strings.Builder
		for _, f := range matched {
			schemaType := ""
			if f.Schema != nil {
				schemaType = f.Schema.Type
			}
			rec := struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Custom bool   `json:"custom"`
				Type   string `json:"type"`
			}{f.ID, f.Name, f.Custom, schemaType}
			data, _ := json.Marshal(rec)
			sb.Write(data)
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}

	var sb strings.Builder
	for _, f := range matched {
		custom := ""
		if f.Custom {
			custom = "  [custom]"
		}
		schemaType := ""
		if f.Schema != nil {
			schemaType = "  " + f.Schema.Type
		}
		sb.WriteString(fmt.Sprintf("%-35s  %-40s%s%s\n", f.ID, f.Name, custom, schemaType))
	}
	if len(matched) == 0 {
		sb.WriteString("no fields found\n")
	}
	return sb.String(), nil
}
