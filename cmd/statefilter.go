package cmd

import (
	"fmt"
	"strings"

	"github.com/cgrossde/jiracli/internal/jira"
)

// stateCategory maps a --state value to a Jira status-category name.
// "" and "all" map to "" (no category filter). Unknown values are an error.
func stateCategory(state string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "all":
		return "", nil
	case "todo", "to-do":
		return "To Do", nil
	case "in-progress", "inprogress":
		return "In Progress", nil
	case "done":
		return "Done", nil
	default:
		return "", fmt.Errorf("unknown --state %q — valid values: todo, in-progress, done, all", state)
	}
}

// resolveStateFilter builds a jira.ChildFilter from the unified filter flags
// shared by `hierarchy` and `effort`.
//
//   - --state todo|in-progress|done keeps only that status category.
//   - --state all shows everything (overrides --exclude-done / --open).
//   - --open is an alias for --exclude-done; --state takes precedence.
func resolveStateFilter(state string, excludeDone, open bool) (jira.ChildFilter, error) {
	cat, err := stateCategory(state)
	if err != nil {
		return jira.ChildFilter{}, err
	}
	if cat != "" {
		return jira.ChildFilter{Category: cat, Label: "--state " + strings.ToLower(strings.TrimSpace(state))}, nil
	}
	// state is "" or "all".
	if strings.EqualFold(strings.TrimSpace(state), "all") {
		return jira.ChildFilter{}, nil
	}
	if open {
		return jira.ChildFilter{ExcludeDone: true, Label: "--open"}, nil
	}
	if excludeDone {
		return jira.ChildFilter{ExcludeDone: true, Label: "--exclude-done"}, nil
	}
	return jira.ChildFilter{}, nil
}
