package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

const fullContractSpec = "../../testdata/full-contract.yaml"

func loadFullContract(t *testing.T) []spec.Endpoint {
	t.Helper()
	return loadEndpoints(t, fullContractSpec)
}

// ---------------------------------------------------------------------------
// Parser: responses, security, cookies, deprecated, external docs
// ---------------------------------------------------------------------------

func TestFullContract_ParseResponses(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("ListItems_has_200_and_401", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if len(ep.Responses) < 2 {
			t.Fatalf("expected at least 2 responses, got %d", len(ep.Responses))
		}
		var found200, found401 bool
		for _, r := range ep.Responses {
			switch r.StatusCode {
			case "200":
				found200 = true
				if r.Description == "" {
					t.Error("200 description empty")
				}
				if r.ContentType != "application/json" {
					t.Errorf("200 content type = %q, want application/json", r.ContentType)
				}
				if r.Schema == nil {
					t.Error("200 schema is nil")
				}
				// Schema should be an array type.
				if r.Schema["type"] != "array" {
					t.Errorf("200 schema type = %v, want array", r.Schema["type"])
				}
			case "401":
				found401 = true
				if r.Description == "" {
					t.Error("401 description empty")
				}
			}
		}
		if !found200 {
			t.Error("200 response not found")
		}
		if !found401 {
			t.Error("401 response not found")
		}
	})

	t.Run("ListItems_ResponseHeaders", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		var resp200 spec.ResponseInfo
		for _, r := range ep.Responses {
			if r.StatusCode == "200" {
				resp200 = r
				break
			}
		}
		if len(resp200.Headers) < 2 {
			t.Fatalf("expected at least 2 headers, got %d", len(resp200.Headers))
		}
		headerNames := make(map[string]bool)
		for _, h := range resp200.Headers {
			headerNames[h.Name] = true
			if h.Description == "" {
				t.Errorf("header %q has empty description", h.Name)
			}
		}
		for _, expected := range []string{"X-Total-Count", "X-Page", "Link"} {
			if !headerNames[expected] {
				t.Errorf("expected header %q not found", expected)
			}
		}
	})

	t.Run("CreateItem_201_with_Location_header", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/items")
		var resp201 spec.ResponseInfo
		for _, r := range ep.Responses {
			if r.StatusCode == "201" {
				resp201 = r
				break
			}
		}
		if resp201.StatusCode == "" {
			t.Fatal("201 response not found")
		}
		foundLocation := false
		for _, h := range resp201.Headers {
			if h.Name == "Location" {
				foundLocation = true
			}
		}
		if !foundLocation {
			t.Error("Location header not found in 201 response")
		}
	})

	t.Run("DeleteItem_204_no_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "DELETE", "/items/{id}")
		var resp204 spec.ResponseInfo
		for _, r := range ep.Responses {
			if r.StatusCode == "204" {
				resp204 = r
				break
			}
		}
		if resp204.StatusCode == "" {
			t.Fatal("204 response not found")
		}
		if resp204.Schema != nil {
			t.Error("204 response should have nil schema")
		}
	})

	t.Run("Ping_204_no_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/ping")
		if len(ep.Responses) == 0 {
			t.Fatal("expected responses for /ping")
		}
		for _, r := range ep.Responses {
			if r.StatusCode == "204" && r.Schema != nil {
				t.Error("204 response should have nil schema")
			}
		}
	})
}

func TestFullContract_ParseSecurity(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("ListItems_bearerAuth", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if len(ep.Security) == 0 {
			t.Fatal("expected security names")
		}
		if ep.Security[0] != "bearerAuth" {
			t.Errorf("security = %v, want [bearerAuth]", ep.Security)
		}
	})

	t.Run("ListItems_SecurityInfo_details", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if len(ep.SecurityInfo) == 0 {
			t.Fatal("expected SecurityInfo")
		}
		si := ep.SecurityInfo[0]
		if si.Name != "bearerAuth" {
			t.Errorf("Name = %q, want bearerAuth", si.Name)
		}
		if si.Type != "http" {
			t.Errorf("Type = %q, want http", si.Type)
		}
		if si.Scheme != "bearer" {
			t.Errorf("Scheme = %q, want bearer", si.Scheme)
		}
	})

	t.Run("DeleteItem_apiKeyHeader", func(t *testing.T) {
		ep := findEndpoint(t, eps, "DELETE", "/items/{id}")
		if len(ep.SecurityInfo) == 0 {
			t.Fatal("expected SecurityInfo")
		}
		si := ep.SecurityInfo[0]
		if si.Name != "apiKeyHeader" {
			t.Errorf("Name = %q, want apiKeyHeader", si.Name)
		}
		if si.Type != "apiKey" {
			t.Errorf("Type = %q, want apiKey", si.Type)
		}
		if si.In != "header" {
			t.Errorf("In = %q, want header", si.In)
		}
	})

	t.Run("GetStats_apiKeyCookie", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/admin/stats")
		if len(ep.SecurityInfo) == 0 {
			t.Fatal("expected SecurityInfo")
		}
		si := ep.SecurityInfo[0]
		if si.Name != "apiKeyCookie" {
			t.Errorf("Name = %q, want apiKeyCookie", si.Name)
		}
		if si.Type != "apiKey" {
			t.Errorf("Type = %q, want apiKey", si.Type)
		}
		if si.In != "cookie" {
			t.Errorf("In = %q, want cookie", si.In)
		}
	})

	t.Run("PatchItem_no_security", func(t *testing.T) {
		ep := findEndpoint(t, eps, "PATCH", "/items/{id}")
		if len(ep.Security) != 0 {
			t.Errorf("expected no security for PATCH /items/{id}, got %v", ep.Security)
		}
	})
}

func TestFullContract_ParseCookieParams(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("ListItems_has_session_cookie", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if len(ep.CookieParams) == 0 {
			t.Fatal("expected cookie params")
		}
		found := false
		for _, p := range ep.CookieParams {
			if p.Name == "session" {
				found = true
				if p.Type != "string" {
					t.Errorf("type = %q, want string", p.Type)
				}
			}
		}
		if !found {
			t.Error("session cookie param not found")
		}
	})
}

func TestFullContract_ParseDeprecated(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("UpdateItem_deprecated", func(t *testing.T) {
		ep := findEndpoint(t, eps, "PUT", "/items/{id}")
		if !ep.Deprecated {
			t.Error("expected PUT /items/{id} to be deprecated")
		}
	})

	t.Run("ListItems_not_deprecated", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if ep.Deprecated {
			t.Error("GET /items should not be deprecated")
		}
	})
}

func TestFullContract_ParseExternalDocs(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("GetReport_has_externalDocs", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/report")
		if ep.ExternalDocs == "" {
			t.Fatal("expected ExternalDocs URL")
		}
		if ep.ExternalDocs != "https://api.example.com/docs/reports" {
			t.Errorf("ExternalDocs = %q, want https://api.example.com/docs/reports", ep.ExternalDocs)
		}
	})

	t.Run("ListItems_no_externalDocs", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		if ep.ExternalDocs != "" {
			t.Errorf("expected no ExternalDocs for GET /items, got %q", ep.ExternalDocs)
		}
	})
}

// ---------------------------------------------------------------------------
// OutputSchema generation
// ---------------------------------------------------------------------------

func TestFullContract_OutputSchema(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("ListItems_array_wrapped_in_object", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		tool := buildTool(ep, "api")
		if tool.OutputSchema == nil {
			t.Fatal("expected non-nil OutputSchema")
		}
		if tool.OutputSchema.Type != "object" {
			t.Errorf("output schema type = %q, want object", tool.OutputSchema.Type)
		}
		// Array schema should be wrapped under "items" property.
		if tool.OutputSchema.Properties == nil {
			t.Fatal("expected Properties on output schema")
		}
		itemsProp, ok := tool.OutputSchema.Properties["items"]
		if !ok {
			t.Fatal("expected 'items' property in wrapped output schema")
		}
		if itemsProp.Type != "array" {
			t.Errorf("items property type = %q, want array", itemsProp.Type)
		}
	})

	t.Run("CreateItem_object_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/items")
		tool := buildTool(ep, "api")
		if tool.OutputSchema == nil {
			t.Fatal("expected non-nil OutputSchema for POST /items")
		}
		if tool.OutputSchema.Type != "object" {
			t.Errorf("output schema type = %q, want object", tool.OutputSchema.Type)
		}
	})

	t.Run("GetItem_object_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items/{id}")
		tool := buildTool(ep, "api")
		if tool.OutputSchema == nil {
			t.Fatal("expected non-nil OutputSchema for GET /items/{id}")
		}
		if tool.OutputSchema.Type != "object" {
			t.Errorf("output schema type = %q, want object", tool.OutputSchema.Type)
		}
	})

	t.Run("DeleteItem_no_output_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "DELETE", "/items/{id}")
		tool := buildTool(ep, "api")
		if tool.OutputSchema != nil {
			t.Error("expected nil OutputSchema for DELETE /items/{id}")
		}
	})

	t.Run("Ping_no_output_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/ping")
		tool := buildTool(ep, "api")
		// 204 response has no schema, so no OutputSchema.
		if tool.OutputSchema != nil {
			t.Error("expected nil OutputSchema for GET /ping (204 no body)")
		}
	})

	t.Run("ExportCSV_string_wrapped_in_object", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/export")
		tool := buildTool(ep, "api")
		if tool.OutputSchema == nil {
			t.Fatal("expected non-nil OutputSchema for GET /export")
		}
		if tool.OutputSchema.Type != "object" {
			t.Errorf("output schema type = %q, want object", tool.OutputSchema.Type)
		}
		// String schema should be wrapped under "data" property.
		if _, ok := tool.OutputSchema.Properties["data"]; !ok {
			t.Error("expected 'data' property in wrapped output schema for string type")
		}
	})

	t.Run("GetStats_object_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/admin/stats")
		tool := buildTool(ep, "api")
		if tool.OutputSchema == nil {
			t.Fatal("expected non-nil OutputSchema for GET /admin/stats")
		}
		if tool.OutputSchema.Type != "object" {
			t.Errorf("output schema type = %q, want object", tool.OutputSchema.Type)
		}
	})
}

// ---------------------------------------------------------------------------
// Enriched description
// ---------------------------------------------------------------------------

func TestFullContract_Description(t *testing.T) {
	eps := loadFullContract(t)

	t.Run("deprecated_endpoint_has_flag", func(t *testing.T) {
		ep := findEndpoint(t, eps, "PUT", "/items/{id}")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "[DEPRECATED]") {
			t.Errorf("description %q should contain [DEPRECATED]", tool.Description)
		}
	})

	t.Run("non_deprecated_no_flag", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		tool := buildTool(ep, "api")
		if strings.Contains(tool.Description, "[DEPRECATED]") {
			t.Error("GET /items description should not contain [DEPRECATED]")
		}
	})

	t.Run("response_codes_in_description", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "200:") {
			t.Error("description should contain 200 response code")
		}
		if !strings.Contains(tool.Description, "401:") {
			t.Error("description should contain 401 response code")
		}
	})

	t.Run("auth_info_in_description", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "Auth:") {
			t.Error("description should contain auth info")
		}
		if !strings.Contains(tool.Description, "bearerAuth") {
			t.Error("description should reference bearerAuth scheme")
		}
	})

	t.Run("external_docs_in_description", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/report")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "Docs:") {
			t.Error("description should contain external docs link")
		}
		if !strings.Contains(tool.Description, "https://api.example.com/docs/reports") {
			t.Error("description should contain external docs URL")
		}
	})

	t.Run("method_and_path_in_description", func(t *testing.T) {
		ep := findEndpoint(t, eps, "POST", "/items")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "POST /items") {
			t.Error("description should contain method and path")
		}
	})

	t.Run("summary_in_description", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		tool := buildTool(ep, "api")
		if !strings.Contains(tool.Description, "List items with pagination") {
			t.Error("description should contain summary")
		}
	})
}

// ---------------------------------------------------------------------------
// Handler: envelope with status/headers/body, cookie forwarding
// ---------------------------------------------------------------------------

func TestFullContract_HandlerEnvelope(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "GET" && r.URL.Path == "/items":
			w.Header().Set("X-Total-Count", "42")
			w.Header().Set("X-Page", "1")
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "1", "name": "Item 1"},
			})

		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/items/"):
			id := r.URL.Path[len("/items/"):]
			w.Header().Set("ETag", `"abc123"`)
			json.NewEncoder(w).Encode(map[string]any{
				"id":   id,
				"name": "Test Item",
			})

		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/items/"):
			w.WriteHeader(204)

		case r.Method == "GET" && r.URL.Path == "/admin/stats":
			json.NewEncoder(w).Encode(map[string]any{
				"total_items":  100,
				"active_items": 80,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	eps := loadFullContract(t)
	c := mockClient(t, api)

	t.Run("envelope_has_status_content_type_headers_body", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{})
		env := resultJSON(t, res)

		// Status.
		if env["status"] != float64(200) {
			t.Errorf("status = %v, want 200", env["status"])
		}

		// Content type.
		ct, ok := env["content_type"].(string)
		if !ok || !strings.Contains(ct, "json") {
			t.Errorf("content_type = %v, want JSON content type", env["content_type"])
		}

		// Headers.
		headers, ok := env["headers"].(map[string]any)
		if !ok {
			t.Fatalf("headers not a map, got %T", env["headers"])
		}
		if headers["X-Total-Count"] != "42" {
			t.Errorf("X-Total-Count = %v, want 42", headers["X-Total-Count"])
		}

		// Body should be an array.
		body, ok := env["body"].([]any)
		if !ok {
			t.Fatalf("body not an array, got %T: %v", env["body"], env["body"])
		}
		if len(body) == 0 {
			t.Fatal("body is empty")
		}
	})

	t.Run("204_envelope_status", func(t *testing.T) {
		ep := findEndpoint(t, eps, "DELETE", "/items/{id}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"id": "test-id"})
		if res.IsError {
			t.Fatalf("unexpected error: %s", resultText(t, res))
		}
		env := resultJSON(t, res)
		if env["status"] != float64(204) {
			t.Errorf("status = %v, want 204", env["status"])
		}
	})

	t.Run("response_headers_in_envelope", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items/{id}")
		handler := buildHandler(ep, c)
		res := callTool(t, handler, map[string]any{"id": "item-1"})
		env := resultJSON(t, res)
		headers, ok := env["headers"].(map[string]any)
		if !ok {
			t.Fatalf("headers not a map")
		}
		if headers["Etag"] != `"abc123"` {
			t.Errorf("ETag header = %v, want \"abc123\"", headers["Etag"])
		}
	})
}

func TestFullContract_CookieParamForwarding(t *testing.T) {
	var capturedCookie string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"id": "1"}})
	}))
	defer api.Close()

	eps := loadFullContract(t)
	c := mockClient(t, api)

	t.Run("cookie_param_sent_as_cookie_header", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		handler := buildHandler(ep, c)
		_ = callTool(t, handler, map[string]any{
			"session": "my-session-token",
		})
		if !strings.Contains(capturedCookie, "session=my-session-token") {
			t.Errorf("Cookie = %q, want to contain session=my-session-token", capturedCookie)
		}
	})

	t.Run("cookie_param_in_input_schema", func(t *testing.T) {
		ep := findEndpoint(t, eps, "GET", "/items")
		schema := buildInputSchema(ep)
		if _, ok := schema.Properties["session"]; !ok {
			t.Error("session cookie param should appear in input schema")
		}
	})
}

// ---------------------------------------------------------------------------
// Full endpoint count
// ---------------------------------------------------------------------------

func TestFullContract_EndpointCount(t *testing.T) {
	eps := loadFullContract(t)
	// Paths: /items (GET, POST), /items/{id} (GET, PUT, PATCH, DELETE),
	//        /admin/stats (GET), /export (GET), /ping (GET), /report (GET) = 10
	if len(eps) != 10 {
		t.Errorf("endpoint count = %d, want 10", len(eps))
	}
}
