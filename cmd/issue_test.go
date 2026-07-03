package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
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

// ── unknownFieldTokens / --fields typo detection ─────────────────────────────

// fakeFieldsServer serves GET /rest/api/2/field with a fixed field list.
func fakeFieldsServer(t *testing.T, fields []jira.Field) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/field" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(fields)
	}))
}

func fakeFieldsClient(serverURL string) *jira.Client {
	return jira.New(keychain.Entry{Profile: "test", URL: serverURL, PAT: "test-pat"})
}

func TestUnknownFieldTokens_KnownNamesPass(t *testing.T) {
	srv := fakeFieldsServer(t, []jira.Field{
		{ID: "duedate", Name: "Due Date"},
		{ID: "customfield_10031", Name: "Story Points"},
	})
	defer srv.Close()
	client := fakeFieldsClient(srv.URL)

	// "reporter" is a default field, "duedate" is a real field id,
	// "Story Points" resolves by display name, "customfield_10099" looks
	// like a raw id and is trusted without a round-trip.
	got := unknownFieldTokens(context.Background(), client, nil, jira.DefaultIssueFields,
		"+duedate,Story Points,-reporter,customfield_10099")
	if len(got) != 0 {
		t.Errorf("expected no unknown fields, got: %v", got)
	}
}

func TestUnknownFieldTokens_GarbageNameFlagged(t *testing.T) {
	srv := fakeFieldsServer(t, []jira.Field{
		{ID: "duedate", Name: "Due Date"},
	})
	defer srv.Close()
	client := fakeFieldsClient(srv.URL)

	got := unknownFieldTokens(context.Background(), client, nil, jira.DefaultIssueFields, "ünïcödé,🎉,duedate")
	if len(got) != 2 {
		t.Fatalf("expected 2 unknown fields, got: %v", got)
	}
	if got[0] != "ünïcödé" || got[1] != "🎉" {
		t.Errorf("unexpected unknown field set: %v", got)
	}
}

func TestUnknownFieldsWarning_Format(t *testing.T) {
	msg := unknownFieldsWarning([]string{"ünïcödé", "🎉"})
	if !strings.Contains(msg, "⚠") {
		t.Errorf("expected warning glyph, got: %q", msg)
	}
	if !strings.Contains(msg, `"ünïcödé"`) || !strings.Contains(msg, `"🎉"`) {
		t.Errorf("expected both unknown field names quoted, got: %q", msg)
	}
	if !strings.Contains(msg, "jiracli lookup fields") {
		t.Errorf("expected corrective hint, got: %q", msg)
	}
}

func TestIsCustomFieldID(t *testing.T) {
	cases := map[string]bool{
		"customfield_10031": true,
		"customfield_":      false,
		"customfield_abc":   false,
		"CUSTOMFIELD_10031": false, // caller lowercases before checking
		"labels":            false,
	}
	for input, want := range cases {
		if got := isCustomFieldID(input); got != want {
			t.Errorf("isCustomFieldID(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestRenderIssue_ComponentsLabelsTwoColumn verifies Components/Labels
// render side by side when Components fits the value column, and each gets
// its own full-width line when Components is too wide to share a row.
func TestRenderIssue_ComponentsLabelsTwoColumn(t *testing.T) {
	fieldSet := map[string]bool{} // fieldSet=nil means default set; use empty non-nil to suppress unrelated blocks
	base := jira.IssueRecord{Key: "ACME-1", Summary: "s", IssueType: "Task", Status: "Open"}

	t.Run("fits on one row", func(t *testing.T) {
		rec := base
		rec.Components = []string{"ZTP"}
		rec.Labels = []string{"Internal"}
		out := renderIssue(rec, IssueFlags{}, fieldSet, "")
		if !strings.Contains(out, "Components:") || !strings.Contains(out, "Labels:") {
			t.Fatalf("expected both Components and Labels labels in output:\n%s", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "Components:") {
				if !strings.Contains(line, "Labels:") {
					t.Errorf("expected Components and Labels on the same line, got: %q", line)
				}
			}
		}
	})

	t.Run("falls back to separate lines when components is too wide", func(t *testing.T) {
		rec := base
		rec.Components = []string{"A very long component name that exceeds the value column width"}
		rec.Labels = []string{"Internal"}
		out := renderIssue(rec, IssueFlags{}, fieldSet, "")
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "Components:") && strings.Contains(line, "Labels:") {
				t.Errorf("expected Components and Labels on separate lines when Components is wide, got: %q", line)
			}
		}
		if !strings.Contains(out, "Components:") || !strings.Contains(out, "Labels:") {
			t.Fatalf("expected both Components and Labels labels in output:\n%s", out)
		}
	})
}

// TestCollectExtraFields_GenericFieldRendering verifies that a built-in
// field with no dedicated renderIssue section (e.g. environment) and a
// custom field are surfaced generically via ExtraFields, while fields that
// already have dedicated handling (duedate) or are consumed by hierarchy
// config are excluded to avoid double-rendering.
func TestCollectExtraFields_GenericFieldRendering(t *testing.T) {
	raw := jira.IssueRaw{
		RawFields: map[string]json.RawMessage{
			"environment":       json.RawMessage(`"PROD_CHANGE"`),
			"duedate":           json.RawMessage(`"2024-01-11"`),
			"customfield_10014": json.RawMessage(`"ACME-1"`), // epic link, excluded via hf
			"customfield_99999": json.RawMessage(`"custom value"`),
			"customfield_absent": json.RawMessage(`null`),
		},
	}
	hf := jira.HierarchyFieldIDs{EpicLink: "customfield_10014"}
	fields := "environment,duedate,customfield_10014,customfield_99999,customfield_absent"

	got := collectExtraFields(raw, fields, hf)

	want := map[string]string{
		"environment":       "PROD_CHANGE",
		"customfield_99999": "custom value",
	}
	if len(got) != len(want) {
		t.Fatalf("collectExtraFields() = %+v, want %d entries matching %+v", got, len(want), want)
	}
	for _, fv := range got {
		wantVal, ok := want[fv.ID]
		if !ok {
			t.Errorf("unexpected extra field %q in result", fv.ID)
			continue
		}
		if fv.Value != wantVal {
			t.Errorf("field %q value = %q, want %q", fv.ID, fv.Value, wantVal)
		}
		if fv.Label == "" {
			t.Errorf("field %q has empty label", fv.ID)
		}
	}
}
