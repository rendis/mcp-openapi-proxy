package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// reservedHeaders are HTTP headers managed internally by the client.
// OpenAPI header parameters with these names are excluded from the tool
// schema and not injected into requests to prevent overriding auth or content type.
var reservedHeaders = map[string]bool{
	"authorization": true,
	"content-type":  true,
	"host":          true,
}

func isReservedHeader(name string) bool {
	return reservedHeaders[strings.ToLower(name)]
}

// GenerateTools creates MCP tools from parsed endpoints and registers them on the server.
func GenerateTools(srv *mcp.Server, endpoints []spec.Endpoint, c *client.Client, prefix string) {
	for _, ep := range endpoints {
		ep := ep // capture loop variable
		tool := buildTool(ep, prefix)
		handler := buildHandler(ep, c)
		srv.AddTool(tool, handler)
	}
}

// buildTool constructs the MCP Tool definition from an endpoint.
func buildTool(ep spec.Endpoint, prefix string) *mcp.Tool {
	name := toolName(prefix, ep.Method, ep.Path)

	desc := fmt.Sprintf("%s %s", ep.Method, ep.Path)
	if ep.Summary != "" {
		desc += " — " + ep.Summary
	}

	inputSchema := buildInputSchema(ep)

	return &mcp.Tool{
		Name:        name,
		Description: desc,
		InputSchema: inputSchema,
		Annotations: toolAnnotations(ep.Method),
	}
}

// toolAnnotations returns read-only / destructive hints based on the HTTP method.
func toolAnnotations(method string) *mcp.ToolAnnotations {
	switch method {
	case "GET":
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	case "DELETE":
		destructive := true
		return &mcp.ToolAnnotations{DestructiveHint: &destructive}
	default:
		return nil
	}
}

// toolName builds a sanitized tool name: {prefix}_{method}_{sanitized_path}.
func toolName(prefix, method, path string) string {
	sanitized := sanitizePath(path)
	if sanitized == "" {
		return strings.ToLower(fmt.Sprintf("%s_%s", prefix, strings.ToLower(method)))
	}
	name := fmt.Sprintf("%s_%s_%s", prefix, strings.ToLower(method), sanitized)
	return strings.ToLower(name)
}

// sanitizePath replaces special characters to produce a valid tool name segment.
func sanitizePath(path string) string {
	r := strings.NewReplacer(
		"/", "_",
		"-", "_",
		"{", "_",
		"}", "_",
		".", "_",
	)
	s := r.Replace(path)

	// Collapse multiple underscores.
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}

	// Trim leading/trailing underscores.
	s = strings.Trim(s, "_")

	return strings.ToLower(s)
}

// buildInputSchema constructs a JSON Schema object for the tool input.
func buildInputSchema(ep spec.Endpoint) *jsonschema.Schema {
	schema := &jsonschema.Schema{
		Type:       "object",
		Properties: make(map[string]*jsonschema.Schema),
	}

	var required []string

	// Path parameters.
	for _, p := range ep.PathParams {
		propSchema := paramToSchema(p)
		schema.Properties[p.Name] = propSchema
		if p.Required {
			required = append(required, p.Name)
		}
	}

	// Query parameters.
	for _, p := range ep.QueryParams {
		propSchema := paramToSchema(p)
		schema.Properties[p.Name] = propSchema
		if p.Required {
			required = append(required, p.Name)
		}
	}

	// Header parameters (excluding reserved HTTP headers).
	for _, p := range ep.HeaderParams {
		if isReservedHeader(p.Name) {
			continue
		}
		propSchema := paramToSchema(p)
		schema.Properties[p.Name] = propSchema
		if p.Required {
			required = append(required, p.Name)
		}
	}

	// Request body: nest under "body" property.
	if ep.RequestBody != nil && ep.RequestBody.Schema != nil {
		bodySchema := mapToJSONSchema(ep.RequestBody.Schema)
		if bodySchema != nil {
			schema.Properties["body"] = bodySchema
			if ep.RequestBody.Required {
				required = append(required, "body")
			}
		}
	}

	if len(required) > 0 {
		schema.Required = required
	}

	return schema
}

// paramToSchema converts a spec.Param to a jsonschema.Schema.
func paramToSchema(p spec.Param) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        mapParamType(p.Type),
		Description: p.Description,
	}
	if p.Default != nil {
		data, err := json.Marshal(p.Default)
		if err == nil {
			s.Default = data
		}
	}
	return s
}

// mapParamType maps OpenAPI parameter types to JSON Schema types.
func mapParamType(t string) string {
	switch t {
	case "integer":
		return "integer"
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return "array"
	default:
		return "string"
	}
}

// mapToJSONSchema converts a map[string]any (from OpenAPI Schema) to a
// jsonschema.Schema by marshaling to JSON and back.
func mapToJSONSchema(m map[string]any) *jsonschema.Schema {
	data, err := json.Marshal(m)
	if err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return &jsonschema.Schema{Type: "object"}
	}

	// Ensure the type is set, default to object for body schemas.
	if s.Type == "" && len(s.Types) == 0 {
		s.Type = "object"
	}
	return &s
}

// buildHandler creates a ToolHandler that calls the API client.
func buildHandler(ep spec.Endpoint, c *client.Client) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if req.Params.Arguments != nil {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, fmt.Errorf("unmarshal arguments: %w", err)
			}
		}
		if args == nil {
			args = make(map[string]any)
		}

		// Substitute path parameters.
		path := ep.Path
		for _, p := range ep.PathParams {
			if val, ok := args[p.Name]; ok {
				path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(fmt.Sprintf("%v", val)))
			}
		}

		// Build query string from query parameters.
		query := url.Values{}
		for _, p := range ep.QueryParams {
			if val, ok := args[p.Name]; ok {
				switch v := val.(type) {
				case []interface{}:
					for _, elem := range v {
						query.Add(p.Name, fmt.Sprintf("%v", elem))
					}
				default:
					query.Set(p.Name, fmt.Sprintf("%v", val))
				}
			}
		}

		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}

		// Collect header parameters (excluding reserved HTTP headers).
		var reqHeaders map[string]string
		if len(ep.HeaderParams) > 0 {
			reqHeaders = make(map[string]string, len(ep.HeaderParams))
			for _, p := range ep.HeaderParams {
				if isReservedHeader(p.Name) {
					continue
				}
				if val, ok := args[p.Name]; ok {
					reqHeaders[p.Name] = fmt.Sprintf("%v", val)
				}
			}
		}

		// Extract body.
		var body any
		if ep.RequestBody != nil {
			if b, ok := args["body"]; ok {
				body = b
			}
		}

		// Call the API.
		result, err := c.Do(ctx, ep.Method, path, body, reqHeaders)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("API error: %v", err)},
				},
			}, nil
		}

		// Format result as JSON text.
		text, jsonErr := formatResult(result)
		if jsonErr != nil {
			text = fmt.Sprintf("%v", result)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: text},
			},
		}, nil
	}
}

// formatResult converts the API response to a formatted JSON string.
func formatResult(result any) (string, error) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
