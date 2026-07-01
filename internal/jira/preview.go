package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationRow is a single validation entry in a Preview.
type ValidationRow struct {
	Status  string // "✓", "⚠", or "✗"
	Message string
}

// Preview describes a write operation before it is executed.
type Preview struct {
	Method      string          // "POST", "PUT", or "DELETE"
	Path        string          // path after /rest/api/2, e.g. "/issue/ACME-123/comment"
	Body        any             // marshalled to JSON for display (nil for DELETE)
	Description string          // human-readable effect, e.g. "+ 1 comment on ACME-123 by Alex Chen"
	Validation  []ValidationRow // ✓/⚠/✗ rows
}

// Render returns the proposal §4.3 preview block as a string.
func (p Preview) Render(baseURL string) string {
	var sb strings.Builder

	sb.WriteString("DRY RUN — no changes made.\n\n")
	sb.WriteString(fmt.Sprintf("%s %s/rest/api/2%s\n\n", p.Method, baseURL, p.Path))

	if p.Body != nil {
		sb.WriteString("Body:\n")
		raw, _ := json.MarshalIndent(p.Body, "", "  ")
		lines := strings.Split(string(raw), "\n")
		if len(lines) > 40 {
			lines = lines[:40]
			lines = append(lines, "  … (truncated)")
		}
		for _, l := range lines {
			sb.WriteString("  " + l + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Effect:\n")
	sb.WriteString("  " + p.Description + "\n")

	if len(p.Validation) > 0 {
		sb.WriteString("\nValidation:\n")
		for _, v := range p.Validation {
			sb.WriteString(fmt.Sprintf("  %s %s\n", v.Status, v.Message))
		}
	}

	return sb.String()
}

// Execute sends the preview's request via the client.
// p.Path must be the path segment after /rest/api/2 (the client prepends the base).
func (p Preview) Execute(ctx context.Context, c *Client) ([]byte, error) {
	raw, _ := json.Marshal(p.Body)
	switch p.Method {
	case "POST":
		body, status, err := c.Post(ctx, p.Path, nil, bytes.NewReader(raw), "application/json")
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("%w", MapStatus("", status, body))
		}
		return body, nil
	case "DELETE":
		body, status, err := c.Delete(ctx, p.Path, nil)
		if err != nil {
			return nil, err
		}
		if status != 204 && status != 200 {
			return nil, fmt.Errorf("%w", MapStatus("", status, body))
		}
		return body, nil
	default: // PUT
		body, status, err := c.Put(ctx, p.Path, nil, bytes.NewReader(raw), "application/json")
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("%w", MapStatus("", status, body))
		}
		return body, nil
	}
}
