package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestIssue_FieldsAndFieldsOnlyMutex(t *testing.T) {
	flags := IssueFlags{
		Fields:     "description",
		FieldsOnly: "key,summary,status",
	}
	_, err := Issue(context.Background(), flags, "ACME-1")
	if err == nil {
		t.Fatal("expected error for --fields + --fields-only, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in error, got: %v", err)
	}
}
