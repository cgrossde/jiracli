package cache

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cgrossde/jiracli/internal/keychain"
)

// ErrMiss is returned by Get when the entry is absent or expired.
var ErrMiss = errors.New("cache miss")

// Store is a file-backed key/value cache rooted at Root.
type Store struct{ Root string }

// CacheEntry describes a cached item as returned by List.
type CacheEntry struct {
	Key       string
	SavedAt   time.Time
	ExpiresAt time.Time
	Expired   bool
}

// envelope is the on-disk wrapper for every cached value.
type envelope struct {
	SavedAt time.Time       `json:"savedAt"`
	Value   json.RawMessage `json:"value"`
}

// ProfileHash returns an 8-hex-char SHA-256 fingerprint of (profile+"\x00"+URL).
// It namespaces the cache per (profile, instance) without embedding PATs in paths.
func ProfileHash(entry keychain.Entry) string {
	h := sha256.Sum256([]byte(entry.Profile + "\x00" + entry.URL))
	return fmt.Sprintf("%x", h[:4])
}

// NewStore returns a Store rooted at $XDG_CACHE_HOME/jiracli/<hash>/
// (or ~/.cache/jiracli/<hash>/ when XDG_CACHE_HOME is unset).
func NewStore(entry keychain.Entry) *Store {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return &Store{Root: filepath.Join(base, "jiracli", ProfileHash(entry))}
}

// Get reads the cached value for key into out.
// Returns ErrMiss when the entry is absent, corrupt, or older than ttl.
func (s *Store) Get(key string, ttl time.Duration, out any) error {
	data, err := os.ReadFile(s.keyPath(key))
	if err != nil {
		return ErrMiss
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return ErrMiss
	}
	if time.Since(env.SavedAt) > ttl {
		return ErrMiss
	}
	if err := json.Unmarshal(env.Value, out); err != nil {
		return ErrMiss
	}
	return nil
}

// Put serialises value and writes it atomically via temp-file + rename.
// Intermediate directories are created as needed (mode 0755).
func (s *Store) Put(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache.Put marshal: %w", err)
	}
	env := envelope{SavedAt: time.Now().UTC(), Value: json.RawMessage(raw)}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("cache.Put envelope: %w", err)
	}
	path := s.keyPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("cache.Put mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("cache.Put write: %w", err)
	}
	return os.Rename(tmp, path)
}

// Delete removes a single cache entry. No-op if the entry is absent.
func (s *Store) Delete(key string) error {
	err := os.Remove(s.keyPath(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// DeleteGlob removes every entry whose slash-delimited key matches pattern
// (filepath.Match semantics).
func (s *Store) DeleteGlob(pattern string) error {
	entries, err := s.List()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if matched, _ := filepath.Match(pattern, e.Key); matched {
			_ = s.Delete(e.Key)
		}
	}
	return nil
}

// List returns every cached entry under Root.
// Returns nil, nil when Root does not exist yet.
func (s *Store) List() ([]CacheEntry, error) {
	var results []CacheEntry
	err := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rel, _ := filepath.Rel(s.Root, path)
		key := strings.TrimSuffix(strings.ReplaceAll(rel, string(filepath.Separator), "/"), ".json")

		var savedAt time.Time
		if data, rerr := os.ReadFile(path); rerr == nil {
			var env envelope
			if jerr := json.Unmarshal(data, &env); jerr == nil {
				savedAt = env.SavedAt
			}
		}
		if savedAt.IsZero() {
			if info, ierr := d.Info(); ierr == nil {
				savedAt = info.ModTime()
			}
		}
		results = append(results, CacheEntry{Key: key, SavedAt: savedAt})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return results, err
}

// keyPath converts a slash-delimited cache key into an absolute file path.
func (s *Store) keyPath(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." || filepath.IsAbs(p) {
			parts[i] = "_invalid_"
		}
	}
	parts[len(parts)-1] += ".json"
	return filepath.Join(append([]string{s.Root}, parts...)...)
}
