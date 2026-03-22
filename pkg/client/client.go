package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const defaultMaxBodyBytes int64 = 10 << 20

// BinaryBody is the canonical binary envelope returned by the proxy.
type BinaryBody struct {
	Encoding   string `json:"encoding"`
	DataBase64 string `json:"data_base64"`
	SizeBytes  int    `json:"size_bytes"`
}

// Request describes a fully materialized outbound HTTP request.
type Request struct {
	Method               string
	URL                  string
	Headers              http.Header
	Body                 []byte
	ExpectedContentTypes []string
}

// Response wraps the HTTP response with parsed body and metadata.
type Response struct {
	StatusCode     int
	Headers        map[string][]string
	ContentType    string
	RawContentType string
	Body           any
}

// Client is a thin HTTP transport with global headers and bounded response
// bodies. It deliberately does not resolve authentication.
type Client struct {
	extraHeaders map[string]string
	httpClient   *http.Client
	maxBodyBytes int64
}

// New creates a client with the provided extra headers and body limit.
func New(extraHeaders map[string]string, maxBodyBytes int64) *Client {
	if extraHeaders == nil {
		extraHeaders = map[string]string{}
	}
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxBodyBytes
	}
	return &Client{
		extraHeaders: extraHeaders,
		maxBodyBytes: maxBodyBytes,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Do performs the request and returns the decoded response body, regardless of
// the HTTP status code. Transport and decoding failures are returned as errors.
func (c *Client) Do(ctx context.Context, req *Request) (*Response, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range c.extraHeaders {
		httpReq.Header.Set(k, v)
	}
	for k, values := range req.Headers {
		httpReq.Header.Del(k)
		for _, value := range values {
			httpReq.Header.Add(k, value)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := c.readBody(resp.Body)
	if err != nil {
		return nil, err
	}

	rawContentType := resp.Header.Get("Content-Type")
	contentType := selectContentType(rawContentType, req.ExpectedContentTypes)

	return &Response{
		StatusCode:     resp.StatusCode,
		Headers:        cloneHeaders(resp.Header),
		ContentType:    contentType,
		RawContentType: rawContentType,
		Body:           decodeBody(contentType, bodyBytes),
	}, nil
}

func (c *Client) readBody(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, c.maxBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > c.maxBodyBytes {
		return nil, &BodyTooLargeError{Limit: c.maxBodyBytes}
	}
	return body, nil
}

func decodeBody(contentType string, body []byte) any {
	if len(body) == 0 {
		return nil
	}

	if isJSONContentType(contentType) {
		var decoded any
		if err := json.Unmarshal(body, &decoded); err == nil {
			return decoded
		}
	}

	if isTextContentType(contentType) || (contentType == "" && utf8.Valid(body)) {
		return string(body)
	}

	return BinaryBody{
		Encoding:   "base64",
		DataBase64: base64.StdEncoding.EncodeToString(body),
		SizeBytes:  len(body),
	}
}

func selectContentType(raw string, expected []string) string {
	if raw != "" {
		mediaType, _, err := mime.ParseMediaType(raw)
		if err == nil {
			return mediaType
		}
		return raw
	}
	if len(expected) == 0 {
		return ""
	}
	return expected[0]
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json")
}

func isTextContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/xml":
		return true
	case strings.HasSuffix(contentType, "+xml"):
		return true
	case contentType == "application/javascript":
		return true
	case contentType == "application/x-yaml":
		return true
	case contentType == "application/yaml":
		return true
	case contentType == "application/csv":
		return true
	default:
		return false
	}
}

func cloneHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, values := range h {
		out[k] = append([]string(nil), values...)
	}
	return out
}
