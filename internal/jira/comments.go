package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// CommentAuthor is the author sub-object on a comment.
type CommentAuthor struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// Comment is a single Jira issue comment.
type Comment struct {
	ID      string        `json:"id"`
	Author  CommentAuthor `json:"author"`
	Created string        `json:"created"`
	Updated string        `json:"updated"`
	Body    string        `json:"body"`
}

// CommentsResponse is the paginated response from /issue/<KEY>/comment.
type CommentsResponse struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Comments   []Comment `json:"comments"`
}

// GetComments fetches comments for an issue. page is 1-indexed.
// limit is clamped to [1, 200].
func (c *Client) GetComments(ctx context.Context, key string, page, limit int) (CommentsResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	startAt := (page - 1) * limit

	q := url.Values{}
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("orderBy", "created")

	body, status, err := c.Get(ctx, "/issue/"+key+"/comment", q)
	if err != nil {
		return CommentsResponse{}, err
	}
	if status != 200 {
		return CommentsResponse{}, fmt.Errorf("get comments %s: %w", key, MapStatus("", status, body))
	}

	var resp CommentsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return CommentsResponse{}, fmt.Errorf("parse comments: %w", err)
	}
	return resp, nil
}

// AddComment POSTs a new comment on an issue. Returns the new comment's id.
func (c *Client) AddComment(ctx context.Context, key, body string) (string, error) {
	payload := map[string]string{"body": body}
	raw, _ := json.Marshal(payload)
	respBody, status, err := c.Post(ctx, "/issue/"+key+"/comment", nil, bytes.NewReader(raw), "application/json")
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", fmt.Errorf("add comment on %s: %w", key, MapStatus("", status, respBody))
	}
	var resp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(respBody, &resp)
	return resp.ID, nil
}

// GetComment fetches a single comment by ID.
func (c *Client) GetComment(ctx context.Context, issueKey, commentID string) (Comment, error) {
	body, status, err := c.Get(ctx, "/issue/"+issueKey+"/comment/"+commentID, nil)
	if err != nil {
		return Comment{}, err
	}
	if status != 200 {
		return Comment{}, fmt.Errorf("get comment %s on %s: %w", commentID, issueKey, MapStatus("", status, body))
	}
	var c2 Comment
	if err := json.Unmarshal(body, &c2); err != nil {
		return Comment{}, fmt.Errorf("parse comment: %w", err)
	}
	return c2, nil
}
