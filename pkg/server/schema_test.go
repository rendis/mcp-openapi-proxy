package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

func TestBuildInputSchema_UsesLocationSections(t *testing.T) {
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/items/{id}",
		PathParams: []spec.Param{
			{Name: "id", Required: true, Schema: map[string]any{"type": "string"}},
		},
		QueryParams: []spec.Param{
			{Name: "id", Schema: map[string]any{"type": "integer"}},
		},
		HeaderParams: []spec.Param{
			{Name: "id", Schema: map[string]any{"type": "string"}},
		},
		CookieParams: []spec.Param{
			{Name: "session", Required: true, Schema: map[string]any{"type": "string"}},
		},
		RequestBody: &spec.RequestBody{
			Required: true,
			Content: []spec.MediaType{
				{ContentType: "application/json", Schema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}},
			},
		},
	}

	schema := buildInputSchema(ep)
	if schema.Type != "object" {
		t.Fatalf("root type = %q", schema.Type)
	}
	for _, section := range []string{"path", "query", "headers", "cookies", "body"} {
		if _, ok := schema.Properties[section]; !ok {
			t.Fatalf("missing section %q", section)
		}
	}
	wantRequired := map[string]bool{"path": true, "cookies": true, "body": true}
	for _, name := range schema.Required {
		delete(wantRequired, name)
	}
	if len(wantRequired) != 0 {
		t.Fatalf("missing required sections: %#v", wantRequired)
	}
}

func TestBuildInputSchema_MultipleBodyMediaTypesRequireWrapper(t *testing.T) {
	ep := spec.Endpoint{
		Method: "POST",
		Path:   "/upload",
		RequestBody: &spec.RequestBody{
			Required: true,
			Content: []spec.MediaType{
				{ContentType: "application/json", Schema: map[string]any{"type": "object"}},
				{ContentType: "application/octet-stream", Schema: map[string]any{"type": "string", "format": "binary"}},
			},
		},
	}

	body := buildInputSchema(ep).Properties["body"]
	if body.Type != "object" {
		t.Fatalf("body type = %q", body.Type)
	}
	if len(body.OneOf) != 2 {
		t.Fatalf("expected oneOf variants for body, got %d", len(body.OneOf))
	}
}

func TestBuildOutputSchema_EnvelopeAndProxyError(t *testing.T) {
	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		Responses: []spec.ResponseInfo{
			{
				StatusCode:  "200",
				Description: "OK",
				Content: []spec.MediaType{
					{ContentType: "application/json", Schema: map[string]any{"type": "object"}},
				},
			},
			{
				StatusCode:  "404",
				Description: "Not found",
				Content: []spec.MediaType{
					{ContentType: "text/plain", Schema: map[string]any{"type": "string"}},
				},
			},
		},
	}

	schema := buildOutputSchema(ep)
	if schema.Type != "object" {
		t.Fatalf("output type = %q", schema.Type)
	}
	if len(schema.OneOf) < 3 {
		t.Fatalf("expected response variants + fallback + proxy error, got %d", len(schema.OneOf))
	}
	if _, ok := schema.Properties["proxy_error"]; !ok {
		t.Fatal("expected proxy_error on root envelope schema")
	}
}

func TestBuildDescription_IncludesAuthAndMediaTypes(t *testing.T) {
	ep := spec.Endpoint{
		Method:      "PATCH",
		Path:        "/items/{id}",
		Summary:     "Patch item",
		Description: "Updates an item",
		Deprecated:  true,
		RequestBody: &spec.RequestBody{
			Content: []spec.MediaType{{ContentType: "application/merge-patch+json", Schema: map[string]any{"type": "object"}}},
		},
		Responses: []spec.ResponseInfo{
			{StatusCode: "200", Description: "OK", Content: []spec.MediaType{{ContentType: "application/json", Schema: map[string]any{"type": "object"}}}},
		},
		SecurityRequirements: []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{{Name: "bearerAuth", Type: "http", Scheme: "bearer", Scopes: []string{"write:items"}}}},
		},
		ExternalDocs: "https://example.com/docs",
	}

	desc := buildDescription(ep)
	for _, fragment := range []string{"Deprecated: true", "Auth:", "application/merge-patch+json", "Responses:", "https://example.com/docs"} {
		if !strings.Contains(desc, fragment) {
			t.Fatalf("description missing %q: %s", fragment, desc)
		}
	}
}

func TestMapToJSONSchema_PreservesBooleanFalseAdditionalProperties(t *testing.T) {
	schema := mapToJSONSchema(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	})
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if !strings.Contains(string(data), `"additionalProperties":false`) {
		t.Fatalf("expected additionalProperties=false, got %s", string(data))
	}
}
