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
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"})
	if err != nil {
		t.Fatalf("AddIssuesToEpic: %v", err)
	}
	if issues, ok := gotBody["issues"]; !ok || len(issues) != 1 || issues[0] != "PROJ-2" {
		t.Errorf("request body issues = %v, want [PROJ-2]", gotBody["issues"])
	}
}

func TestAddIssuesToEpic_errorWrapsServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"errorMessages":["epic not found"],"errors":{}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	err := c.AddIssuesToEpic(context.Background(), "EPIC-1", []string{"PROJ-2"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "EPIC-1") {
		t.Errorf("error %q should mention epic key", err.Error())
	}
}
