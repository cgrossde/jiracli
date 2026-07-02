package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListProjectPriorities_fallsBackOn404 verifies the documented fallback:
// DC versions too old (or with the feature disabled) return 404 from the
// priority-scheme endpoint, and the client should fall back to the global
// priority list rather than erroring out.
func TestListProjectPriorities_fallsBackOn404(t *testing.T) {
	want := []Priority{{ID: "1", Name: "High"}, {ID: "2", Name: "Low"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "priorityscheme"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/priority"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(want)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, fallback, err := c.ListProjectPriorities(context.Background(), "MOB", nil, true)
	if err != nil {
		t.Fatalf("ListProjectPriorities: %v", err)
	}
	if !fallback {
		t.Errorf("fallback = false, want true on 404")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d priorities, want %d", len(got), len(want))
	}
}

// TestListProjectPriorities_fallsBackOn403 is a regression test: many real
// users lack "Administer Projects" and get a 403 (not a 404) from the
// priority-scheme endpoint. This must fall back to the global list instead
// of propagating a hard "access denied" error.
func TestListProjectPriorities_fallsBackOn403(t *testing.T) {
	want := []Priority{{ID: "1", Name: "High"}, {ID: "2", Name: "Medium"}, {ID: "3", Name: "Low"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "priorityscheme"):
			w.WriteHeader(http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/priority"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(want)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, fallback, err := c.ListProjectPriorities(context.Background(), "MOB", nil, true)
	if err != nil {
		t.Fatalf("ListProjectPriorities returned error on 403, want fallback: %v", err)
	}
	if !fallback {
		t.Errorf("fallback = false, want true on 403")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d priorities, want %d", len(got), len(want))
	}
}

// TestListProjectPriorities_fallsBackOn403_atPrioritiesStep covers the case
// where the scheme lookup succeeds (200) but the caller is forbidden from
// reading the scheme's priorities themselves.
func TestListProjectPriorities_fallsBackOn403_atPrioritiesStep(t *testing.T) {
	want := []Priority{{ID: "1", Name: "High"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/project/MOB/priorityscheme"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": 10})
		case strings.Contains(r.URL.Path, "/priorityscheme/10/priorities"):
			w.WriteHeader(http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/priority"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(want)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, fallback, err := c.ListProjectPriorities(context.Background(), "MOB", nil, true)
	if err != nil {
		t.Fatalf("ListProjectPriorities: %v", err)
	}
	if !fallback {
		t.Errorf("fallback = false, want true on 403 at priorities step")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d priorities, want %d", len(got), len(want))
	}
}

// TestListProjectPriorities_happyPath verifies the normal (non-fallback)
// resolution: scheme lookup succeeds, then its priorities are fetched.
func TestListProjectPriorities_happyPath(t *testing.T) {
	want := []Priority{{ID: "5", Name: "Blocker"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/project/MOB/priorityscheme"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": 42})
		case strings.Contains(r.URL.Path, "/priorityscheme/42/priorities"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"priorities": want})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, fallback, err := c.ListProjectPriorities(context.Background(), "MOB", nil, true)
	if err != nil {
		t.Fatalf("ListProjectPriorities: %v", err)
	}
	if fallback {
		t.Errorf("fallback = true, want false on happy path")
	}
	if len(got) != 1 || got[0].Name != "Blocker" {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestListProjectPriorities_hardErrorOn500 verifies that non-403/404 errors
// still propagate as hard failures (only 403/404 trigger the fallback).
func TestListProjectPriorities_hardErrorOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, _, err := c.ListProjectPriorities(context.Background(), "MOB", nil, true)
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
