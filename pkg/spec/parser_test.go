package spec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func writeDoc(t *testing.T, doc *openapi3.T) string {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func validOperation(summary string) *openapi3.Operation {
	return &openapi3.Operation{
		Summary: summary,
		Responses: openapi3.NewResponses(
			openapi3.WithStatus(200, &openapi3.ResponseRef{
				Value: &openapi3.Response{
					Description: openapi3.Ptr("OK"),
				},
			}),
		),
	}
}

func TestLoadSpec_StrictValidation(t *testing.T) {
	valid := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Test", Version: "1.0.0"},
		Paths:   &openapi3.Paths{},
	}
	valid.Paths.Set("/health", &openapi3.PathItem{Get: validOperation("health")})

	endpoints, _, err := LoadSpec(writeDoc(t, valid))
	if err != nil {
		t.Fatalf("LoadSpec(valid): %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	invalid := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Broken", Version: "1.0.0"},
		Paths:   &openapi3.Paths{},
	}
	invalid.Paths.Set("/broken", &openapi3.PathItem{Get: &openapi3.Operation{Summary: "broken"}})

	if _, _, err := LoadSpec(writeDoc(t, invalid)); err == nil {
		t.Fatal("expected validation error for invalid spec")
	}
}

func TestExtractEndpoints_CapturesServerPrecedenceAndMethods(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Servers", Version: "1.0.0"},
		Servers: openapi3.Servers{
			&openapi3.Server{URL: "https://root.example.com"},
		},
		Paths: &openapi3.Paths{},
	}

	pathItem := &openapi3.PathItem{
		Servers: openapi3.Servers{
			&openapi3.Server{URL: "https://path.example.com"},
		},
		Get: validOperation("get"),
		Head: &openapi3.Operation{
			Summary: "head",
			Servers: &openapi3.Servers{
				&openapi3.Server{URL: "https://op.example.com"},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(204, &openapi3.ResponseRef{
					Value: &openapi3.Response{Description: openapi3.Ptr("No Content")},
				}),
			),
		},
		Options: validOperation("options"),
	}
	doc.Paths.Set("/items", pathItem)

	endpoints := extractEndpoints(doc)
	if len(endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(endpoints))
	}

	find := func(method string) Endpoint {
		t.Helper()
		for _, ep := range endpoints {
			if ep.Method == method {
				return ep
			}
		}
		t.Fatalf("method %s not found", method)
		return Endpoint{}
	}

	if got := find("GET").BaseURL; got != "https://path.example.com" {
		t.Fatalf("GET baseURL = %q, want path server", got)
	}
	if got := find("HEAD").BaseURL; got != "https://op.example.com" {
		t.Fatalf("HEAD baseURL = %q, want op server", got)
	}
	if got := find("OPTIONS").BaseURL; got != "https://path.example.com" {
		t.Fatalf("OPTIONS baseURL = %q, want path server", got)
	}
}

func TestExtractEndpoints_CapturesParameterSchemasAndExamples(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Params", Version: "1.0.0"},
		Paths:   &openapi3.Paths{},
	}

	explode := true
	doc.Paths.Set("/search", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary: "search",
			Parameters: openapi3.Parameters{
				&openapi3.ParameterRef{Value: &openapi3.Parameter{
					Name:          "filters",
					In:            "query",
					Style:         "deepObject",
					Explode:       &explode,
					AllowReserved: true,
					Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: openapi3.Schemas{
							"status": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
						},
						Nullable: true,
					}},
					Example: map[string]any{"status": "active"},
				}},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(200, &openapi3.ResponseRef{
					Value: &openapi3.Response{Description: openapi3.Ptr("OK")},
				}),
			),
		},
	})

	endpoints := extractEndpoints(doc)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	param := endpoints[0].QueryParams[0]
	if param.Style != "deepObject" || !param.Explode || !param.AllowReserved {
		t.Fatalf("unexpected serialization metadata: %+v", param)
	}
	types, ok := param.Schema["type"].([]any)
	if !ok || len(types) != 2 {
		t.Fatalf("expected nullable schema type union, got %#v", param.Schema["type"])
	}
	if _, ok := param.Schema["examples"].([]any); !ok {
		t.Fatalf("expected examples on normalized schema, got %#v", param.Schema)
	}
}

func TestExtractRequestBodyAndResponses_PreserveMediaTypes(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Bodies", Version: "1.0.0"},
		Paths:   &openapi3.Paths{},
	}
	doc.Paths.Set("/upload", &openapi3.PathItem{
		Post: &openapi3.Operation{
			Summary: "upload",
			RequestBody: &openapi3.RequestBodyRef{
				Value: &openapi3.RequestBody{
					Required: true,
					Content: openapi3.Content{
						"multipart/form-data": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
								Type: &openapi3.Types{"object"},
								Properties: openapi3.Schemas{
									"file": &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}, Format: "binary"}},
								},
							}},
						},
						"application/json": &openapi3.MediaType{
							Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}},
						},
					},
				},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(200, &openapi3.ResponseRef{
					Value: &openapi3.Response{
						Description: openapi3.Ptr("OK"),
						Content: openapi3.Content{
							"text/plain":       &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}}},
							"application/json": &openapi3.MediaType{Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"object"}}}},
						},
						Headers: openapi3.Headers{
							"X-Trace": &openapi3.HeaderRef{Value: &openapi3.Header{Parameter: openapi3.Parameter{
								Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
							}}},
						},
					},
				}),
			),
		},
	})

	endpoints := extractEndpoints(doc)
	body := endpoints[0].RequestBody
	if body == nil || len(body.Content) != 2 {
		t.Fatalf("expected 2 request media types, got %#v", body)
	}
	if body.Content[0].ContentType != "application/json" {
		t.Fatalf("expected deterministic request media type ordering, got %#v", body.Content)
	}

	resp := endpoints[0].Responses[0]
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 response media types, got %#v", resp)
	}
	if resp.Content[0].ContentType != "application/json" {
		t.Fatalf("expected deterministic response media type ordering, got %#v", resp.Content)
	}
	if len(resp.Headers) != 1 || resp.Headers[0].Name != "X-Trace" {
		t.Fatalf("expected response header extraction, got %#v", resp.Headers)
	}
}

func TestExtractSecurityRequirementsAndScopes(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "Auth", Version: "1.0.0"},
		Components: &openapi3.Components{
			SecuritySchemes: openapi3.SecuritySchemes{
				"bearerAuth": &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer"}},
				"apiKey":     &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "X-API-Key"}},
			},
		},
		Security: openapi3.SecurityRequirements{
			openapi3.SecurityRequirement{"bearerAuth": []string{"read:items"}},
		},
		Paths: &openapi3.Paths{},
	}
	doc.Paths.Set("/items", &openapi3.PathItem{
		Get: &openapi3.Operation{
			Summary: "items",
			Security: &openapi3.SecurityRequirements{
				openapi3.SecurityRequirement{"apiKey": []string{}},
				openapi3.SecurityRequirement{},
			},
			Responses: openapi3.NewResponses(
				openapi3.WithStatus(200, &openapi3.ResponseRef{
					Value: &openapi3.Response{Description: openapi3.Ptr("OK")},
				}),
			),
		},
	})

	endpoints := extractEndpoints(doc)
	reqs := endpoints[0].SecurityRequirements
	if len(reqs) != 2 {
		t.Fatalf("expected 2 security alternatives, got %#v", reqs)
	}
	if len(reqs[0].Schemes) != 1 || reqs[0].Schemes[0].Name != "apiKey" {
		t.Fatalf("unexpected first security requirement: %#v", reqs[0])
	}
	if len(reqs[1].Schemes) != 0 {
		t.Fatalf("expected anonymous alternative, got %#v", reqs[1])
	}

	scopes := CollectOAuthScopes(doc)
	if len(scopes) != 1 || scopes[0] != "read:items" {
		t.Fatalf("unexpected scopes: %#v", scopes)
	}
}

func TestLoadSpec_XquikOpenAPI31SearchContract(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.1.0",
		Info:    &openapi3.Info{Title: "Xquik API", Version: "1.0"},
		Servers: openapi3.Servers{&openapi3.Server{URL: "https://xquik.com"}},
		Components: &openapi3.Components{
			SecuritySchemes: openapi3.SecuritySchemes{
				"apiKey":      &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{Type: "apiKey", In: "header", Name: "x-api-key"}},
				"oauthBearer": &openapi3.SecuritySchemeRef{Value: &openapi3.SecurityScheme{Type: "http", Scheme: "bearer"}},
			},
		},
		Paths: &openapi3.Paths{},
	}

	search := validOperation("Search tweets")
	search.OperationID = "searchTweets"
	search.Tags = []string{"Tweets"}
	search.Security = &openapi3.SecurityRequirements{
		openapi3.SecurityRequirement{"apiKey": []string{}},
		openapi3.SecurityRequirement{"oauthBearer": []string{}},
		openapi3.SecurityRequirement{},
	}
	search.Parameters = openapi3.Parameters{
		&openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name:     "q",
			In:       openapi3.ParameterInQuery,
			Required: true,
			Schema:   &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		}},
		&openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name: "queryType",
			In:   openapi3.ParameterInQuery,
			Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type:    &openapi3.Types{"string"},
				Default: "Latest",
				Enum:    []any{"Latest", "Top"},
			}},
		}},
		&openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name:   "sinceTime",
			In:     openapi3.ParameterInQuery,
			Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
		}},
		&openapi3.ParameterRef{Value: &openapi3.Parameter{
			Name: "limit",
			In:   openapi3.ParameterInQuery,
			Schema: &openapi3.SchemaRef{Value: &openapi3.Schema{
				Type:    &openapi3.Types{"integer"},
				Default: float64(20),
				Max:     openapi3.Ptr(float64(200)),
			}},
		}},
	}
	doc.Paths.Set("/api/v1/x/tweets/search", &openapi3.PathItem{Get: search})

	endpoints, _, err := LoadSpec(writeDoc(t, doc))
	if err != nil {
		t.Fatalf("LoadSpec(Xquik search): %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	ep := endpoints[0]
	if ep.OperationID != "searchTweets" || ep.BaseURL != "https://xquik.com" {
		t.Fatalf("unexpected endpoint identity: %#v", ep)
	}
	if len(ep.QueryParams) != 4 {
		t.Fatalf("expected 4 query params, got %#v", ep.QueryParams)
	}

	params := map[string]Param{}
	for _, param := range ep.QueryParams {
		params[param.Name] = param
	}
	if !params["q"].Required {
		t.Fatalf("q should be required: %#v", params["q"])
	}
	if got := params["queryType"].Enum; len(got) != 2 || got[0] != "Latest" || got[1] != "Top" {
		t.Fatalf("queryType enum = %#v", got)
	}
	if params["queryType"].Default != "Latest" {
		t.Fatalf("queryType default = %#v", params["queryType"].Default)
	}
	if params["limit"].Maximum == nil || *params["limit"].Maximum != 200 {
		t.Fatalf("limit maximum = %#v", params["limit"].Maximum)
	}

	if len(ep.SecurityRequirements) != 3 {
		t.Fatalf("expected 3 security alternatives, got %#v", ep.SecurityRequirements)
	}
	if ep.SecurityRequirements[0].Schemes[0].ParameterName != "x-api-key" {
		t.Fatalf("api key parameter = %#v", ep.SecurityRequirements[0].Schemes[0])
	}
	if ep.SecurityRequirements[1].Schemes[0].Scheme != "bearer" {
		t.Fatalf("oauth bearer scheme = %#v", ep.SecurityRequirements[1].Schemes[0])
	}
	if len(ep.SecurityRequirements[2].Schemes) != 0 {
		t.Fatalf("anonymous alternative = %#v", ep.SecurityRequirements[2])
	}
}
