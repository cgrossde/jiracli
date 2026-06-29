package jira

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// RefKind identifies what a Ref points to within a Jira issue.
type RefKind int

const (
	RefIssue      RefKind = iota // plain issue key
	RefComment                   // ACME-123:comment:NNN
	RefAttachment                // ACME-123:attach:NNN
	RefLink                      // ACME-123:link:NNN
)

// Ref is a parsed issue reference.
type Ref struct {
	Key          string // e.g. "ACME-123"
	Kind         RefKind
	CommentID    string // non-empty when Kind == RefComment
	AttachmentID string // non-empty when Kind == RefAttachment
	LinkID       string // non-empty when Kind == RefLink
}

// ErrInvalidRef is returned by ParseRef when the input cannot be parsed.
var ErrInvalidRef = errors.New("invalid issue reference")

// issueKeyRE matches a canonical Jira issue key: project prefix, dash, number.
// The project prefix must start with a letter and may contain uppercase letters,
// digits, and underscores.
var issueKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)

// ParseRef parses an issue reference in any of the forms described in §3:
//
//   - ACME-123                         → RefIssue
//   - ACME-123:comment:NNN             → RefComment,    CommentID="NNN"
//   - ACME-123:attach:NNN              → RefAttachment, AttachmentID="NNN"
//   - ACME-123:link:NNN                → RefLink,       LinkID="NNN"
//   - https://host/browse/ACME-123     → RefIssue (query/fragment ignored)
//
// The sub-prefixes are matched case-insensitively.
// The issue key itself is preserved as-is (no case folding).
//
// URL comment anchors (?focusedCommentId=NNN) are intentionally not parsed;
// use the colon form for comment references.
func ParseRef(input string) (Ref, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, input)
	}

	// Detect URLs by scheme prefix.
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return parseURLRef(trimmed, input)
	}

	// Colon-separated forms: KEY | KEY:comment:ID | KEY:attach:ID | KEY:link:ID
	return parseColonRef(trimmed, input)
}

// parseURLRef handles http(s):// inputs.
func parseURLRef(trimmed, original string) (Ref, error) {
	u, err := url.Parse(trimmed)
	if err != nil {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	// Strip fragment and query; extract the last path segment as the key.
	// A valid browse URL ends with /browse/<KEY>.
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 || segments[len(segments)-2] != "browse" {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	key := segments[len(segments)-1]
	if !issueKeyRE.MatchString(key) {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	return Ref{Key: key, Kind: RefIssue}, nil
}

// parseColonRef handles plain-key and KEY:sub:ID forms.
func parseColonRef(trimmed, original string) (Ref, error) {
	parts := strings.SplitN(trimmed, ":", 3)

	key := parts[0]
	if !issueKeyRE.MatchString(key) {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	// Plain key — no colon suffix.
	if len(parts) == 1 {
		return Ref{Key: key, Kind: RefIssue}, nil
	}

	// Must have exactly three parts: KEY:kind:ID
	if len(parts) != 3 {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	subKind := strings.ToLower(parts[1])
	id := parts[2]

	if id == "" {
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}

	switch subKind {
	case "comment":
		return Ref{Key: key, Kind: RefComment, CommentID: id}, nil
	case "attach":
		return Ref{Key: key, Kind: RefAttachment, AttachmentID: id}, nil
	case "link":
		return Ref{Key: key, Kind: RefLink, LinkID: id}, nil
	default:
		return Ref{}, fmt.Errorf("%w: %q — expected ACME-123, ACME-123:comment:NNN, ACME-123:attach:NNN, ACME-123:link:NNN, or a browse URL", ErrInvalidRef, original)
	}
}
