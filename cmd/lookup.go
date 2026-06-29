package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/cgrossde/jiracli/internal/jira"
)

// NewLookupCmd builds the lookup command group.
func NewLookupCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "lookup",
		Short: "Look up Jira metadata: users, labels, components, versions, projects, fields, etc.",
	}
	c.AddCommand(
		newLookupUsersCmd(),
		newLookupLabelsCmd(),
		newLookupComponentsCmd(),
		newLookupVersionsCmd(),
		newLookupProjectsCmd(),
		newLookupIssueTypesCmd(),
		newLookupLinkTypesCmd(),
		newLookupStatusesCmd(),
		newLookupPrioritiesCmd(),
		newLookupFieldsCmd(),
	)
	return c
}

// suggestLabels calls the Jira autocomplete endpoint for label suggestions.
// Returns (labels, truncated, error). truncated is true when the server
// returned exactly 15 results (its cap).
func suggestLabels(ctx context.Context, client *jira.Client, q string) ([]string, bool, error) {
	params := url.Values{}
	params.Set("fieldName", "labels")
	params.Set("fieldValue", q)
	body, status, err := client.Get(ctx, "/jql/autocompletedata/suggestions", params)
	if err != nil {
		return nil, false, err
	}
	if status != 200 {
		return nil, false, fmt.Errorf("label suggestions: HTTP %d", status)
	}
	var resp struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, false, fmt.Errorf("parse label suggestions: %w", err)
	}
	labels := make([]string, len(resp.Results))
	for i, r := range resp.Results {
		labels[i] = r.Value
	}
	return labels, len(labels) == 15, nil
}
