package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// EndpointOpt is a functional option for building test Endpoints.
type EndpointOpt func(*spec.Endpoint)

// WithPathParam adds a path parameter to the endpoint.
func WithPathParam(name, typ string, required bool) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.PathParams = append(ep.PathParams, spec.Param{
			Name:     name,
			Type:     typ,
			Required: required,
		})
	}
}

// WithQueryParam adds a query parameter to the endpoint.
func WithQueryParam(name, typ string, required bool) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.QueryParams = append(ep.QueryParams, spec.Param{
			Name:     name,
			Type:     typ,
			Required: required,
		})
	}
}

// WithHeaderParam adds a header parameter to the endpoint.
func WithHeaderParam(name, typ string, required bool) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.HeaderParams = append(ep.HeaderParams, spec.Param{
			Name:     name,
			Type:     typ,
			Required: required,
		})
	}
}

// WithBody adds a request body with the given schema map.
func WithBody(required bool, schema map[string]any) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.RequestBody = &spec.RequestBody{
			Required: required,
			Content: []spec.MediaType{
				{
					ContentType: "application/json",
					Schema:      schema,
				},
			},
		}
	}
}

// WithBodyContentType adds a request body with a specific content type.
func WithBodyContentType(required bool, contentType string, schema map[string]any) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.RequestBody = &spec.RequestBody{
			Required: required,
			Content: []spec.MediaType{
				{
					ContentType: contentType,
					Schema:      schema,
				},
			},
		}
	}
}

// WithSummary sets the summary on the endpoint.
func WithSummary(s string) EndpointOpt {
	return func(ep *spec.Endpoint) {
		ep.Summary = s
	}
}

// MakeEndpoint builds a test Endpoint with functional options.
func MakeEndpoint(method, path string, opts ...EndpointOpt) spec.Endpoint {
	ep := spec.Endpoint{
		Method: method,
		Path:   path,
	}
	for _, opt := range opts {
		opt(&ep)
	}
	return ep
}

// WriteSpecFile writes an OpenAPI spec to a temp YAML file and returns the path.
func WriteSpecFile(t *testing.T, doc *openapi3.T) string {
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

// MinimalSpec returns a minimal valid OpenAPI 3.0 spec with the given paths.
func MinimalSpec(paths map[string]*openapi3.PathItem) *openapi3.T {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:   "Test API",
			Version: "1.0.0",
		},
		Paths: &openapi3.Paths{},
	}
	for p, item := range paths {
		doc.Paths.Set(p, item)
	}
	return doc
}

// SimplePathItem creates a PathItem with a single operation on the given method.
func SimplePathItem(method string, op *openapi3.Operation) *openapi3.PathItem {
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

// SimpleOperation creates a basic operation with optional parameters.
func SimpleOperation(summary string, params ...*openapi3.ParameterRef) *openapi3.Operation {
	return &openapi3.Operation{
		Summary:    summary,
		Parameters: params,
	}
}

// PathParam creates an OpenAPI path parameter reference.
func PathParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       "path",
			Required: required,
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{typ},
				},
			},
		},
	}
}

// QueryParamRef creates an OpenAPI query parameter reference.
func QueryParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       "query",
			Required: required,
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{typ},
				},
			},
		},
	}
}

// HeaderParamRef creates an OpenAPI header parameter reference.
func HeaderParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       "header",
			Required: required,
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{typ},
				},
			},
		},
	}
}

// CookieParamRef creates an OpenAPI cookie parameter reference.
func CookieParamRef(name, typ string, required bool) *openapi3.ParameterRef {
	return &openapi3.ParameterRef{
		Value: &openapi3.Parameter{
			Name:     name,
			In:       "cookie",
			Required: required,
			Schema: &openapi3.SchemaRef{
				Value: &openapi3.Schema{
					Type: &openapi3.Types{typ},
				},
			},
		},
	}
}

// JSONBodyRef creates an OpenAPI request body with application/json content.
func JSONBodyRef(required bool, schema *openapi3.Schema) *openapi3.RequestBodyRef {
	return &openapi3.RequestBodyRef{
		Value: &openapi3.RequestBody{
			Required: required,
			Content: openapi3.Content{
				"application/json": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{Value: schema},
				},
			},
		},
	}
}

// MultipartBodyRef creates an OpenAPI request body with multipart/form-data content.
func MultipartBodyRef(required bool) *openapi3.RequestBodyRef {
	return &openapi3.RequestBodyRef{
		Value: &openapi3.RequestBody{
			Required: required,
			Content: openapi3.Content{
				"multipart/form-data": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{Type: &openapi3.Types{"object"}},
					},
				},
			},
		},
	}
}

// FormURLEncodedBodyRef creates a request body with application/x-www-form-urlencoded.
func FormURLEncodedBodyRef(required bool) *openapi3.RequestBodyRef {
	return &openapi3.RequestBodyRef{
		Value: &openapi3.RequestBody{
			Required: required,
			Content: openapi3.Content{
				"application/x-www-form-urlencoded": &openapi3.MediaType{
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{Type: &openapi3.Types{"object"}},
					},
				},
			},
		},
	}
}
