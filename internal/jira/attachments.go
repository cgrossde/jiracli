package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// AttachmentMeta is the response from /rest/api/2/attachment/<id>.
type AttachmentMeta struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Created  string `json:"created"`
	Author   struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"author"`
	Content string `json:"content"` // direct download URL
}

// GetAttachmentMeta fetches metadata for a single attachment by ID.
func (c *Client) GetAttachmentMeta(ctx context.Context, id string) (AttachmentMeta, error) {
	body, status, err := c.Get(ctx, "/attachment/"+id, nil)
	if err != nil {
		return AttachmentMeta{}, err
	}
	if status != 200 {
		return AttachmentMeta{}, fmt.Errorf("get attachment %s: %w", id, MapStatus("", status, body))
	}
	var meta AttachmentMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return AttachmentMeta{}, fmt.Errorf("parse attachment meta: %w", err)
	}
	return meta, nil
}

// DownloadAttachment streams the attachment content to destPath.
// When destPath is empty the file is saved to /tmp/jiracli-attach/<id>-<filename>.
// Returns the path of the written file.
func (c *Client) DownloadAttachment(ctx context.Context, meta AttachmentMeta, destPath string) (string, error) {
	if destPath == "" {
		dir, err := os.MkdirTemp("", "jiracli-attach-*")
		if err != nil {
			return "", fmt.Errorf("creating download dir: %w", err)
		}
		safeFilename := filepath.Base(meta.Filename)
		if safeFilename == "." || safeFilename == "/" {
			safeFilename = meta.ID
		}
		destPath = filepath.Join(dir, meta.ID+"-"+safeFilename)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.Content, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.PAT)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download attachment: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("writing attachment: %w", err)
	}
	return destPath, nil
}

// StreamAttachment streams the attachment body to w without saving to disk.
func (c *Client) StreamAttachment(ctx context.Context, meta AttachmentMeta, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.Content, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.PAT)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// UploadAttachment uploads a single file to an issue via multipart POST.
// Returns the attachment metadata returned by the server.
func (c *Client) UploadAttachment(ctx context.Context, issueKey, filePath string) (AttachmentMeta, error) {
	files := map[string]string{"file": filePath}
	respBody, status, err := c.PostMultipart(ctx, "/issue/"+issueKey+"/attachments", nil, files)
	if err != nil {
		return AttachmentMeta{}, err
	}
	if status != 200 && status != 201 {
		return AttachmentMeta{}, fmt.Errorf("upload attachment to %s: %w", issueKey, MapStatus("", status, respBody))
	}
	// Server returns []AttachmentMeta
	var metas []AttachmentMeta
	if err := json.Unmarshal(respBody, &metas); err != nil {
		return AttachmentMeta{}, fmt.Errorf("parse upload response: %w", err)
	}
	if len(metas) == 0 {
		return AttachmentMeta{}, fmt.Errorf("no attachment returned")
	}
	return metas[0], nil
}
