package cache

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/cgrossde/jiracli/internal/keychain"
)

// newTestStore returns a Store backed by a temporary directory.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Root: t.TempDir()}
}

// TestPutGet_hit verifies that a value written with Put is returned by Get
// when retrieved within the TTL window.
func TestPutGet_hit(t *testing.T) {
	s := newTestStore(t)
	want := map[string]string{"hello": "world"}
	if err := s.Put("fields", want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var got map[string]string
	if err := s.Get("fields", time.Hour, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("got %v, want {hello:world}", got)
	}
}

// TestPutGet_expired verifies that Get returns ErrMiss when the TTL is zero
// (i.e. the entry is always considered expired).
func TestPutGet_expired(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("fields", "value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var out string
	err := s.Get("fields", 0, &out)
	if !errors.Is(err, ErrMiss) {
		t.Errorf("expected ErrMiss, got %v", err)
	}
}

// TestDelete verifies that Get returns ErrMiss after the entry is deleted.
func TestDelete(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("fields", 42); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete("fields"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var out int
	if err := s.Get("fields", time.Hour, &out); !errors.Is(err, ErrMiss) {
		t.Errorf("expected ErrMiss after delete, got %v", err)
	}
}

// TestDelete_missing verifies that Delete is a no-op on an absent key.
func TestDelete_missing(t *testing.T) {
	s := newTestStore(t)
	if err := s.Delete("nonexistent"); err != nil {
		t.Errorf("Delete on missing key should be no-op, got %v", err)
	}
}

// TestDeleteGlob verifies that only matching keys are removed.
func TestDeleteGlob(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("project/ACME", "acme"); err != nil {
		t.Fatalf("Put project/ACME: %v", err)
	}
	if err := s.Put("fields", "all-fields"); err != nil {
		t.Fatalf("Put fields: %v", err)
	}

	if err := s.DeleteGlob("project/*"); err != nil {
		t.Fatalf("DeleteGlob: %v", err)
	}

	// project/ACME must be gone.
	var v string
	if err := s.Get("project/ACME", time.Hour, &v); !errors.Is(err, ErrMiss) {
		t.Errorf("expected ErrMiss for project/ACME after glob delete, got %v", err)
	}

	// fields must still be present.
	if err := s.Get("fields", time.Hour, &v); err != nil {
		t.Errorf("fields should survive glob delete, got %v", err)
	}
}

// TestList verifies that List returns an entry for every key that was Put.
func TestList(t *testing.T) {
	s := newTestStore(t)
	keys := []string{"fields", "project/ACME"}
	for _, k := range keys {
		if err := s.Put(k, k+"-value"); err != nil {
			t.Fatalf("Put %q: %v", k, err)
		}
	}

	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != len(keys) {
		t.Fatalf("List returned %d entries, want %d", len(entries), len(keys))
	}

	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Key
		if e.SavedAt.IsZero() {
			t.Errorf("entry %q has zero SavedAt", e.Key)
		}
	}
	sort.Strings(got)
	sort.Strings(keys)
	for i := range keys {
		if got[i] != keys[i] {
			t.Errorf("List[%d] = %q, want %q", i, got[i], keys[i])
		}
	}
}

// TestList_emptyRoot verifies that List on a non-existent root returns nil, nil.
func TestList_emptyRoot(t *testing.T) {
	s := &Store{Root: t.TempDir() + "/nonexistent"}
	entries, err := s.List()
	if err != nil {
		t.Errorf("List on missing root should return nil error, got %v", err)
	}
	if entries != nil {
		t.Errorf("List on missing root should return nil slice, got %v", entries)
	}
}

// TestProfileHash_stable verifies determinism and collision-resistance.
func TestProfileHash_stable(t *testing.T) {
	e1 := keychain.Entry{Profile: "default", URL: "https://jira.example.com"}
	e2 := keychain.Entry{Profile: "default", URL: "https://jira.example.com"}
	e3 := keychain.Entry{Profile: "staging", URL: "https://jira.example.com"}
	e4 := keychain.Entry{Profile: "default", URL: "https://other.example.com"}

	h1 := ProfileHash(e1)
	h2 := ProfileHash(e2)
	if h1 != h2 {
		t.Errorf("same inputs produced different hashes: %q vs %q", h1, h2)
	}
	if ProfileHash(e3) == h1 {
		t.Errorf("different profile should produce different hash")
	}
	if ProfileHash(e4) == h1 {
		t.Errorf("different URL should produce different hash")
	}
	// Hash should be 8 hex chars (4 bytes × 2).
	if len(h1) != 8 {
		t.Errorf("ProfileHash length = %d, want 8", len(h1))
	}
}
