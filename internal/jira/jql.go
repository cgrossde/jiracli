package jira

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reStatusClause     = regexp.MustCompile(`(?i)\bstatus(category)?\s*(=|!=|in\b|not\s+in\b)`)
	reResolutionClause = regexp.MustCompile(`(?i)\bresolution\s*(=|!=|is\b|in\b|not\s+in\b)`)
)

// DefaultOpenFilter wraps userJQL with `statusCategory != "Done"` unless
// the user already mentions status/statusCategory/resolution.
func DefaultOpenFilter(userJQL string) string {
	if reStatusClause.MatchString(userJQL) || reResolutionClause.MatchString(userJQL) {
		return userJQL
	}
	if strings.TrimSpace(userJQL) == "" {
		return `statusCategory != "Done"`
	}
	return fmt.Sprintf(`(%s) AND statusCategory != "Done"`, userJQL)
}
