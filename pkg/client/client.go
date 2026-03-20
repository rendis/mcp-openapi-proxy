package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
)

// Client is a generic HTTP client with bearer-token auth and extra headers.
type Client struct {
	baseURL       string
	tokenProvider auth.TokenProvider
	extraHeaders  map[string]string
	httpClient    *http.Client
}

// New creates a Client pointing at the given API base URL.
// extraHeaders are applied to every request (e.g. {"X-Workspace": "abc"}).
func New(baseURL string, tp auth.TokenProvider, extraHeaders map[string]string) *Client {
	if extraHeaders == nil {
		extraHeaders = make(map[string]string)
	}
	return &Client{
		baseURL:       baseURL,
		tokenProvider: tp,
		extraHeaders:  extraHeaders,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Do performs an HTTP request to the given path with the specified method and
// optional body. The path is appended to the base URL as-is.
func (c *Client) Do(ctx context.Context, method, path string, body any) (any, error) {
	var bodyReader io.Reader
	var contentType string

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
		contentType = "application/json"
	}

	resp, err := c.do(ctx, method, path, bodyReader, contentType)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return map[string]any{"status": "ok"}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, parseAPIError(resp)
	}
	return decodeJSON(resp)
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, path string) (any, error) {
	return c.Do(ctx, http.MethodGet, path, nil)
}

// Post performs a POST request with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) (any, error) {
	return c.Do(ctx, http.MethodPost, path, body)
}

// Put performs a PUT request with a JSON body.
func (c *Client) Put(ctx context.Context, path string, body any) (any, error) {
	return c.Do(ctx, http.MethodPut, path, body)
}

// Patch performs a PATCH request with a JSON body.
func (c *Client) Patch(ctx context.Context, path string, body any) (any, error) {
	return c.Do(ctx, http.MethodPatch, path, body)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) (any, error) {
	return c.Do(ctx, http.MethodDelete, path, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	token, err := c.tokenProvider.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("obtain auth token: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for k, v := range c.extraHeaders {
		req.Header.Set(k, v)
	}

	return c.httpClient.Do(req)
}

func decodeJSON(resp *http.Response) (any, error) {
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}
