package jira

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cgrossde/jiracli/internal/keychain"
)

// Sentinel errors — callers use errors.Is to distinguish them.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrServer       = errors.New("server error")
)

// User is the Jira account record returned by /rest/api/2/myself.
type User struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
}

// ServerInfo is the payload returned by /rest/api/2/serverInfo.
type ServerInfo struct {
	Version        string `json:"version"`
	DeploymentType string `json:"deploymentType"`
	BaseURL        string `json:"baseURL"`
}

// Client is a thin wrapper around *http.Client that targets a single Jira
// Data Center instance using a Personal Access Token.
type Client struct {
	BaseURL  string // no trailing slash
	PAT      string
	Insecure bool         // skips TLS verification when true
	HTTP     *http.Client // 30 s timeout by default
}

// New constructs a Client from a keychain.Entry. The BaseURL trailing slash is
// trimmed. When entry.Insecure is true, TLS certificate verification is
// disabled for this client.
func New(entry keychain.Entry) *Client {
	baseURL := strings.TrimRight(entry.URL, "/")

	transport := http.DefaultTransport
	if entry.Insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}

	return &Client{
		BaseURL:  baseURL,
		PAT:      entry.PAT,
		Insecure: entry.Insecure,
		HTTP: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// apiURL builds the full URL for a Jira REST API v2 path.
// path must start with "/" (e.g. "/myself").
func (c *Client) apiURL(path string, query url.Values) string {
	u := c.BaseURL + "/rest/api/2" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// do executes an HTTP request, sets auth and accept headers, and returns the
// response body bytes and status code.
func (c *Client) do(req *http.Request) ([]byte, int, error) {
	req.Header.Set("Authorization", "Bearer "+c.PAT)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}
	return body, resp.StatusCode, nil
}

// Get performs GET <BaseURL>/rest/api/2<path>?<query>.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(path, query), nil)
	if err != nil {
		return nil, 0, err
	}
	return c.do(req)
}

// Post performs POST <BaseURL>/rest/api/2<path>?<query> with the given body.
func (c *Client) Post(ctx context.Context, path string, query url.Values, body io.Reader, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(path, query), body)
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.do(req)
}

// Put performs PUT <BaseURL>/rest/api/2<path>?<query> with the given body.
func (c *Client) Put(ctx context.Context, path string, query url.Values, body io.Reader, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.apiURL(path, query), body)
	if err != nil {
		return nil, 0, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.do(req)
}

// Delete performs DELETE <BaseURL>/rest/api/2<path>?<query>.
func (c *Client) Delete(ctx context.Context, path string, query url.Values) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.apiURL(path, query), nil)
	if err != nil {
		return nil, 0, err
	}
	return c.do(req)
}

// PostMultipart performs a multipart/form-data POST to upload files.
// fields contains plain text fields; files maps form field name → path on disk.
// The Atlassian XSRF bypass header X-Atlassian-Token: no-check is set
// automatically, as required by the Jira attachment endpoint.
func (c *Client) PostMultipart(ctx context.Context, path string, fields map[string]string, files map[string]string) ([]byte, int, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	for key, val := range fields {
		if err := mw.WriteField(key, val); err != nil {
			return nil, 0, fmt.Errorf("writing multipart field %q: %w", key, err)
		}
	}

	for fieldName, filePath := range files {
		fw, err := mw.CreateFormFile(fieldName, filepath.Base(filePath))
		if err != nil {
			return nil, 0, fmt.Errorf("creating form file for %q: %w", filePath, err)
		}
		f, err := os.Open(filePath)
		if err != nil {
			return nil, 0, fmt.Errorf("opening file %q: %w", filePath, err)
		}
		if _, err := io.Copy(fw, f); err != nil {
			f.Close()
			return nil, 0, fmt.Errorf("streaming file %q: %w", filePath, err)
		}
		f.Close()
	}

	if err := mw.Close(); err != nil {
		return nil, 0, fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL(path, nil), &buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check")

	return c.do(req)
}

// Myself fetches /rest/api/2/myself and returns the authenticated user record.
func (c *Client) Myself(ctx context.Context) (User, error) {
	body, status, err := c.Get(ctx, "/myself", nil)
	if err != nil {
		return User{}, err
	}
	if status != http.StatusOK {
		return User{}, MapStatus("", status, body)
	}
	var u User
	if err := json.Unmarshal(body, &u); err != nil {
		return User{}, fmt.Errorf("parsing /myself response: %w", err)
	}
	return u, nil
}

// ServerInfo fetches /rest/api/2/serverInfo. This endpoint is typically
// accessible anonymously on most Jira DC instances and is used during setup to
// verify the URL and learn the deployment type and version.
func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	body, status, err := c.Get(ctx, "/serverInfo", nil)
	if err != nil {
		return ServerInfo{}, err
	}
	if status != http.StatusOK {
		return ServerInfo{}, MapStatus("", status, body)
	}
	var si ServerInfo
	if err := json.Unmarshal(body, &si); err != nil {
		return ServerInfo{}, fmt.Errorf("parsing /serverInfo response: %w", err)
	}
	return si, nil
}

// jiraErrorBody is the shape of Jira's standard error response.
type jiraErrorBody struct {
	ErrorMessages []string          `json:"errorMessages"`
	Errors        map[string]string `json:"errors"`
}

// MapStatus converts an HTTP status code and response body into a corrective
// sentinel error. The returned error wraps the appropriate sentinel so callers
// can use errors.Is. profile is used in the 401 message to name the keychain
// entry that needs refreshing.
func MapStatus(profile string, status int, body []byte) error {
	switch {
	case status == http.StatusUnauthorized:
		msg := fmt.Sprintf("PAT in keychain for profile %q was rejected (HTTP 401) — run: jiracli auth reauth", profile)
		return fmt.Errorf("%s: %w", msg, ErrUnauthorized)

	case status == http.StatusForbidden:
		return fmt.Errorf("access denied (HTTP 403): %w", ErrForbidden)

	case status == http.StatusNotFound:
		// Surface Jira's own error messages when present.
		var eb jiraErrorBody
		if err := json.Unmarshal(body, &eb); err == nil && len(eb.ErrorMessages) > 0 {
			return fmt.Errorf("not found: %s: %w", strings.Join(eb.ErrorMessages, "; "), ErrNotFound)
		}
		return fmt.Errorf("not found (HTTP 404): %w", ErrNotFound)

	case status >= 500:
		return fmt.Errorf("server error HTTP %d: %w", status, ErrServer)

	case status == http.StatusBadRequest:
		// Surface Jira's validation/parse errors (e.g. invalid JQL).
		var eb jiraErrorBody
		if err := json.Unmarshal(body, &eb); err == nil {
			var msgs []string
			msgs = append(msgs, eb.ErrorMessages...)
			for k, v := range eb.Errors {
				msgs = append(msgs, k+": "+v)
			}
			if len(msgs) > 0 {
				return fmt.Errorf("bad request: %s", strings.Join(msgs, "; "))
			}
		}
		return fmt.Errorf("bad request (HTTP 400)")

	default:
		return fmt.Errorf("unexpected HTTP %d", status)
	}
}
