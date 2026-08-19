package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAddIssuesToEpic_success(t *testing.T) {
	var gotBody map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/rest/agile/1.0/epic/EPIC-1/issue" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"}, "")
	if err != nil {
		t.Fatalf("AddIssuesToEpic: %v", err)
	}
	if issues, ok := gotBody["issues"]; !ok || len(issues) != 1 || issues[0] != "PROJ-2" {
		t.Errorf("request body issues = %v, want [PROJ-2]", gotBody["issues"])
	}
}

func TestAddIssuesToEpic_errorNoFallbackWrapsServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":["epic not found"],"errors":{}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	// Empty epicLinkFieldID → no PUT fallback; the Agile error surfaces directly.
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"}, "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "EPIC-1") {
		t.Errorf("error %q should mention epic key", err.Error())
	}
}

// When the Agile endpoint rejects the link with a screen-validation 400, the
// client falls back to PUT /issue/{key} with the resolved Epic Link field id.
func TestAddIssuesToEpic_putFallbackOnAgile400(t *testing.T) {
	var putBody map[string]map[string]string
	agileCalled, putCalled := false, false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/rest/agile/1.0/epic/EPIC-1/issue":
			agileCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errorMessages":[],"errors":{"customfield_10014":"Field 'customfield_10014' cannot be set. It is not on the appropriate screen, or unknown."}}`)) //nolint:errcheck
		case r.Method == "PUT" && r.URL.Path == "/rest/api/2/issue/PROJ-2":
			putCalled = true
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"}, "customfield_10014")
	if err != nil {
		t.Fatalf("AddIssuesToEpic with fallback: %v", err)
	}
	if !agileCalled || !putCalled {
		t.Fatalf("expected both Agile POST and PUT fallback; agile=%v put=%v", agileCalled, putCalled)
	}
	if got := putBody["fields"]["customfield_10014"]; got != "EPIC-1" {
		t.Errorf("PUT fields.customfield_10014 = %q, want EPIC-1", got)
	}
}

// When the Agile endpoint fails AND the PUT fallback also fails, the aggregate
// error names the failing issue key.
func TestAddIssuesToEpic_putFallbackAlsoFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":[],"errors":{"customfield_10014":"cannot be set"}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"}, "customfield_10014")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "PROJ-2") {
		t.Errorf("error %q should name the failing issue key", err.Error())
	}
}
