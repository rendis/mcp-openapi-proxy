package spec

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// ── test helpers (inline to avoid import cycle with testutil) ─────────

func minimalSpec(t *testing.T, paths map[string]*openapi3.PathItem) *openapi3.T {
	t.Helper()
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Test API", Version: "1.0.0"},
		Paths:   &openapi3.Paths{},
	}
	for p, item := range paths {
		doc.Paths.Set(p, item)
	}
	return doc
}

func writeSpecFile(t *testing.T, doc *openapi3.T) string {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write spec file: %v", err)
	}
	return path
}

func simplePathItem(method string, op *openapi3.Operation) *openapi3.PathItem {
	item := &openapi3.PathItem{}
	switch method {
	case "GET":
		item.Get = op
	case "POST":
		item.Post = op
	case "PUT":
		item.Put = op
	case "PATCH":
		item.Patch = op
	case "DELETE":
		item.Delete = op
	}
	return item
}

func simpleOp(summary string, params ...*openapi3.ParameterRef) *openapi3.Operation {
	return &openapi3.Operation{Summary: summary, Parameters: params}
}

func pathParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "path", Required: required,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{typ}}},
	}}
}

func queryParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "query", Required: required,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{typ}}},
	}}
}

func headerParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "header", Required: required,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{typ}}},
	}}
}

func cookieParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: name, In: "cookie", Required: required,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{typ}}},
	}}
}

// ── LoadSpec ─────────────────────────────────────────────────────────

func TestLoadSpec_LocalJSONFile(t *testing.T) {
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/pets": simplePathItem("GET", simpleOp("List pets")),
	})
	path := writeSpecFile(t, doc)

	endpoints, loaded, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil doc")
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	ep := endpoints[0]
	if ep.Method != "GET" || ep.Path != "/pets" {
		t.Errorf("unexpected endpoint: %s %s", ep.Method, ep.Path)
	}
	if ep.Summary != "List pets" {
		t.Errorf("expected summary 'List pets', got %q", ep.Summary)
	}
}

func TestLoadSpec_InvalidFile(t *testing.T) {
	_, _, err := LoadSpec("/tmp/does-not-exist-xyz.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadSpec_MinimalOneEndpoint(t *testing.T) {
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/health": simplePathItem("GET", simpleOp("Health check")),
	})
	path := writeSpecFile(t, doc)

	endpoints, _, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
}

// ── extractEndpoints ─────────────────────────────────────────────────

func TestExtractEndpoints_AllHTTPMethods(t *testing.T) {
	op := &openapi3.Operation{Summary: "op"}
	item := &openapi3.PathItem{
		Get: op, Post: op, Put: op, Patch: op, Delete: op,
	}
	doc := minimalSpec(t, map[string]*openapi3.PathItem{"/res": item})

	endpoints := extractEndpoints(doc)
	if len(endpoints) != 5 {
		t.Fatalf("expected 5 endpoints, got %d", len(endpoints))
	}
	methods := map[string]bool{}
	for _, ep := range endpoints {
		methods[ep.Method] = true
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !methods[m] {
			t.Errorf("missing method %s", m)
		}
	}
}

func TestExtractEndpoints_PathsSorted(t *testing.T) {
	op := &openapi3.Operation{Summary: "op"}
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/zebra": simplePathItem("GET", op),
		"/alpha": simplePathItem("GET", op),
		"/mid":   simplePathItem("GET", op),
	})

	endpoints := extractEndpoints(doc)
	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(endpoints))
	}
	if endpoints[0].Path != "/alpha" || endpoints[1].Path != "/mid" || endpoints[2].Path != "/zebra" {
		t.Errorf("paths not sorted: %s, %s, %s", endpoints[0].Path, endpoints[1].Path, endpoints[2].Path)
	}
}

func TestExtractEndpoints_MergesPathAndOpParams(t *testing.T) {
	pathParams := openapi3.Parameters{
		pathParamRef("id", "string", true),
		queryParamRef("page", "integer", false),
	}
	opParams := openapi3.Parameters{
		queryParamRef("page", "integer", true), // overrides path-level
	}
	op := &openapi3.Operation{Summary: "with params", Parameters: opParams}
	item := &openapi3.PathItem{Parameters: pathParams, Get: op}
	doc := minimalSpec(t, map[string]*openapi3.PathItem{"/items/{id}": item})

	endpoints := extractEndpoints(doc)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	ep := endpoints[0]
	if len(ep.PathParams) != 1 || ep.PathParams[0].Name != "id" {
		t.Errorf("expected path param 'id', got %+v", ep.PathParams)
	}
	if len(ep.QueryParams) != 1 {
		t.Fatalf("expected 1 query param, got %d", len(ep.QueryParams))
	}
	if !ep.QueryParams[0].Required {
		t.Error("expected query param 'page' to be required (op-level override)")
	}
}

func TestExtractEndpoints_NilPaths(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "empty", Version: "1.0.0"},
	}
	endpoints := extractEndpoints(doc)
	if endpoints != nil {
		t.Errorf("expected nil for nil paths, got %v", endpoints)
	}
}

// ── Parameter handling ───────────────────────────────────────────────

func TestExtractEndpoints_PathParamsCaptured(t *testing.T) {
	op := simpleOp("get item", pathParamRef("id", "string", true))
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/items/{id}": simplePathItem("GET", op),
	})
	endpoints := extractEndpoints(doc)
	if len(endpoints[0].PathParams) != 1 {
		t.Fatalf("expected 1 path param, got %d", len(endpoints[0].PathParams))
	}
	p := endpoints[0].PathParams[0]
	if p.Name != "id" || p.Type != "string" || !p.Required {
		t.Errorf("unexpected path param: %+v", p)
	}
}

func TestExtractEndpoints_QueryParamsCaptured(t *testing.T) {
	op := simpleOp("search", queryParamRef("q", "string", true))
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/search": simplePathItem("GET", op),
	})
	endpoints := extractEndpoints(doc)
	if len(endpoints[0].QueryParams) != 1 {
		t.Fatalf("expected 1 query param, got %d", len(endpoints[0].QueryParams))
	}
	p := endpoints[0].QueryParams[0]
	if p.Name != "q" || p.Type != "string" || !p.Required {
		t.Errorf("unexpected query param: %+v", p)
	}
}

func TestExtractEndpoints_HeaderParamsCaptured(t *testing.T) {
	op := simpleOp("with header", headerParamRef("X-Trace", "string", false))
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/trace": simplePathItem("GET", op),
	})
	endpoints := extractEndpoints(doc)
	if len(endpoints[0].HeaderParams) != 1 {
		t.Fatalf("expected 1 header param, got %d", len(endpoints[0].HeaderParams))
	}
	p := endpoints[0].HeaderParams[0]
	if p.Name != "X-Trace" || p.Type != "string" {
		t.Errorf("unexpected header param: %+v", p)
	}
}

func TestExtractEndpoints_CookieParamWarning(t *testing.T) {
	op := simpleOp("with cookie", cookieParamRef("session", "string", false))
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/me": simplePathItem("GET", op),
	})

	// Capture log output to verify warning.
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	_ = extractEndpoints(doc)

	output := buf.String()
	if len(output) == 0 {
		t.Error("expected warning on stderr for cookie parameter, got nothing")
	}
	if !bytes.Contains(buf.Bytes(), []byte("cookie")) {
		t.Errorf("expected warning to mention 'cookie', got: %s", output)
	}
}

// ── extractBodySchema ────────────────────────────────────────────────

func TestExtractBodySchema_PrefersJSON(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"application/xml": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
			"application/json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	if schema == nil {
		t.Error("expected non-nil schema")
	}
}

func TestExtractBodySchema_PrefersMergePatchJSON(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"application/xml": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
			"application/merge-patch+json": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "application/merge-patch+json" {
		t.Errorf("expected application/merge-patch+json, got %s", ct)
	}
	if schema == nil {
		t.Error("expected non-nil schema")
	}
}

func TestExtractBodySchema_SkipsMultipart(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"multipart/form-data": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "" || schema != nil {
		t.Errorf("expected empty result for multipart-only, got ct=%q schema=%v", ct, schema)
	}
}

func TestExtractBodySchema_SkipsFormURLEncoded(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"application/x-www-form-urlencoded": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "" || schema != nil {
		t.Errorf("expected empty result for form-urlencoded-only, got ct=%q schema=%v", ct, schema)
	}
}

func TestExtractBodySchema_ReturnsNilForUnsupportedOnly(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"multipart/form-data": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
			"application/x-www-form-urlencoded": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "" || schema != nil {
		t.Errorf("expected nil for unsupported-only content, got ct=%q schema=%v", ct, schema)
	}
}

func TestExtractBodySchema_FallbackToXML(t *testing.T) {
	rb := &openapi3.RequestBody{
		Content: openapi3.Content{
			"multipart/form-data": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
			"application/xml": &openapi3.MediaType{
				Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
			},
		},
	}
	ct, schema := extractBodySchema(rb)
	if ct != "application/xml" {
		t.Errorf("expected fallback to application/xml, got %q", ct)
	}
	if schema == nil {
		t.Error("expected non-nil schema for XML fallback")
	}
}

func TestExtractBodySchema_NilContent(t *testing.T) {
	rb := &openapi3.RequestBody{Content: nil}
	ct, schema := extractBodySchema(rb)
	if ct != "" || schema != nil {
		t.Errorf("expected empty for nil content, got ct=%q", ct)
	}
}

// ── mergeParameters ──────────────────────────────────────────────────

func TestMergeParameters_OpOverridesPath(t *testing.T) {
	pathParams := openapi3.Parameters{queryParamRef("limit", "integer", false)}
	opParams := openapi3.Parameters{queryParamRef("limit", "integer", true)}

	merged := mergeParameters(pathParams, opParams)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged param, got %d", len(merged))
	}
	if !merged[0].Value.Required {
		t.Error("expected op-level override to win (required=true)")
	}
}

func TestMergeParameters_NilRefs(t *testing.T) {
	pathParams := openapi3.Parameters{nil, queryParamRef("ok", "string", false)}
	opParams := openapi3.Parameters{nil}

	merged := mergeParameters(pathParams, opParams)
	found := false
	for _, p := range merged {
		if p != nil && p.Value != nil && p.Value.Name == "ok" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'ok' param to survive merge with nil refs")
	}
}

// ── Security ─────────────────────────────────────────────────────────

func TestExtractSecurityNames_OpOverridesDoc(t *testing.T) {
	docSecurity := openapi3.SecurityRequirements{{"api_key": {}}}
	opSecurity := &openapi3.SecurityRequirements{{"oauth2": {}}}

	names := extractSecurityNames(opSecurity, docSecurity)
	if len(names) != 1 || names[0] != "oauth2" {
		t.Errorf("expected [oauth2], got %v", names)
	}
}

func TestExtractSecurityNames_FallbackToDoc(t *testing.T) {
	docSecurity := openapi3.SecurityRequirements{{"api_key": {}}}

	names := extractSecurityNames(nil, docSecurity)
	if len(names) != 1 || names[0] != "api_key" {
		t.Errorf("expected [api_key], got %v", names)
	}
}

// ── Param enrichment (M2): Enum, Format, Min/Max, MinLength/MaxLength ──

func TestExtractEndpoints_ParamEnumCaptured(t *testing.T) {
	paramRef := &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: "status", In: "query", Required: false,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"},
			Enum: []any{"active", "inactive", "pending"},
		}},
	}}
	op := simpleOp("filter", paramRef)
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/items": simplePathItem("GET", op),
	})

	endpoints := extractEndpoints(doc)
	p := endpoints[0].QueryParams[0]
	if len(p.Enum) != 3 {
		t.Fatalf("expected 3 enum values, got %d", len(p.Enum))
	}
	if p.Enum[0] != "active" || p.Enum[1] != "inactive" || p.Enum[2] != "pending" {
		t.Errorf("unexpected enum values: %v", p.Enum)
	}
}

func TestExtractEndpoints_ParamFormatCaptured(t *testing.T) {
	paramRef := &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: "created_at", In: "query", Required: false,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"}, Format: "date-time",
		}},
	}}
	op := simpleOp("filter", paramRef)
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/events": simplePathItem("GET", op),
	})

	endpoints := extractEndpoints(doc)
	p := endpoints[0].QueryParams[0]
	if p.Format != "date-time" {
		t.Errorf("expected format 'date-time', got %q", p.Format)
	}
}

func TestExtractEndpoints_ParamMinMaxCaptured(t *testing.T) {
	min := float64(1)
	max := float64(100)
	paramRef := &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: "limit", In: "query", Required: false,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"integer"}, Min: &min, Max: &max,
		}},
	}}
	op := simpleOp("list", paramRef)
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/items": simplePathItem("GET", op),
	})

	endpoints := extractEndpoints(doc)
	p := endpoints[0].QueryParams[0]
	if p.Minimum == nil || *p.Minimum != 1 {
		t.Errorf("expected minimum 1, got %v", p.Minimum)
	}
	if p.Maximum == nil || *p.Maximum != 100 {
		t.Errorf("expected maximum 100, got %v", p.Maximum)
	}
}

func TestExtractEndpoints_ParamMinMaxLengthCaptured(t *testing.T) {
	minLen := uint64(3)
	maxLen := uint64(50)
	paramRef := &openapi3.ParameterRef{Value: &openapi3.Parameter{
		Name: "name", In: "query", Required: false,
		Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
			Type: &openapi3.Types{"string"}, MinLength: minLen, MaxLength: &maxLen,
		}},
	}}
	op := simpleOp("search", paramRef)
	doc := minimalSpec(t, map[string]*openapi3.PathItem{
		"/search": simplePathItem("GET", op),
	})

	endpoints := extractEndpoints(doc)
	p := endpoints[0].QueryParams[0]
	if p.MinLength == nil || *p.MinLength != 3 {
		t.Errorf("expected minLength 3, got %v", p.MinLength)
	}
	if p.MaxLength == nil || *p.MaxLength != 50 {
		t.Errorf("expected maxLength 50, got %v", p.MaxLength)
	}
}
