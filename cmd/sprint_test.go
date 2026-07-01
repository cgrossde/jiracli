package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/jira"
	"github.com/cgrossde/jiracli/internal/keychain"
)

// mustSprintIssue builds a jira.IssueRaw from JSON so tests don't have to
// reconstruct the anonymous Fields struct by hand.
func mustSprintIssue(t *testing.T, body string) jira.IssueRaw {
	t.Helper()
	var raw jira.IssueRaw
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return raw
}

func TestFilterCurrentSprintIssues_Assigned(t *testing.T) {
	entry := keychain.Entry{User: "alice", DisplayName: "Alice Example"}
	client := jira.New(entry) // not used: identity resolves from entry, no network
	issues := []jira.IssueRaw{
		mustSprintIssue(t, `{"key":"X-1","fields":{"assignee":{"name":"alice","displayName":"Alice Example"},"status":{"statusCategory":{"key":"indeterminate"}}}}`),
		mustSprintIssue(t, `{"key":"X-2","fields":{"assignee":{"name":"bob","displayName":"Bob"},"status":{"statusCategory":{"key":"new"}}}}`),
		mustSprintIssue(t, `{"key":"X-3","fields":{"status":{"statusCategory":{"key":"new"}}}}`), // unassigned
	}
	flags := sprintCurrentFlags{Assigned: true}
	got := filterCurrentSprintIssues(context.Background(), client, entry, flags, issues)
	if len(got) != 1 || got[0].Key != "X-1" {
		t.Fatalf("expected only X-1, got %+v", keysOf(got))
	}
}

func TestFilterCurrentSprintIssues_ExcludeDone(t *testing.T) {
	entry := keychain.Entry{User: "alice"}
	client := jira.New(entry)
	issues := []jira.IssueRaw{
		mustSprintIssue(t, `{"key":"X-1","fields":{"status":{"statusCategory":{"key":"done"}}}}`),
		mustSprintIssue(t, `{"key":"X-2","fields":{"status":{"statusCategory":{"key":"indeterminate"}}}}`),
		mustSprintIssue(t, `{"key":"X-3","fields":{"status":{"statusCategory":{"key":"Done"}}}}`), // case-insensitive
	}
	flags := sprintCurrentFlags{ExcludeDone: true}
	got := filterCurrentSprintIssues(context.Background(), client, entry, flags, issues)
	if len(got) != 1 || got[0].Key != "X-2" {
		t.Fatalf("expected only X-2, got %v", keysOf(got))
	}
}

func TestFilterCurrentSprintIssues_AssignedAndExcludeDone(t *testing.T) {
	entry := keychain.Entry{User: "alice"}
	client := jira.New(entry)
	issues := []jira.IssueRaw{
		mustSprintIssue(t, `{"key":"X-1","fields":{"assignee":{"name":"alice"},"status":{"statusCategory":{"key":"done"}}}}`),
		mustSprintIssue(t, `{"key":"X-2","fields":{"assignee":{"name":"alice"},"status":{"statusCategory":{"key":"indeterminate"}}}}`),
		mustSprintIssue(t, `{"key":"X-3","fields":{"assignee":{"name":"bob"},"status":{"statusCategory":{"key":"indeterminate"}}}}`),
	}
	flags := sprintCurrentFlags{Assigned: true, ExcludeDone: true}
	got := filterCurrentSprintIssues(context.Background(), client, entry, flags, issues)
	if len(got) != 1 || got[0].Key != "X-2" {
		t.Fatalf("expected only X-2, got %v", keysOf(got))
	}
}

func keysOf(issues []jira.IssueRaw) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.Key
	}
	return out
}

func TestRenderSprintList_JSONPaginationRealTotal(t *testing.T) {
	flags := sprintListFlags{JSON: true, Board: 1}
	sprints := []jira.Sprint{{ID: 1, Name: "S1"}, {ID: 2, Name: "S2"}}
	out := renderSprintList(flags, sprints, false /*isLast*/, 1, 50, 120)
	last := lastTrailer(out)
	if !strings.Contains(last, `"_pagination"`) {
		t.Fatalf("expected pagination trailer, got: %q", last)
	}
	for _, want := range []string{`"total":120`, `"pages":3`, `"next_page":2`, `"has_more":true`} {
		if !strings.Contains(last, want) {
			t.Errorf("trailer missing %s: %q", want, last)
		}
	}
}

func TestRenderSprintList_JSONPaginationUnknownTotal(t *testing.T) {
	flags := sprintListFlags{JSON: true, Board: 1}
	sprints := []jira.Sprint{{ID: 1, Name: "S1"}}
	out := renderSprintList(flags, sprints, false /*isLast*/, 1, 50, -1)
	last := lastTrailer(out)
	if !strings.Contains(last, `"has_more":true`) {
		t.Fatalf("expected has_more trailer, got: %q", last)
	}
	if strings.Contains(last, `"total"`) || strings.Contains(last, `"pages"`) {
		t.Errorf("unknown-total trailer must omit total/pages, got: %q", last)
	}
}

func TestRenderSprintList_JSONNoTrailerOnLastPage(t *testing.T) {
	flags := sprintListFlags{JSON: true, Board: 1}
	sprints := []jira.Sprint{{ID: 1, Name: "S1"}}
	out := renderSprintList(flags, sprints, true /*isLast*/, 1, 50, 1)
	if strings.Contains(out, `"_pagination"`) {
		t.Errorf("expected no pagination trailer on last page, got: %q", out)
	}
}

// lastTrailer returns the final non-empty line of an NDJSON string.
func lastTrailer(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
