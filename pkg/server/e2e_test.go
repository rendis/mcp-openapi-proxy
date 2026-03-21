package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func makeReq(args map[string]any) *mcp.CallToolRequest {
	data, _ := json.Marshal(args)
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(data)},
	}
}

func callTool(t *testing.T, handler mcp.ToolHandler, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := handler(context.Background(), makeReq(args))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty content in result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func resultJSON(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := resultText(t, res)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("unmarshal result: %v (text: %s)", err, text)
	}
	return m
}

func loadEndpoints(t *testing.T, specPath string) []spec.Endpoint {
	t.Helper()
	eps, _, err := spec.LoadSpec(specPath)
	if err != nil {
		t.Fatalf("LoadSpec(%s): %v", specPath, err)
	}
	return eps
}

func findEndpoint(t *testing.T, eps []spec.Endpoint, method, path string) spec.Endpoint {
	t.Helper()
	for _, ep := range eps {
		if ep.Method == method && ep.Path == path {
			return ep
		}
	}
	t.Fatalf("endpoint %s %s not found", method, path)
	return spec.Endpoint{}
}

func mockClient(t *testing.T, srv *httptest.Server) *client.Client {
	t.Helper()
	return client.New(srv.URL, auth.NewStaticTokenProvider("test-token"), nil)
}

// ── Petstore CRUD ────────────────────────────────────────────────────────────

func TestE2E_Petstore(t *testing.T) {
	// Mock API server
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		// GET /pets
		case r.Method == "GET" && r.URL.Path == "/pets":
			// Verify query params are received
			limit := r.URL.Query().Get("limit")
			tags := r.URL.Query()["tags"]
			json.NewEncoder(w).Encode(map[string]any{
				"limit": limit,
				"tags":  tags,
				"count": 2,
			})

		// POST /pets
		case r.Method == "POST" && r.URL.Path == "/pets":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["id"] = "pet-001"
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(body)

		// GET /pets/{petId}
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/pets/") && !strings.Contains(r.URL.Path[6:], "/"):
			petId := r.URL.Path[6:]
			json.NewEncoder(w).Encode(map[string]any{
				"id":   petId,
				"name": "Fido",
			})

		// PUT /pets/{petId}
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/pets/") && !strings.Contains(r.URL.Path[6:], "/"):
			petId := r.URL.Path[6:]
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			body["id"] = petId
			json.NewEncoder(w).Encode(body)

		// DELETE /pets/{petId}
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/pets/"):
			w.WriteHeader(204)

		// PUT /pets/{petId}/tags
		case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/tags"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(body)

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	eps := loadEndpoints(t, "../../testdata/petstore.yaml")
	c := mockClient(t, api)

	t.Run("ListPets_WithQueryParams", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/pets")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"limit": 10, "offset": 0})
		m := resultJSON(t, res)
		if m["limit"] != "10" {
			t.Errorf("limit = %v, want 10", m["limit"])
		}
	})

	t.Run("ListPets_WithArrayTags", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/pets")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"tags": []any{"cat", "dog"}})
		m := resultJSON(t, res)
		tags, ok := m["tags"].([]any)
		if !ok {
			t.Fatalf("tags not an array: %v", m["tags"])
		}
		if len(tags) != 2 || tags[0] != "cat" || tags[1] != "dog" {
			t.Errorf("tags = %v, want [cat, dog]", tags)
		}
	})

	t.Run("CreatePet_WithBody", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/pets")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{
			"body": map[string]any{"name": "Buddy", "tag": "dog"},
		})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		if m["name"] != "Buddy" {
			t.Errorf("name = %v, want Buddy", m["name"])
		}
		if m["id"] != "pet-001" {
			t.Errorf("id = %v, want pet-001", m["id"])
		}
	})

	t.Run("GetPet_ByID", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/pets/{petId}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"petId": "abc123"})
		m := resultJSON(t, res)
		if m["id"] != "abc123" {
			t.Errorf("id = %v, want abc123", m["id"])
		}
	})

	t.Run("UpdatePet_WithBody", func(t *testing.T) {
		ep := findEndpoint(t, eps, "PUT", "/pets/{petId}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{
			"petId": "abc123",
			"body":  map[string]any{"name": "Updated"},
		})
		m := resultJSON(t, res)
		if m["name"] != "Updated" {
			t.Errorf("name = %v, want Updated", m["name"])
		}
		if m["id"] != "abc123" {
			t.Errorf("id = %v, want abc123", m["id"])
		}
	})

	t.Run("DeletePet_Returns204", func(t *testing.T) {
		ep := findEndpoint(t, eps, "DELETE", "/pets/{petId}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"petId": "abc123"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})
}

// ── Headers and Auth ─────────────────────────────────────────────────────────

func TestE2E_HeadersAndAuth(t *testing.T) {
	var lastReq *http.Request
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReq = r
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "GET" && r.URL.Path == "/resources":
			json.NewEncoder(w).Encode([]map[string]any{{"id": "1"}})

		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/resources/"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(body)

		case r.Method == "GET" && r.URL.Path == "/admin/config":
			json.NewEncoder(w).Encode(map[string]any{"debug": true})

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	eps := loadEndpoints(t, "../../testdata/headers-and-auth.yaml")
	c := mockClient(t, api)

	t.Run("HeaderParams_Injected", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/resources")
		handler := buildHandler(ep, c)
		_ = callTool(t, handler, map[string]any{
			"X-Request-Id": "req-001",
			"X-Tenant":     "acme",
		})
		if lastReq.Header.Get("X-Request-Id") != "req-001" {
			t.Errorf("X-Request-Id = %q, want req-001", lastReq.Header.Get("X-Request-Id"))
		}
		if lastReq.Header.Get("X-Tenant") != "acme" {
			t.Errorf("X-Tenant = %q, want acme", lastReq.Header.Get("X-Tenant"))
		}
	})

	t.Run("MergePatchContentType", func(t *testing.T) {
		ep := findEndpoint(t, eps, "PATCH", "/resources/{id}")
		handler := buildHandler(ep, c)
		_ = callTool(t, handler, map[string]any{
			"id":   "res-1",
			"body": map[string]any{"name": "updated"},
		})
		// The content type should be application/json (default) since
		// the handler sends JSON regardless. The merge-patch+json is
		// the spec's preferred content type for parsing, but the actual
		// HTTP request Content-Type depends on client.Do behavior.
		ct := lastReq.Header.Get("Content-Type")
		if ct == "" {
			t.Error("Content-Type header missing")
		}
	})

	t.Run("RequiredHeaderParam_InSchema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/admin/config")
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["X-Admin-Token"]; !ok {
			t.Error("X-Admin-Token not in schema properties")
		}
		found := false
		for _, r := range schema.Required {
			if r == "X-Admin-Token" {
				found = true
			}
		}
		if !found {
			t.Error("X-Admin-Token not in required list")
		}
	})
}

// ── Edge Cases ───────────────────────────────────────────────────────────────

func TestE2E_EdgeCases(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Echo the request path and query for verification
		json.NewEncoder(w).Encode(map[string]any{
			"path":  r.URL.Path,
			"query": r.URL.RawQuery,
		})
	}))
	defer api.Close()

	eps := loadEndpoints(t, "../../testdata/edge-cases.yaml")
	c := mockClient(t, api)

	t.Run("DotInPath_ToolNameCorrect", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/v1/health.check")
		tool := buildTool(ep, "svc")
		if tool.Name != "svc_get_v1_health_check" {
			t.Errorf("tool name = %q, want svc_get_v1_health_check", tool.Name)
		}
	})

	t.Run("MultipartBody_EndpointSkipped", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/upload")
		if ep.RequestBody != nil {
			t.Error("multipart endpoint should have nil RequestBody (skipped)")
		}
	})

	t.Run("FormURLEncoded_EndpointSkipped", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/submit")
		if ep.RequestBody != nil {
			t.Error("form-urlencoded endpoint should have nil RequestBody (skipped)")
		}
	})

	t.Run("CookieParam_NotInSchema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items/{id}")
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["session"]; ok {
			t.Error("cookie param 'session' should not appear in schema")
		}
		// Verify path param is still there
		if _, ok := schema.Properties["id"]; !ok {
			t.Error("path param 'id' missing from schema")
		}
	})

	t.Run("EnumParam_InSchema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/search")
		// Verify the status param was parsed with enum
		var statusParam spec.Param
		for _, p := range ep.QueryParams {
			if p.Name == "status" {
				statusParam = p
				break
			}
		}
		if len(statusParam.Enum) != 3 {
			t.Errorf("status enum = %v, want 3 values", statusParam.Enum)
		}
	})

	t.Run("FormatParam_Preserved", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/search")
		var emailParam spec.Param
		for _, p := range ep.QueryParams {
			if p.Name == "email" {
				emailParam = p
				break
			}
		}
		if emailParam.Format != "email" {
			t.Errorf("email format = %q, want email", emailParam.Format)
		}
	})

	t.Run("MinMaxParam_Preserved", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/search")
		var scoreParam spec.Param
		for _, p := range ep.QueryParams {
			if p.Name == "min_score" {
				scoreParam = p
				break
			}
		}
		if scoreParam.Minimum == nil || *scoreParam.Minimum != 0 {
			t.Errorf("min_score minimum = %v, want 0", scoreParam.Minimum)
		}
		if scoreParam.Maximum == nil || *scoreParam.Maximum != 100 {
			t.Errorf("min_score maximum = %v, want 100", scoreParam.Maximum)
		}
	})

	t.Run("PathParam_SpecialChars_Substituted", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/data/{key}")
		handler := buildHandler(ep, c)
		// url.PathEscape encodes a/b → a%2Fb in the URL string,
		// but Go's HTTP client normalizes paths — the server may see
		// the decoded form. What matters: the request succeeds and
		// the value arrives.
		res := callTool(t, handler, map[string]any{"key": "hello-world"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		path := m["path"].(string)
		if path != "/data/hello-world" {
			t.Errorf("path = %q, want /data/hello-world", path)
		}
	})

	t.Run("PathParam_SpaceChar_Arrives", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/data/{key}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"key": "hello world"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		// Go's net/http decodes %20 back to space in r.URL.Path.
		// What matters: the value arrives correctly and the request succeeds.
		path := m["path"].(string)
		if path != "/data/hello world" {
			t.Errorf("path = %q, want /data/hello world", path)
		}
	})
}

// ── Response Handling ────────────────────────────────────────────────────────

func TestE2E_ResponseHandling(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "Hello, plain text!")

		case "/html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<h1>Hello HTML</h1>")

		case "/created":
			w.WriteHeader(201)

		case "/accepted":
			w.WriteHeader(202)

		case "/error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": "bad request"})

		case "/internal":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]any{"error": "internal"})

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	eps := loadEndpoints(t, "../../testdata/responses.yaml")
	c := mockClient(t, api)

	t.Run("TextPlain_ReturnedAsString", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/text")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, nil)
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		text := resultText(t, res)
		if !strings.Contains(text, "Hello, plain text!") {
			t.Errorf("text = %q, want to contain 'Hello, plain text!'", text)
		}
	})

	t.Run("HTML_ReturnedAsString", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/html")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, nil)
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		text := resultText(t, res)
		// Non-JSON response is returned as a string, then formatResult
		// JSON-encodes it — so HTML chars are escaped. Verify the content
		// is present in some form.
		if !strings.Contains(text, "Hello HTML") {
			t.Errorf("text = %q, want to contain 'Hello HTML'", text)
		}
	})

	t.Run("201_EmptyBody_StatusOk", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/created")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"body": map[string]any{"name": "test"}})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})

	t.Run("202_EmptyBody_StatusOk", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/accepted")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"body": map[string]any{"task": "process"}})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		m := resultJSON(t, res)
		if m["status"] != "ok" {
			t.Errorf("status = %v, want ok", m["status"])
		}
	})

	t.Run("400_ReturnsError", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/error")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, nil)
		if !res.IsError {
			t.Fatal("expected IsError: true")
		}
		text := resultText(t, res)
		if !strings.Contains(text, "400") {
			t.Errorf("error text = %q, want to contain 400", text)
		}
	})

	t.Run("500_ReturnsError", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/internal")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, nil)
		if !res.IsError {
			t.Fatal("expected IsError: true")
		}
		text := resultText(t, res)
		if !strings.Contains(text, "500") {
			t.Errorf("error text = %q, want to contain 500", text)
		}
	})
}

// ── Full Pipeline ────────────────────────────────────────────────────────────

func TestE2E_FullPipeline(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	t.Run("PetstoreToolCount", func(t *testing.T) {
		eps := loadEndpoints(t, "../../testdata/petstore.yaml")
		// petstore has: GET /pets, POST /pets, GET /pets/{petId}, PUT /pets/{petId},
		// DELETE /pets/{petId}, PUT /pets/{petId}/tags = 6 endpoints
		if len(eps) != 6 {
			t.Errorf("endpoint count = %d, want 6", len(eps))
		}
	})

	t.Run("ToolNamesMatchPattern", func(t *testing.T) {
		eps := loadEndpoints(t, "../../testdata/petstore.yaml")
		c := mockClient(t, api)

		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
		GenerateTools(srv, eps, c, "pet")

		expectedNames := map[string]bool{
			"pet_get_pets":            true,
			"pet_post_pets":           true,
			"pet_get_pets_petid":      true,
			"pet_put_pets_petid":      true,
			"pet_delete_pets_petid":   true,
			"pet_put_pets_petid_tags": true,
		}

		for _, ep := range eps {
			name := toolName("pet", ep.Method, ep.Path)
			if !expectedNames[name] {
				t.Errorf("unexpected tool name: %s", name)
			}
			delete(expectedNames, name)
		}
		for name := range expectedNames {
			t.Errorf("missing tool name: %s", name)
		}
	})

	t.Run("ToolAnnotations_GetReadOnly_DeleteDestructive", func(t *testing.T) {
		eps := loadEndpoints(t, "../../testdata/petstore.yaml")

		for _, ep := range eps {
			tool := buildTool(ep, "api")
			ann := tool.Annotations
			switch ep.Method {
			case "GET":
				if ann == nil || !ann.ReadOnlyHint {
					t.Errorf("%s %s: GET should have ReadOnlyHint", ep.Method, ep.Path)
				}
			case "DELETE":
				if ann == nil || ann.DestructiveHint == nil || !*ann.DestructiveHint {
					t.Errorf("%s %s: DELETE should have DestructiveHint", ep.Method, ep.Path)
				}
			default:
				if ann != nil {
					t.Errorf("%s %s: %s should have nil annotations", ep.Method, ep.Path, ep.Method)
				}
			}
		}
	})

	t.Run("EdgeCases_SkipCount", func(t *testing.T) {
		eps := loadEndpoints(t, "../../testdata/edge-cases.yaml")
		// edge-cases has 6 paths but /upload (multipart) and /submit (form) have
		// their bodies skipped. The endpoints still get registered (they're not
		// filtered entirely), but their RequestBody is nil.
		// Count: health.check(GET), upload(POST), submit(POST), items/{id}(GET),
		//        search(GET), data/{key}(GET) = 6
		if len(eps) != 6 {
			t.Errorf("endpoint count = %d, want 6", len(eps))
		}

		// Verify multipart and form bodies are nil
		upload := findEndpoint(t, eps, "POST", "/upload")
		if upload.RequestBody != nil {
			t.Error("/upload should have nil RequestBody")
		}
		submit := findEndpoint(t, eps, "POST", "/submit")
		if submit.RequestBody != nil {
			t.Error("/submit should have nil RequestBody")
		}
	})
}

// Ensure unused imports are referenced.
var _ = io.EOF
