package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/internal/testutil"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// ---------------------------------------------------------------------------
// toolName
// ---------------------------------------------------------------------------

func TestToolName(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		method string
		path   string
		want   string
	}{
		{
			name:   "basic",
			prefix: "api",
			method: "GET",
			path:   "/users",
			want:   "api_get_users",
		},
		{
			name:   "special chars replaced",
			prefix: "api",
			method: "POST",
			path:   "/users/{id}/roles-admin",
			want:   "api_post_users_id_roles_admin",
		},
		{
			name:   "consecutive underscores collapsed",
			prefix: "api",
			method: "GET",
			path:   "/users/{id}",
			want:   "api_get_users_id",
		},
		{
			name:   "leading/trailing underscores trimmed",
			prefix: "api",
			method: "GET",
			path:   "/users/",
			want:   "api_get_users",
		},
		{
			name:   "empty path",
			prefix: "api",
			method: "GET",
			path:   "",
			want:   "api_get",
		},
		{
			name:   "path with dots",
			prefix: "svc",
			method: "GET",
			path:   "/v1/health.check",
			want:   "svc_get_v1_health_check",
		},
		{
			name:   "trailing slash same as without",
			prefix: "api",
			method: "GET",
			path:   "/users/",
			want:   "api_get_users",
		},
		{
			name:   "numeric segments",
			prefix: "api",
			method: "GET",
			path:   "/v2/users",
			want:   "api_get_v2_users",
		},
		{
			name:   "method is lowercased",
			prefix: "api",
			method: "DELETE",
			path:   "/users/{id}",
			want:   "api_delete_users_id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolName(tt.prefix, tt.method, tt.path)
			if got != tt.want {
				t.Errorf("toolName(%q, %q, %q) = %q, want %q",
					tt.prefix, tt.method, tt.path, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildInputSchema
// ---------------------------------------------------------------------------

func TestBuildInputSchema(t *testing.T) {
	t.Run("path params as top-level properties", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/users/{id}",
			testutil.WithPathParam("id", "string", true),
		)
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["id"]; !ok {
			t.Fatal("expected 'id' in properties")
		}
		if schema.Properties["id"].Type != "string" {
			t.Errorf("id type = %q, want %q", schema.Properties["id"].Type, "string")
		}
	})

	t.Run("query params as top-level properties", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/users",
			testutil.WithQueryParam("page", "integer", false),
			testutil.WithQueryParam("limit", "integer", false),
		)
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["page"]; !ok {
			t.Fatal("expected 'page' in properties")
		}
		if schema.Properties["page"].Type != "integer" {
			t.Errorf("page type = %q, want %q", schema.Properties["page"].Type, "integer")
		}
	})

	t.Run("header params as top-level properties", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/data",
			testutil.WithHeaderParam("X-Request-ID", "string", true),
		)
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["X-Request-ID"]; !ok {
			t.Fatal("expected 'X-Request-ID' in properties")
		}
	})

	t.Run("reserved headers excluded from schema", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/data",
			testutil.WithHeaderParam("Authorization", "string", false),
			testutil.WithHeaderParam("Content-Type", "string", false),
			testutil.WithHeaderParam("Host", "string", false),
			testutil.WithHeaderParam("X-Custom", "string", true),
		)
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["Authorization"]; ok {
			t.Error("Authorization should be excluded from schema")
		}
		if _, ok := schema.Properties["Content-Type"]; ok {
			t.Error("Content-Type should be excluded from schema")
		}
		if _, ok := schema.Properties["Host"]; ok {
			t.Error("Host should be excluded from schema")
		}
		if _, ok := schema.Properties["X-Custom"]; !ok {
			t.Error("X-Custom should be in schema")
		}
	})

	t.Run("body nested under body key", func(t *testing.T) {
		ep := testutil.MakeEndpoint("POST", "/users",
			testutil.WithBody(true, map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			}),
		)
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["body"]; !ok {
			t.Fatal("expected 'body' in properties")
		}
	})

	t.Run("required params in required array", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/users/{id}",
			testutil.WithPathParam("id", "string", true),
			testutil.WithQueryParam("optional", "string", false),
		)
		schema := buildInputSchema(ep)
		if len(schema.Required) != 1 || schema.Required[0] != "id" {
			t.Errorf("Required = %v, want [id]", schema.Required)
		}
	})

	t.Run("param types mapped correctly", func(t *testing.T) {
		types := map[string]string{
			"string":  "string",
			"integer": "integer",
			"number":  "number",
			"boolean": "boolean",
			"array":   "array",
		}
		for input, expected := range types {
			t.Run(input, func(t *testing.T) {
				ep := testutil.MakeEndpoint("GET", "/test",
					testutil.WithQueryParam("p", input, false),
				)
				schema := buildInputSchema(ep)
				got := schema.Properties["p"].Type
				if got != expected {
					t.Errorf("type for %q = %q, want %q", input, got, expected)
				}
			})
		}
	})

	t.Run("default values preserved", func(t *testing.T) {
		ep := testutil.MakeEndpoint("GET", "/test")
		ep.QueryParams = []spec.Param{
			{Name: "limit", Type: "integer", Default: 20},
		}
		schema := buildInputSchema(ep)
		prop := schema.Properties["limit"]
		if prop.Default == nil {
			t.Fatal("expected default to be set")
		}
		// Default is json.RawMessage
		var val float64
		if err := json.Unmarshal(prop.Default, &val); err != nil {
			t.Fatalf("unmarshal default: %v", err)
		}
		if val != 20 {
			t.Errorf("default = %v, want 20", val)
		}
	})
}

// ---------------------------------------------------------------------------
// toolAnnotations
// ---------------------------------------------------------------------------

func TestToolAnnotations(t *testing.T) {
	t.Run("GET is read-only", func(t *testing.T) {
		ann := toolAnnotations("GET")
		if ann == nil {
			t.Fatal("expected non-nil annotations for GET")
		}
		if !ann.ReadOnlyHint {
			t.Error("expected ReadOnlyHint=true for GET")
		}
	})

	t.Run("DELETE is destructive", func(t *testing.T) {
		ann := toolAnnotations("DELETE")
		if ann == nil {
			t.Fatal("expected non-nil annotations for DELETE")
		}
		if ann.DestructiveHint == nil || !*ann.DestructiveHint {
			t.Error("expected DestructiveHint=true for DELETE")
		}
	})

	t.Run("POST returns nil", func(t *testing.T) {
		if ann := toolAnnotations("POST"); ann != nil {
			t.Errorf("expected nil for POST, got %+v", ann)
		}
	})

	t.Run("PUT returns nil", func(t *testing.T) {
		if ann := toolAnnotations("PUT"); ann != nil {
			t.Errorf("expected nil for PUT, got %+v", ann)
		}
	})

	t.Run("PATCH returns nil", func(t *testing.T) {
		if ann := toolAnnotations("PATCH"); ann != nil {
			t.Errorf("expected nil for PATCH, got %+v", ann)
		}
	})
}

// ---------------------------------------------------------------------------
// buildHandler
// ---------------------------------------------------------------------------

// makeCallToolRequest builds a CallToolRequest with the given arguments map.
func makeCallToolRequest(args map[string]any) *mcp.CallToolRequest {
	data, _ := json.Marshal(args)
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(data),
		},
	}
}

func TestBuildHandler(t *testing.T) {
	t.Run("path param substitution", func(t *testing.T) {
		var capturedPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/users/{id}",
			testutil.WithPathParam("id", "string", true),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"id": "abc"})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		if capturedPath != "/users/abc" {
			t.Errorf("path = %q, want /users/abc", capturedPath)
		}
	})

	t.Run("path param with URL-special chars is escaped", func(t *testing.T) {
		var capturedRequestURI string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedRequestURI = r.RequestURI
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/users/{id}",
			testutil.WithPathParam("id", "string", true),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"id": "a/b"})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		// The slash in "a/b" must be percent-encoded so the server sees it as one segment.
		if !strings.Contains(capturedRequestURI, "a%2Fb") {
			t.Errorf("RequestURI = %q, want to contain a%%2Fb", capturedRequestURI)
		}
	})

	t.Run("path param with percent char is escaped", func(t *testing.T) {
		var capturedRequestURI string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedRequestURI = r.RequestURI
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/users/{id}",
			testutil.WithPathParam("id", "string", true),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"id": "50%"})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		// The percent in "50%" must be escaped as %25 in the wire request.
		if !strings.Contains(capturedRequestURI, "50%25") {
			t.Errorf("RequestURI = %q, want to contain 50%%25", capturedRequestURI)
		}
	})

	t.Run("query params appended", func(t *testing.T) {
		var capturedQuery url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/users",
			testutil.WithQueryParam("page", "integer", false),
			testutil.WithQueryParam("limit", "integer", false),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"page": 1, "limit": 20})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		if capturedQuery.Get("page") != "1" {
			t.Errorf("page = %q, want 1", capturedQuery.Get("page"))
		}
		if capturedQuery.Get("limit") != "20" {
			t.Errorf("limit = %q, want 20", capturedQuery.Get("limit"))
		}
	})

	t.Run("array query params expanded", func(t *testing.T) {
		var capturedQuery url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/items",
			testutil.WithQueryParam("ids", "array", false),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"ids": []any{"a", "b", "c"}})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		got := capturedQuery["ids"]
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Errorf("ids = %v, want [a b c]", got)
		}
	})

	t.Run("header params injected", func(t *testing.T) {
		var capturedHeader http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeader = r.Header
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/data",
			testutil.WithHeaderParam("X-Custom", "string", true),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{"X-Custom": "myval"})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		if capturedHeader.Get("X-Custom") != "myval" {
			t.Errorf("X-Custom = %q, want myval", capturedHeader.Get("X-Custom"))
		}
	})

	t.Run("reserved headers not injected", func(t *testing.T) {
		var capturedHeader http.Header
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeader = r.Header
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/data",
			testutil.WithHeaderParam("Authorization", "string", false),
			testutil.WithHeaderParam("Host", "string", false),
			testutil.WithHeaderParam("X-Custom", "string", true),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("test-token"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{
			"Authorization": "hacked",
			"Host":          "evil.com",
			"X-Custom":      "ok",
		})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		// Authorization should be the token provider's value, not "hacked"
		if got := capturedHeader.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want 'Bearer test-token'", got)
		}
		// X-Custom should be injected normally
		if capturedHeader.Get("X-Custom") != "ok" {
			t.Errorf("X-Custom = %q, want 'ok'", capturedHeader.Get("X-Custom"))
		}
	})

	t.Run("body extracted from body key", func(t *testing.T) {
		var capturedBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"created": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("POST", "/users",
			testutil.WithBody(true, map[string]any{
				"type": "object",
			}),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{
			"body": map[string]any{"name": "Alice", "age": 30},
		})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Fatalf("result.IsError = true")
		}
		if capturedBody["name"] != "Alice" {
			t.Errorf("body.name = %v, want Alice", capturedBody["name"])
		}
	})

	t.Run("missing optional params no error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/users",
			testutil.WithQueryParam("page", "integer", false),
		)
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		// Call with no arguments at all.
		req := makeCallToolRequest(map[string]any{})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result.IsError {
			t.Error("expected IsError=false for missing optional param")
		}
	})

	t.Run("API error returns IsError true", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
		}))
		t.Cleanup(srv.Close)

		ep := testutil.MakeEndpoint("GET", "/fail")
		c := client.New(srv.URL, testutil.MockTokenProvider("tok"), nil)
		handler := buildHandler(ep, c)

		req := makeCallToolRequest(map[string]any{})
		result, err := handler(context.Background(), req)
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !result.IsError {
			t.Error("expected IsError=true for 500 response")
		}
	})
}
