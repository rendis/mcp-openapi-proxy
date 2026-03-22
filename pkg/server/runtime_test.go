package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

func newHandler(ep spec.Endpoint, cfg Config) mcp.ToolHandler {
	return buildHandler(ep, client.New(nil, 1<<20), auth.NewResolver(cfg.AuthProfile), cfg)
}

func TestBuildHandler_JSONEnvelopeAndQuerySerialization(t *testing.T) {
	var gotQuery string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	ep := loadEndpoint(t, "/pets", "GET", "../../testdata/petstore.yaml")
	handler := newHandler(ep, Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"})
	result, err := handler(context.Background(), toolRequest(t, map[string]any{
		"query": map[string]any{
			"limit": 10,
			"tags":  []any{"cat", "dog"},
		},
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", envelopeFromResult(t, result))
	}
	env := envelopeFromResult(t, result)
	if statusCode(t, env) != 200 {
		t.Fatalf("status = %#v", env["status"])
	}
	if env["content_type"] != "application/json" {
		t.Fatalf("content_type = %#v", env["content_type"])
	}
	if gotQuery != "limit=10&tags=cat&tags=dog" && gotQuery != "tags=cat&tags=dog&limit=10" {
		t.Fatalf("unexpected query: %q", gotQuery)
	}
}

func TestBuildHandler_UsesMergePatchContentType(t *testing.T) {
	var gotContentType string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	ep := loadEndpoint(t, "/resources/{id}", "PATCH", "../../testdata/headers-and-auth.yaml")
	t.Setenv("MCP_AUTH_TOKEN", "secret")
	handler := newHandler(ep, Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"})
	_, err := handler(context.Background(), toolRequest(t, map[string]any{
		"path": map[string]any{"id": "res-1"},
		"body": map[string]any{"name": "updated"},
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if base, _, _ := mime.ParseMediaType(gotContentType); base != "application/merge-patch+json" {
		t.Fatalf("content type = %q", gotContentType)
	}
}

func TestBuildHandler_MultipartBinaryInput(t *testing.T) {
	var fileName, fileContent string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		fileName = part.FileName()
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("ReadAll(part): %v", err)
		}
		fileContent = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uploaded":true}`))
	}))
	defer api.Close()

	ep := loadEndpoint(t, "/upload", "POST", "../../testdata/edge-cases.yaml")
	handler := newHandler(ep, Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"})
	_, err := handler(context.Background(), toolRequest(t, map[string]any{
		"body": map[string]any{
			"file": map[string]any{
				"source":       "base64",
				"data_base64":  base64.StdEncoding.EncodeToString([]byte("hello")),
				"filename":     "hello.txt",
				"content_type": "text/plain",
			},
		},
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if fileName != "hello.txt" || fileContent != "hello" {
		t.Fatalf("unexpected multipart payload: %q %q", fileName, fileContent)
	}
}

func TestBuildHandler_AuthResolutionAndErrorEnvelope(t *testing.T) {
	t.Run("bearer auth", func(t *testing.T) {
		var authz string
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer api.Close()

		t.Setenv("MCP_AUTH_TOKEN", "secret")
		ep := loadEndpoint(t, "/resources", "GET", "../../testdata/headers-and-auth.yaml")
		handler := newHandler(ep, Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"})
		result, err := handler(context.Background(), toolRequest(t, map[string]any{
			"headers": map[string]any{"X-Tenant": "acme"},
		}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("unexpected error: %#v", envelopeFromResult(t, result))
		}
		if authz != "Bearer secret" {
			t.Fatalf("authorization = %q", authz)
		}
	})

	t.Run("api error becomes MCP error envelope", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad request"}`))
		}))
		defer api.Close()

		ep := loadEndpoint(t, "/error", "GET", "../../testdata/responses.yaml")
		handler := newHandler(ep, Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"})
		result, err := handler(context.Background(), toolRequest(t, map[string]any{}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !result.IsError {
			t.Fatal("expected IsError=true for 400 response")
		}
		env := envelopeFromResult(t, result)
		if statusCode(t, env) != 400 {
			t.Fatalf("status = %#v", env["status"])
		}
	})
}

func TestBuildHandler_RejectsInsecureRemoteAuth(t *testing.T) {
	t.Setenv("MCP_AUTH_TOKEN", "secret")
	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		SecurityRequirements: []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{{Name: "bearerAuth", Type: "http", Scheme: "bearer"}}},
		},
		Responses: []spec.ResponseInfo{{StatusCode: "200"}},
	}
	handler := newHandler(ep, Config{
		BaseURL:           "http://example.com",
		ToolPrefix:        "api",
		AuthProfile:       "default",
		AllowInsecureHTTP: false,
	})
	result, err := handler(context.Background(), toolRequest(t, map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected insecure transport error")
	}
	env := envelopeFromResult(t, result)
	if statusCode(t, env) != 0 {
		t.Fatalf("status = %#v", env["status"])
	}
	proxyErr := env["proxy_error"].(map[string]any)
	if proxyErr["code"] != "insecure_transport" {
		t.Fatalf("proxy_error = %#v", proxyErr)
	}
}

func TestSerializeRequestBody_PathSourceReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	data, ct, err := serializeRequestBody(&spec.RequestBody{
		Required: true,
		Content:  []spec.MediaType{{ContentType: "application/octet-stream", Schema: map[string]any{"type": "string", "format": "binary"}}},
	}, map[string]any{"source": "path", "path": path, "content_type": "text/plain"})
	if err != nil {
		t.Fatalf("serializeRequestBody: %v", err)
	}
	if string(data) != "hello" || ct != "application/octet-stream" {
		t.Fatalf("unexpected serialized body: %q %q", string(data), ct)
	}
}

func TestMultipartWriterPreservesExplicitPartContentType(t *testing.T) {
	var partCT string
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	err := writeMultipartPart(writer, "file", map[string]any{
		"source":       "base64",
		"data_base64":  base64.StdEncoding.EncodeToString([]byte("x")),
		"filename":     "x.txt",
		"content_type": "text/plain",
	}, spec.Encoding{})
	if err != nil {
		t.Fatalf("writeMultipartPart: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(&body, writer.Boundary())
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}
	partCT = part.Header.Get("Content-Type")
	if partCT != "text/plain" {
		t.Fatalf("part content type = %q", partCT)
	}
}
