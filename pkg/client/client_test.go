package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rendis/mcp-openapi-proxy/internal/testutil"
)

// errTokenProvider is a TokenProvider that always returns an error.
type errTokenProvider struct{ err error }

func (p *errTokenProvider) Token(_ context.Context) (string, error) { return "", p.err }

// --- Request construction ---

func TestDo_GET_MethodAndPath(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"GET /items": testutil.JSONHandler(200, map[string]string{"ok": "true"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider("tok"), nil)
	resp, err := c.Do(context.Background(), http.MethodGet, "/items", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resp.Body)
	}
	if m["ok"] != "true" {
		t.Errorf("expected ok=true, got %v", m["ok"])
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestDo_POST_SendsJSONBody(t *testing.T) {
	var gotBody map[string]any
	var gotCT string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider("tok"), nil)
	_, err := c.Do(context.Background(), http.MethodPost, "/items", map[string]string{"name": "test"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["name"] != "test" {
		t.Errorf("body name = %v, want test", gotBody["name"])
	}
}

func TestDo_AuthorizationHeader_Present(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider("my-secret"), nil)
	_, err := c.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer my-secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-secret")
	}
}

func TestDo_AuthorizationHeader_EmptyToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	_, err := c.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestDo_ExtraHeaders_Injected(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Workspace")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider("tok"), map[string]string{"X-Workspace": "abc"})
	_, err := c.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "abc" {
		t.Errorf("X-Workspace = %q, want %q", gotHeader, "abc")
	}
}

func TestDo_PerRequestHeaders_Override(t *testing.T) {
	var gotExtra, gotReq string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExtra = r.Header.Get("X-Workspace")
		gotReq = r.Header.Get("X-Request-Id")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider("tok"), map[string]string{"X-Workspace": "orig"})
	_, err := c.Do(context.Background(), http.MethodGet, "/x", nil, map[string]string{
		"X-Workspace":  "override",
		"X-Request-Id": "req-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotExtra != "override" {
		t.Errorf("X-Workspace = %q, want %q", gotExtra, "override")
	}
	if gotReq != "req-1" {
		t.Errorf("X-Request-Id = %q, want %q", gotReq, "req-1")
	}
}

// --- Response handling: success ---

func TestDo_200_JSON(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"GET /data": testutil.JSONHandler(200, map[string]any{"key": "val", "num": 42.0}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Get(context.Background(), "/data")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["key"] != "val" {
		t.Errorf("key = %v, want val", m["key"])
	}
	if m["num"] != 42.0 {
		t.Errorf("num = %v, want 42", m["num"])
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestDo_204_NoContent(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"DELETE /item": testutil.EmptyHandler(204),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Delete(context.Background(), "/item")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", m["status"])
	}
	if resp.StatusCode != 204 {
		t.Errorf("StatusCode = %d, want 204", resp.StatusCode)
	}
}

func TestDo_201_EmptyBody(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"POST /items": testutil.EmptyHandler(201),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Post(context.Background(), "/items", map[string]string{"name": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v (BUG A8: 201 empty body should return status ok)", err)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resp.Body)
	}
	if m["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", m["status"])
	}
}

func TestDo_202_EmptyBody(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"POST /jobs": testutil.EmptyHandler(202),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Post(context.Background(), "/jobs", map[string]string{"cmd": "run"})
	if err != nil {
		t.Fatalf("unexpected error: %v (BUG A8: 202 empty body should return status ok)", err)
	}
	m, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resp.Body)
	}
	if m["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", m["status"])
	}
}

func TestDo_200_TextPlain(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"GET /health": testutil.TextHandler(200, "healthy"),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Get(context.Background(), "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v (BUG A7: text/plain should return raw string)", err)
	}
	s, ok := resp.Body.(string)
	if !ok {
		t.Fatalf("expected string, got %T", resp.Body)
	}
	if s != "healthy" {
		t.Errorf("result = %q, want %q", s, "healthy")
	}
	if resp.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", resp.ContentType)
	}
}

func TestDo_200_TextHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		w.Write([]byte("<h1>Hello</h1>"))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Get(context.Background(), "/page")
	if err != nil {
		t.Fatalf("unexpected error: %v (BUG A7: text/html should return raw string)", err)
	}
	s, ok := resp.Body.(string)
	if !ok {
		t.Fatalf("expected string, got %T", resp.Body)
	}
	if s != "<h1>Hello</h1>" {
		t.Errorf("result = %q, want %q", s, "<h1>Hello</h1>")
	}
}

// --- Response handling: errors ---

func TestDo_400_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	_, err := c.Get(context.Background(), "/bad")
	if err == nil {
		t.Fatal("expected error for 400")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Body != "bad request" {
		t.Errorf("Body = %q, want %q", apiErr.Body, "bad request")
	}
}

func TestDo_500_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	_, err := c.Get(context.Background(), "/fail")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
	if apiErr.Body != "internal error" {
		t.Errorf("Body = %q, want %q", apiErr.Body, "internal error")
	}
}

// --- URL construction ---

func TestNew_TrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	// baseURL with trailing slash + path with leading slash should NOT double-slash
	c := New(srv.URL+"/", testutil.MockTokenProvider(""), nil)
	_, err := c.Get(context.Background(), "/items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/items" {
		t.Errorf("path = %q, want /items (BUG A3: double slash)", gotPath)
	}
}

func TestNew_NoTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	_, err := c.Get(context.Background(), "/items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/items" {
		t.Errorf("path = %q, want /items", gotPath)
	}
}

// --- Content type passthrough ---

func TestDo_ExplicitContentType_ViaReqHeaders(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider("tok"), nil)
	_, err := c.Do(context.Background(), http.MethodPost, "/upload", map[string]string{"data": "x"}, map[string]string{
		"Content-Type": "application/xml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCT != "application/xml" {
		t.Errorf("Content-Type = %q, want application/xml", gotCT)
	}
}

// --- Helper methods ---

func TestGet_DelegatesToDo(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"GET /x": testutil.JSONHandler(200, map[string]string{"m": "get"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["m"] != "get" {
		t.Errorf("expected get, got %v", m["m"])
	}
}

func TestPost_DelegatesToDo(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"POST /x": testutil.JSONHandler(200, map[string]string{"m": "post"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Post(context.Background(), "/x", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["m"] != "post" {
		t.Errorf("expected post, got %v", m["m"])
	}
}

func TestPut_DelegatesToDo(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"PUT /x": testutil.JSONHandler(200, map[string]string{"m": "put"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Put(context.Background(), "/x", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["m"] != "put" {
		t.Errorf("expected put, got %v", m["m"])
	}
}

func TestPatch_DelegatesToDo(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"PATCH /x": testutil.JSONHandler(200, map[string]string{"m": "patch"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Patch(context.Background(), "/x", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["m"] != "patch" {
		t.Errorf("expected patch, got %v", m["m"])
	}
}

func TestDelete_DelegatesToDo(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"DELETE /x": testutil.JSONHandler(200, map[string]string{"m": "delete"}),
	})
	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Delete(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := resp.Body.(map[string]any)
	if m["m"] != "delete" {
		t.Errorf("expected delete, got %v", m["m"])
	}
}

// --- Response metadata ---

func TestDo_ResponseHeaders_Captured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, testutil.MockTokenProvider(""), nil)
	resp, err := c.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers["X-Custom-Header"] != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want custom-value", resp.Headers["X-Custom-Header"])
	}
}

// --- Error handling ---

func TestDo_TokenProviderError_Propagated(t *testing.T) {
	srv := testutil.MockAPI(t, map[string]http.HandlerFunc{
		"GET /x": testutil.JSONHandler(200, nil),
	})
	providerErr := fmt.Errorf("token expired")
	c := New(srv.URL, &errTokenProvider{err: providerErr}, nil)
	_, err := c.Get(context.Background(), "/x")
	if err == nil {
		t.Fatal("expected error from token provider")
	}
	if !strings.Contains(err.Error(), "token expired") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "token expired")
	}
}

func TestParseAPIError_ExtractsStatusAndBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 422,
		Body:       io.NopCloser(strings.NewReader(`{"error":"invalid"}`)),
	}
	apiErr := parseAPIError(resp)
	if apiErr.StatusCode != 422 {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	if apiErr.Body != `{"error":"invalid"}` {
		t.Errorf("Body = %q, want %q", apiErr.Body, `{"error":"invalid"}`)
	}
}
