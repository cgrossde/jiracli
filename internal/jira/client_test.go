package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgrossde/jiracli/internal/keychain"
)

// newTestClient builds a Client pointed at the given test server URL.
func newTestClient(serverURL string) *Client {
	return New(keychain.Entry{
		Profile: "test",
		URL:     serverURL,
		PAT:     "test-pat",
	})
}

func TestGet_200(t *testing.T) {
	want := `{"name":"alice"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-pat" {
			t.Errorf("missing/wrong Authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("missing Accept header")
		}
		if !strings.HasPrefix(r.URL.Path, "/rest/api/2/") {
			t.Errorf("unexpected path prefix: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, want)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	body, status, err := c.Get(context.Background(), "/myself", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestMapStatus_401(t *testing.T) {
	err := MapStatus("default", http.StatusUnauthorized, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got: %v", err)
	}
	if !strings.Contains(err.Error(), "jiracli auth reauth") {
		t.Errorf("error missing corrective hint: %v", err)
	}
	if !strings.Contains(err.Error(), `"default"`) {
		t.Errorf("error missing profile name: %v", err)
	}
}

func TestMapStatus_403(t *testing.T) {
	err := MapStatus("prod", http.StatusForbidden, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("expected ErrForbidden, got: %v", err)
	}
}

func TestMapStatus_404_noBody(t *testing.T) {
	err := MapStatus("", http.StatusNotFound, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestMapStatus_404_withErrorMessages(t *testing.T) {
	body := `{"errorMessages":["Issue ACME-999 does not exist"],"errors":{}}`
	err := MapStatus("", http.StatusNotFound, []byte(body))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ACME-999") {
		t.Errorf("error message should contain Jira errorMessages: %v", err)
	}
}

func TestMapStatus_500(t *testing.T) {
	err := MapStatus("", http.StatusInternalServerError, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrServer) {
		t.Errorf("expected ErrServer, got: %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestMapStatus_503(t *testing.T) {
	err := MapStatus("", 503, nil)
	if !errors.Is(err, ErrServer) {
		t.Errorf("503 should be ErrServer, got: %v", err)
	}
}

func TestGet_401_returnsErrUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	body, status, err := c.Get(context.Background(), "/myself", nil)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if status != 401 {
		t.Fatalf("status = %d, want 401", status)
	}
	// body returned; caller maps via MapStatus
	_ = body
	mapErr := MapStatus("test", status, body)
	if !errors.Is(mapErr, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized after MapStatus: %v", mapErr)
	}
}

func TestPostMultipart_headersAndContentType(t *testing.T) {
	// Write a temp file to upload.
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(tmpFile, []byte("hello jira"), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	var gotContentType, gotAtlassianToken string
	var gotFileContent string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotAtlassianToken = r.Header.Get("X-Atlassian-Token")

		// Parse the multipart body to verify file content.
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("bad Content-Type: %v", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusOK)
			return
		}
		mr := multipartReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("reading multipart: %v", err)
				break
			}
			data, _ := io.ReadAll(part)
			if part.FileName() == "hello.txt" {
				gotFileContent = string(data)
			}
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `[]`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, status, err := c.PostMultipart(
		context.Background(),
		"/issue/ACME-1/attachments",
		map[string]string{"note": "test upload"},
		map[string]string{"file": tmpFile},
	)
	if err != nil {
		t.Fatalf("PostMultipart: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	if gotAtlassianToken != "no-check" {
		t.Errorf("X-Atlassian-Token = %q, want %q", gotAtlassianToken, "no-check")
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	if gotFileContent != "hello jira" {
		t.Errorf("file content = %q, want %q", gotFileContent, "hello jira")
	}
}

func TestMyself(t *testing.T) {
	want := User{
		Name:         "alice",
		DisplayName:  "Alice Smith",
		EmailAddress: "alice@example.com",
		Active:       true,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/myself" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if got != want {
		t.Errorf("Myself = %+v, want %+v", got, want)
	}
}

func TestServerInfo(t *testing.T) {
	want := ServerInfo{
		Version:        "9.4.0",
		DeploymentType: "Server",
		BaseURL:        "https://jira.example.com",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/serverInfo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.ServerInfo(context.Background())
	if err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	if got != want {
		t.Errorf("ServerInfo = %+v, want %+v", got, want)
	}
}

func TestNew_trimsTrailingSlash(t *testing.T) {
	c := New(keychain.Entry{URL: "https://jira.example.com/", PAT: "x"})
	if strings.HasSuffix(c.BaseURL, "/") {
		t.Errorf("BaseURL should not have trailing slash: %s", c.BaseURL)
	}
}

// multipartReader is a thin helper so the test file has no extra import for
// mime/multipart — it lives in the same package and reuses the top-level import.
func multipartReader(r io.Reader, boundary string) *multipart.Reader {
	return multipart.NewReader(r, boundary)
}
