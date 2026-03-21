package spec

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// Endpoint represents a parsed API endpoint from the spec.
type Endpoint struct {
	Method       string       // GET, POST, PUT, PATCH, DELETE
	Path         string       // /admin/features/{key}
	OperationID  string       // operationId from spec (may be empty)
	Summary      string       // short description
	Description  string       // long description
	Tags         []string     // grouping tags
	PathParams   []Param      // path parameters
	QueryParams  []Param      // query parameters
	HeaderParams []Param      // header parameters
	RequestBody  *RequestBody // body schema (nil for GET/DELETE)
	Security     []string     // security scheme names
}

// Param describes an API parameter.
type Param struct {
	Name        string
	Description string
	Required    bool
	Type        string // string, integer, number, boolean, array
	Default     any
	Enum        []any
	Format      string
	Minimum     *float64
	Maximum     *float64
	MinLength   *uint64
	MaxLength   *uint64
}

// RequestBody describes the request body schema.
type RequestBody struct {
	Required    bool
	ContentType string         // typically application/json
	Schema      map[string]any // JSON Schema for the body
}

// LoadSpec loads an OpenAPI 3.x spec from a local file path or HTTP(S) URL.
// It returns the parsed endpoints, the raw OpenAPI document, and any error.
func LoadSpec(source string) ([]Endpoint, *openapi3.T, error) {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	var (
		doc *openapi3.T
		err error
	)

	if isHTTP(source) {
		u, parseErr := url.Parse(source)
		if parseErr != nil {
			return nil, nil, fmt.Errorf("parse spec URL: %w", parseErr)
		}
		loader.ReadFromURIFunc = httpReadFromURI
		doc, err = loader.LoadFromURI(u)
	} else {
		doc, err = loader.LoadFromFile(source)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("load spec from %s: %w", source, err)
	}

	endpoints := extractEndpoints(doc)
	return endpoints, doc, nil
}

// extractEndpoints walks doc.Paths and extracts all operations.
func extractEndpoints(doc *openapi3.T) []Endpoint {
	if doc.Paths == nil {
		return nil
	}

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	}

	var endpoints []Endpoint

	// Iterate paths in sorted order for deterministic output.
	pathMap := doc.Paths.Map()
	paths := make([]string, 0, len(pathMap))
	for p := range pathMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		pathItem := pathMap[path]
		if pathItem == nil {
			continue
		}

		for _, method := range methods {
			op := pathItem.GetOperation(method)
			if op == nil {
				continue
			}

			ep := Endpoint{
				Method:      method,
				Path:        path,
				OperationID: op.OperationID,
				Summary:     op.Summary,
				Description: op.Description,
				Tags:        op.Tags,
			}

			// Collect parameters from both path-level and operation-level.
			allParams := mergeParameters(pathItem.Parameters, op.Parameters)
			for _, pRef := range allParams {
				if pRef == nil || pRef.Value == nil {
					continue
				}
				p := pRef.Value
				param := Param{
					Name:        p.Name,
					Description: p.Description,
					Required:    p.Required,
					Type:        schemaType(p.Schema),
					Default:     schemaDefault(p.Schema),
				}
				if p.Schema != nil && p.Schema.Value != nil {
					s := p.Schema.Value
					param.Enum = s.Enum
					param.Format = s.Format
					param.Minimum = s.Min
					param.Maximum = s.Max
					if s.MinLength != 0 {
						ml := s.MinLength
						param.MinLength = &ml
					}
					param.MaxLength = s.MaxLength
				}
				switch p.In {
				case openapi3.ParameterInPath:
					ep.PathParams = append(ep.PathParams, param)
				case openapi3.ParameterInQuery:
					ep.QueryParams = append(ep.QueryParams, param)
				case openapi3.ParameterInHeader:
					ep.HeaderParams = append(ep.HeaderParams, param)
				case openapi3.ParameterInCookie:
					log.Printf("warning: cookie parameter %q on %s %s is not supported and will be ignored", p.Name, method, path)
				}
			}

			// Request body
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				rb := op.RequestBody.Value
				ct, schema := extractBodySchema(rb)
				if schema != nil {
					ep.RequestBody = &RequestBody{
						Required:    rb.Required,
						ContentType: ct,
						Schema:      schema,
					}
				}
			}

			// Security
			ep.Security = extractSecurityNames(op.Security, doc.Security)

			endpoints = append(endpoints, ep)
		}
	}

	return endpoints
}

// mergeParameters combines path-level and operation-level parameters.
// Operation-level parameters override path-level ones with the same name+in.
func mergeParameters(pathParams, opParams openapi3.Parameters) openapi3.Parameters {
	seen := make(map[string]bool)
	var result openapi3.Parameters

	// Operation params take priority.
	for _, p := range opParams {
		if p != nil && p.Value != nil {
			key := p.Value.In + ":" + p.Value.Name
			seen[key] = true
		}
		result = append(result, p)
	}
	for _, p := range pathParams {
		if p == nil || p.Value == nil {
			continue
		}
		key := p.Value.In + ":" + p.Value.Name
		if seen[key] {
			continue
		}
		result = append(result, p)
	}
	return result
}

// schemaType extracts the primary type string from a SchemaRef.
func schemaType(ref *openapi3.SchemaRef) string {
	if ref == nil || ref.Value == nil || ref.Value.Type == nil {
		return "string"
	}
	types := ref.Value.Type.Slice()
	if len(types) == 0 {
		return "string"
	}
	return types[0]
}

// schemaDefault extracts the default value from a SchemaRef.
func schemaDefault(ref *openapi3.SchemaRef) any {
	if ref == nil || ref.Value == nil {
		return nil
	}
	return ref.Value.Default
}

// extractBodySchema extracts the JSON schema from the request body content.
// It returns the content type and a plain map[string]any schema.
func extractBodySchema(rb *openapi3.RequestBody) (string, map[string]any) {
	if rb.Content == nil {
		return "", nil
	}

	// Prefer application/json.
	for _, ct := range []string{"application/json", "application/merge-patch+json"} {
		mt := rb.Content[ct]
		if mt != nil && mt.Schema != nil {
			schema := schemaRefToMap(mt.Schema)
			return ct, schema
		}
	}

	// Fallback: first available content type that is NOT multipart or form-urlencoded.
	for ct, mt := range rb.Content {
		if strings.HasPrefix(ct, "multipart/") {
			continue
		}
		if strings.HasPrefix(ct, "application/x-www-form") {
			continue
		}
		if mt != nil && mt.Schema != nil {
			schema := schemaRefToMap(mt.Schema)
			return ct, schema
		}
	}

	return "", nil
}

// schemaRefToMap converts an openapi3.SchemaRef to a plain map[string]any
// by marshaling to JSON and back.
func schemaRefToMap(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return nil
	}

	data, err := json.Marshal(ref.Value)
	if err != nil {
		return nil
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}

	// Remove internal fields that are not part of JSON Schema.
	delete(m, "__origin__")

	return m
}

// extractSecurityNames collects security scheme names from an operation's
// security requirements, falling back to the document-level security.
func extractSecurityNames(opSecurity *openapi3.SecurityRequirements, docSecurity openapi3.SecurityRequirements) []string {
	reqs := docSecurity
	if opSecurity != nil {
		reqs = *opSecurity
	}

	var names []string
	seen := make(map[string]bool)
	for _, req := range reqs {
		for name := range req {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// isHTTP returns true if the source starts with http:// or https://.
func isHTTP(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

// httpReadFromURI fetches a spec from an HTTP(S) URL.
func httpReadFromURI(loader *openapi3.Loader, location *url.URL) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(location.String())
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", location, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", location, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", location, err)
	}
	return data, nil
}
