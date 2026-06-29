package jira

import (
	"errors"
	"testing"
)

func TestParseRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Ref
		wantErr bool
	}{
		// ── Valid: plain key ─────────────────────────────────────────────────
		{
			name:  "plain key",
			input: "ACME-123",
			want:  Ref{Key: "ACME-123", Kind: RefIssue},
		},
		{
			name:  "project with digits and underscore",
			input: "MY_PROJECT2-999",
			want:  Ref{Key: "MY_PROJECT2-999", Kind: RefIssue},
		},
		{
			name:  "large issue number",
			input: "PROJ-100000",
			want:  Ref{Key: "PROJ-100000", Kind: RefIssue},
		},

		// ── Valid: comment colon form ─────────────────────────────────────────
		{
			name:  "comment ref",
			input: "ACME-123:comment:456",
			want:  Ref{Key: "ACME-123", Kind: RefComment, CommentID: "456"},
		},
		{
			name:  "comment ref case-insensitive prefix",
			input: "ACME-123:COMMENT:789",
			want:  Ref{Key: "ACME-123", Kind: RefComment, CommentID: "789"},
		},
		{
			name:  "comment ref mixed case prefix",
			input: "ACME-123:Comment:101",
			want:  Ref{Key: "ACME-123", Kind: RefComment, CommentID: "101"},
		},

		// ── Valid: attachment colon form ──────────────────────────────────────
		{
			name:  "attach ref",
			input: "ACME-123:attach:789",
			want:  Ref{Key: "ACME-123", Kind: RefAttachment, AttachmentID: "789"},
		},
		{
			name:  "attach ref case-insensitive prefix",
			input: "ACME-123:ATTACH:321",
			want:  Ref{Key: "ACME-123", Kind: RefAttachment, AttachmentID: "321"},
		},
		{
			name:  "attach ref mixed case prefix",
			input: "ACME-123:Attach:42",
			want:  Ref{Key: "ACME-123", Kind: RefAttachment, AttachmentID: "42"},
		},

		// ── Valid: browse URL (https) ─────────────────────────────────────────
		{
			name:  "https browse URL",
			input: "https://jira.example.com/browse/ACME-123",
			want:  Ref{Key: "ACME-123", Kind: RefIssue},
		},
		{
			name:  "http browse URL",
			input: "http://jira.internal/browse/PROJ-7",
			want:  Ref{Key: "PROJ-7", Kind: RefIssue},
		},
		{
			name:  "browse URL with query string ignored (focusedCommentId NOT parsed)",
			input: "https://jira.example.com/browse/ACME-123?focusedCommentId=456",
			want:  Ref{Key: "ACME-123", Kind: RefIssue},
		},
		{
			name:  "browse URL with fragment ignored",
			input: "https://jira.example.com/browse/ACME-123#top",
			want:  Ref{Key: "ACME-123", Kind: RefIssue},
		},
		{
			name:  "browse URL with path prefix (e.g. context root)",
			input: "https://jira.example.com/jira/browse/ACME-123",
			want:  Ref{Key: "ACME-123", Kind: RefIssue},
		},

		// ── Invalid inputs ────────────────────────────────────────────────────
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "lowercase key",
			input:   "acme-123",
			wantErr: true,
		},
		{
			name:    "missing number part",
			input:   "ACME-",
			wantErr: true,
		},
		{
			name:    "no dash in key",
			input:   "ACME123",
			wantErr: true,
		},
		{
			name:  "link ref",
			input: "ACME-123:link:456",
			want:  Ref{Key: "ACME-123", Kind: RefLink, LinkID: "456"},
		},
		{
			name:  "link ref case-insensitive prefix",
			input: "ACME-123:LINK:789",
			want:  Ref{Key: "ACME-123", Kind: RefLink, LinkID: "789"},
		},
		{
			name:    "comment ref missing id",
			input:   "ACME-123:comment:",
			wantErr: true,
		},
		{
			name:    "attach ref missing id",
			input:   "ACME-123:attach:",
			wantErr: true,
		},
		{
			name:    "link ref missing id",
			input:   "ACME-123:link:",
			wantErr: true,
		},
		{
			name:    "only two colon parts",
			input:   "ACME-123:comment",
			wantErr: true,
		},
		{
			name:    "browse URL without browse segment",
			input:   "https://jira.example.com/issues/ACME-123",
			wantErr: true,
		},
		{
			name:    "browse URL with invalid key",
			input:   "https://jira.example.com/browse/acme-123",
			wantErr: true,
		},
		{
			name:    "random string",
			input:   "not-a-ref",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRef(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRef(%q) = %+v, nil; want error", tc.input, got)
				}
				if !errors.Is(err, ErrInvalidRef) {
					t.Fatalf("ParseRef(%q) error = %v; want errors.Is(err, ErrInvalidRef)", tc.input, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseRef(%q)\n  got  %+v\n  want %+v", tc.input, got, tc.want)
			}
		})
	}
}

// TestParseRef_KeyPreservation verifies that key casing is preserved exactly.
func TestParseRef_KeyPreservation(t *testing.T) {
	t.Parallel()
	ref, err := ParseRef("MYPROJ-42")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Key != "MYPROJ-42" {
		t.Errorf("Key = %q, want %q", ref.Key, "MYPROJ-42")
	}
}

// TestParseRef_ErrWraps confirms ErrInvalidRef is always wrapped, not replaced.
func TestParseRef_ErrWraps(t *testing.T) {
	t.Parallel()
	_, err := ParseRef("bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidRef) {
		t.Errorf("error %v does not wrap ErrInvalidRef", err)
	}
	// The error message must contain the original input.
	if msg := err.Error(); msg == "" {
		t.Error("error message is empty")
	}
}
