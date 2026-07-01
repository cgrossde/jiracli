package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/cgrossde/jiracli/internal/jira"
)

// CreateFlags holds all flags for the create command.
type CreateFlags struct {
	Profile     string
	InitDraft   string
	FromDraft   string
	Project     string
	Type        string
	Summary     string
	Description string
	Priority    string
	Assignee    string
	Epic        string   // epic key, resolved via Hierarchy.EpicLinkField
	Components  []string
	Labels      []string
	FixVersions []string
	Custom      []string // "name=value" pairs
	AllowNew    bool
	Yes         bool
	NoCache     bool
}

// draftFile mirrors the YAML draft template schema.
type draftFile struct {
	Project      string            `yaml:"project"`
	Type         string            `yaml:"type"`
	Summary      string            `yaml:"summary"`
	Description  string            `yaml:"description"`
	Priority     string            `yaml:"priority"`
	Assignee     string            `yaml:"assignee"`
	Epic         string            `yaml:"epic"`
	Components   []string          `yaml:"components"`
	Labels       []string          `yaml:"labels"`
	FixVersions  []string          `yaml:"fixVersions"`
	CustomFields map[string]string `yaml:"customFields"`
}

const draftTemplate = `project: ""
type: ""
summary: ""
description: ""
priority: ""
assignee: ""
epic: ""
components: []
labels: []
fixVersions: []
customFields: {}
`

// NewCreateCmd builds the create command.
func NewCreateCmd() *cobra.Command {
	var flags CreateFlags
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new issue (dry-run by default; use --yes to apply)",
		Example: `  jiracli create --init-draft new.yaml            # write template to new.yaml
  jiracli create --from-draft new.yaml            # preview (after editing)
  jiracli create --from-draft new.yaml --yes      # create
  jiracli create --project WEB --type Bug --summary "Login broken"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := Create(cmd.Context(), flags)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), result)
			return nil
		},
	}
	c.Flags().StringVar(&flags.Profile, "profile", "", "Profile name")
	c.Flags().StringVar(&flags.InitDraft, "init-draft", "", "Write a YAML template to this path and exit")
	c.Flags().StringVar(&flags.FromDraft, "from-draft", "", "Load field values from a YAML draft file")
	c.Flags().StringVar(&flags.Project, "project", "", "Project key")
	c.Flags().StringVar(&flags.Type, "type", "", "Issue type")
	c.Flags().StringVar(&flags.Summary, "summary", "", "Issue summary")
	c.Flags().StringVar(&flags.Description, "description", "", "Issue description")
	c.Flags().StringVar(&flags.Priority, "priority", "", "Priority name")
	c.Flags().StringVar(&flags.Assignee, "assignee", "", "Assignee username or 'me'")
	c.Flags().StringVar(&flags.Epic, "epic", "", "Epic key to link this issue to (e.g. PROJ-123)")
	c.Flags().StringArrayVar(&flags.Components, "component", nil, "Component name (repeatable)")
	c.Flags().StringArrayVar(&flags.Labels, "label", nil, "Label (repeatable)")
	c.Flags().StringArrayVar(&flags.FixVersions, "fix-version", nil, "Fix version (repeatable)")
	c.Flags().StringArrayVar(&flags.Custom, "custom", nil, "Custom field name=value (repeatable)")
	c.Flags().BoolVar(&flags.AllowNew, "allow-new", false, "Allow new labels/versions")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "Apply without confirmation")
	c.Flags().BoolVar(&flags.NoCache, "no-cache", false, "Bypass local cache")
	return c
}

// Create is the Layer 1 implementation: validates inputs, builds the preview,
// and delegates to HandleWrite. Returns the rendered output string.
func Create(ctx context.Context, flags CreateFlags) (string, error) {
	// --init-draft: write template to the given path and exit.
	if flags.InitDraft != "" {
		if err := os.WriteFile(flags.InitDraft, []byte(draftTemplate), 0o644); err != nil {
			return "", fmt.Errorf("writing draft template: %w", err)
		}
		return fmt.Sprintf("wrote draft template to %s\nEdit it, then run: jiracli create --from-draft %s [--yes]\n", flags.InitDraft, flags.InitDraft), nil
	}

	// --from-draft: load field values from a YAML draft file.
	if flags.FromDraft != "" {
		data, err := os.ReadFile(flags.FromDraft)
		if err != nil {
			return "", fmt.Errorf("reading draft %s: %w", flags.FromDraft, err)
		}
		var d draftFile
		if err := yaml.Unmarshal(data, &d); err != nil {
			return "", fmt.Errorf("parsing draft YAML: %w", err)
		}
		// CLI flags override draft values.
		if flags.Project == "" {
			flags.Project = d.Project
		}
		if flags.Type == "" {
			flags.Type = d.Type
		}
		if flags.Summary == "" {
			flags.Summary = d.Summary
		}
		if flags.Description == "" {
			flags.Description = d.Description
		}
		if flags.Priority == "" {
			flags.Priority = d.Priority
		}
		if flags.Assignee == "" {
			flags.Assignee = d.Assignee
		}
		if flags.Epic == "" {
			flags.Epic = d.Epic
		}
		if len(flags.Components) == 0 {
			flags.Components = d.Components
		}
		if len(flags.Labels) == 0 {
			flags.Labels = d.Labels
		}
		if len(flags.FixVersions) == 0 {
			flags.FixVersions = d.FixVersions
		}
		for k, v := range d.CustomFields {
			flags.Custom = append(flags.Custom, k+"="+v)
		}
	}

	if flags.Project == "" {
		return "", fmt.Errorf("--project is required")
	}
	if flags.Type == "" {
		return "", fmt.Errorf("--type is required")
	}
	if flags.Summary == "" {
		return "", fmt.Errorf("--summary is required")
	}

	// Resolve credentials.
	entry, store, err := resolveEntryAndStore(flags.Profile)
	if err != nil {
		return "", err
	}
	client := jira.New(entry)

	var validation []jira.ValidationRow

	// Validate project exists and is accessible.
	proj, projErr := client.GetProject(ctx, flags.Project, store, flags.NoCache)
	if projErr != nil {
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("project %s not found or not accessible", flags.Project),
		})
	} else {
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: fmt.Sprintf("project %s exists (%s)", flags.Project, proj.Name),
		})
	}

	// Validate issue type.
	var resolvedTypeID string
	issueTypes, _ := client.ListProjectIssueTypes(ctx, flags.Project, store, flags.NoCache)
	for _, it := range issueTypes {
		if strings.EqualFold(it.Name, flags.Type) {
			resolvedTypeID = it.ID
			break
		}
	}
	if resolvedTypeID == "" {
		var names []string
		for _, it := range issueTypes {
			names = append(names, it.Name)
		}
		hint := strings.Join(names, ", ")
		if hint == "" {
			hint = "none found — check project key and permissions"
		}
		validation = append(validation, jira.ValidationRow{
			Status:  "✗",
			Message: fmt.Sprintf("type %q not valid for %s — available: %s", flags.Type, flags.Project, hint),
		})
	} else {
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: fmt.Sprintf("type %q is valid for %s", flags.Type, flags.Project),
		})
	}

	// Validate required fields via create-meta.
	if resolvedTypeID != "" {
		meta, merr := client.GetCreateMeta(ctx, flags.Project, resolvedTypeID, store, flags.NoCache)
		if merr == nil {
			for _, f := range meta.Fields {
				if !f.Required {
					continue
				}
				// summary and issuetype are always supplied by this command.
				if f.FieldID == "summary" || f.FieldID == "issuetype" {
					continue
				}
				// Check whether the caller supplied this field.
				supplied := false
				switch f.FieldID {
				case "description":
					supplied = flags.Description != ""
				case "priority":
					supplied = flags.Priority != ""
				case "assignee":
					supplied = flags.Assignee != ""
				case "components":
					supplied = len(flags.Components) > 0
				case "labels":
					supplied = len(flags.Labels) > 0
				case "fixVersions":
					supplied = len(flags.FixVersions) > 0
				default:
					// Check custom flags.
					for _, kv := range flags.Custom {
						if idx := strings.Index(kv, "="); idx > 0 {
							if strings.EqualFold(kv[:idx], f.Name) || strings.EqualFold(kv[:idx], f.FieldID) {
								supplied = true
								break
							}
						}
					}
				}
				if supplied {
					continue
				}
				var hint string
				if len(f.AllowedValues) > 0 && len(f.AllowedValues) <= 10 {
					var vals []string
					for _, av := range f.AllowedValues {
						name := av.Name
						if name == "" {
							name = av.Value
						}
						vals = append(vals, name)
					}
					hint = fmt.Sprintf(" (allowed: %s)", strings.Join(vals, ", "))
				}
				validation = append(validation, jira.ValidationRow{
					Status:  "✗",
					Message: fmt.Sprintf("required field %q is missing%s", f.Name, hint),
				})
			}
		}
	}

	// Validate priority.
	if flags.Priority != "" {
		priorities, _, _ := client.ListProjectPriorities(ctx, flags.Project, store, flags.NoCache)
		found := false
		for _, p := range priorities {
			if strings.EqualFold(p.Name, flags.Priority) {
				found = true
				break
			}
		}
		if found {
			validation = append(validation, jira.ValidationRow{
				Status:  "✓",
				Message: fmt.Sprintf("priority %q valid", flags.Priority),
			})
		} else {
			validation = append(validation, jira.ValidationRow{
				Status:  "✗",
				Message: fmt.Sprintf("priority %q not in %s scheme — run: jiracli lookup priorities --project %s", flags.Priority, flags.Project, flags.Project),
			})
		}
	}

	// Resolve assignee.
	var resolvedAssignee string
	if flags.Assignee != "" {
		if flags.Assignee == "me" {
			u, uerr := client.Myself(ctx)
			if uerr == nil {
				resolvedAssignee = u.Name
				validation = append(validation, jira.ValidationRow{
					Status:  "✓",
					Message: fmt.Sprintf("assignee resolved to %s (%s)", u.DisplayName, u.Name),
				})
			} else {
				validation = append(validation, jira.ValidationRow{
					Status:  "⚠",
					Message: "could not resolve 'me' — will attempt to set assignee as-is",
				})
				resolvedAssignee = "me"
			}
		} else {
			users, _ := client.SearchAssignableUsers(ctx, flags.Project, flags.Assignee, 5)
			switch len(users) {
			case 1:
				resolvedAssignee = users[0].Name
				validation = append(validation, jira.ValidationRow{
					Status:  "✓",
					Message: fmt.Sprintf("assignee resolved to %s (%s)", users[0].DisplayName, users[0].Name),
				})
			case 0:
				validation = append(validation, jira.ValidationRow{
					Status:  "✗",
					Message: fmt.Sprintf("assignee %q not found — run: jiracli lookup users %s --project %s", flags.Assignee, flags.Assignee, flags.Project),
				})
			default:
				resolvedAssignee = users[0].Name
				validation = append(validation, jira.ValidationRow{
					Status:  "⚠",
					Message: fmt.Sprintf("assignee %q matched multiple users, using %s (%s)", flags.Assignee, users[0].DisplayName, users[0].Name),
				})
			}
		}
	}

	// Build the POST body.
	fields := map[string]any{
		"project":   map[string]string{"key": flags.Project},
		"issuetype": map[string]string{"name": flags.Type},
		"summary":   flags.Summary,
	}
	if flags.Description != "" {
		fields["description"] = flags.Description
	}
	if flags.Priority != "" {
		fields["priority"] = map[string]string{"name": flags.Priority}
	}
	if resolvedAssignee != "" {
		fields["assignee"] = map[string]string{"name": resolvedAssignee}
	}
	if flags.Epic != "" {
		epicFieldID := entry.Hierarchy.EpicLinkField
		if epicFieldID == "" {
			epicFieldID = "customfield_10014" // fallback default
		}
		fields[epicFieldID] = flags.Epic
		validation = append(validation, jira.ValidationRow{
			Status:  "✓",
			Message: fmt.Sprintf("epic %s (via %s)", flags.Epic, epicFieldID),
		})
	}
	if len(flags.Components) > 0 {
		comps := make([]map[string]string, 0, len(flags.Components))
		for _, comp := range flags.Components {
			comps = append(comps, map[string]string{"name": comp})
		}
		fields["components"] = comps
	}
	if len(flags.Labels) > 0 {
		fields["labels"] = flags.Labels
	}
	if len(flags.FixVersions) > 0 {
		fv := make([]map[string]string, 0, len(flags.FixVersions))
		for _, v := range flags.FixVersions {
			fv = append(fv, map[string]string{"name": v})
		}
		fields["fixVersions"] = fv
	}
	// Resolve and apply custom fields.
	for _, kv := range flags.Custom {
		idx := strings.Index(kv, "=")
		if idx < 0 {
			continue
		}
		name, val := kv[:idx], kv[idx+1:]
		id, _, ferr := client.ResolveFieldID(ctx, name, store, flags.NoCache)
		if ferr == nil {
			fields[id] = val
		} else {
			fields[name] = val
		}
	}

	body := map[string]any{"fields": fields}
	p := jira.Preview{
		Method:      "POST",
		Path:        "/issue",
		Body:        body,
		Description: fmt.Sprintf("create %s/%s: %s", flags.Project, flags.Type, flags.Summary),
		Validation:  validation,
	}

	return HandleWrite(ctx, client, entry.URL, p, flags.Yes, func(resp []byte) string {
		var r struct {
			Key string `json:"key"`
		}
		json.Unmarshal(resp, &r) //nolint:errcheck
		return fmt.Sprintf("✓ created %s: %s\n  → jiracli show %s\n", r.Key, flags.Summary, r.Key)
	})
}
