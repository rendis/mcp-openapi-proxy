package server

import (
	"encoding/json"
	"mime"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

func buildInputSchema(ep spec.Endpoint) *jsonschema.Schema {
	root := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}

	properties := root["properties"].(map[string]any)
	var required []string

	if section := buildParameterSectionSchema(ep.PathParams); section != nil {
		properties["path"] = section
		if hasRequiredParams(ep.PathParams) {
			required = append(required, "path")
		}
	}
	if section := buildParameterSectionSchema(ep.QueryParams); section != nil {
		properties["query"] = section
		if hasRequiredParams(ep.QueryParams) {
			required = append(required, "query")
		}
	}
	if section := buildParameterSectionSchema(ep.HeaderParams); section != nil {
		properties["headers"] = section
		if hasRequiredParams(ep.HeaderParams) {
			required = append(required, "headers")
		}
	}
	if section := buildParameterSectionSchema(ep.CookieParams); section != nil {
		properties["cookies"] = section
		if hasRequiredParams(ep.CookieParams) {
			required = append(required, "cookies")
		}
	}
	if body := buildRequestBodySchema(ep.RequestBody); body != nil {
		properties["body"] = body
		if ep.RequestBody != nil && ep.RequestBody.Required {
			required = append(required, "body")
		}
	}

	if len(required) > 0 {
		root["required"] = required
	}
	return mapToJSONSchema(root)
}

func buildParameterSectionSchema(params []spec.Param) map[string]any {
	if len(params) == 0 {
		return nil
	}

	props := map[string]any{}
	var required []string
	for _, param := range params {
		props[param.Name] = buildParamSchema(param)
		if param.Required {
			required = append(required, param.Name)
		}
	}

	section := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		section["required"] = required
	}
	return section
}

func buildParamSchema(param spec.Param) map[string]any {
	schema := cloneSchemaMap(param.Schema)
	if schema == nil {
		schema = map[string]any{"type": fallbackParamType(param)}
	}
	if desc := strings.TrimSpace(param.Description); desc != "" {
		if _, ok := schema["description"]; !ok {
			schema["description"] = desc
		}
	}
	if len(param.Examples) > 0 {
		schema["examples"] = param.Examples
	}
	if param.Deprecated {
		schema["deprecated"] = true
	}
	return adaptInputSchemaForContentType(schema, param.ContentType)
}

func buildRequestBodySchema(body *spec.RequestBody) map[string]any {
	if body == nil || len(body.Content) == 0 {
		return nil
	}

	if len(body.Content) == 1 {
		return adaptBodySchema(body.Content[0])
	}

	var enum []any
	var variants []any
	for _, mt := range body.Content {
		enum = append(enum, mt.ContentType)
		variants = append(variants, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content_type": map[string]any{"const": mt.ContentType},
				"value":        adaptBodySchema(mt),
			},
			"required":             []string{"content_type", "value"},
			"additionalProperties": false,
		})
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content_type": map[string]any{"type": "string", "enum": enum},
			"value":        map[string]any{},
		},
		"required":             []string{"content_type", "value"},
		"additionalProperties": false,
		"oneOf":                variants,
	}
}

func buildOutputSchema(ep spec.Endpoint) *jsonschema.Schema {
	root := envelopeSchemaMap(map[string]any{}, map[string]any{
		"status":       map[string]any{"type": "integer"},
		"content_type": map[string]any{"type": "string"},
		"headers":      responseHeadersSchemaMap(),
		"body":         map[string]any{},
		"proxy_error":  proxyErrorSchemaMap(),
	}, nil)

	var variants []any
	for _, resp := range ep.Responses {
		if len(resp.Content) == 0 {
			variants = append(variants, responseEnvelopeVariant(resp.StatusCode, "", map[string]any{"type": "null"}))
			continue
		}
		for _, mt := range resp.Content {
			variants = append(variants, responseEnvelopeVariant(resp.StatusCode, mt.ContentType, adaptOutputSchemaForContentType(mt.Schema, mt.ContentType)))
		}
	}

	variants = append(variants, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":       map[string]any{"type": "integer"},
			"content_type": map[string]any{"type": "string"},
			"headers":      responseHeadersSchemaMap(),
			"body":         map[string]any{},
		},
		"required":             []string{"status", "content_type", "headers", "body"},
		"additionalProperties": false,
	})
	variants = append(variants, proxyErrorEnvelopeVariant())
	root["oneOf"] = variants
	return mapToJSONSchema(root)
}

func responseEnvelopeVariant(statusCode, contentType string, bodySchema map[string]any) map[string]any {
	props := map[string]any{
		"status":  map[string]any{"type": "integer"},
		"headers": responseHeadersSchemaMap(),
		"body":    bodySchema,
	}
	if n, ok := statusCodeAsNumber(statusCode); ok {
		props["status"] = map[string]any{"type": "integer", "const": n}
	}
	if contentType != "" {
		props["content_type"] = map[string]any{"type": "string", "const": contentType}
	} else {
		props["content_type"] = map[string]any{"type": "string"}
	}
	return envelopeSchemaMap(nil, props, []string{"status", "content_type", "headers", "body"})
}

func proxyErrorEnvelopeVariant() map[string]any {
	return envelopeSchemaMap(nil, map[string]any{
		"status":       map[string]any{"type": "integer", "const": 0},
		"content_type": map[string]any{"type": "string"},
		"headers":      responseHeadersSchemaMap(),
		"body":         map[string]any{"type": "null"},
		"proxy_error":  proxyErrorSchemaMap(),
	}, []string{"status", "content_type", "headers", "body", "proxy_error"})
}

func envelopeSchemaMap(base map[string]any, properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if base != nil {
		for k, v := range base {
			schema[k] = v
		}
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func responseHeadersSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "string",
			},
		},
	}
}

func proxyErrorSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code":    map[string]any{"type": "string"},
			"message": map[string]any{"type": "string"},
			"details": map[string]any{},
		},
		"required":             []string{"code", "message"},
		"additionalProperties": false,
	}
}

func adaptBodySchema(mt spec.MediaType) map[string]any {
	return adaptInputSchemaForContentType(cloneSchemaMap(mt.Schema), mt.ContentType)
}

func adaptInputSchemaForContentType(schema map[string]any, contentType string) map[string]any {
	base, _, _ := mime.ParseMediaType(contentType)
	switch {
	case isBinaryMediaType(base):
		return binaryInputSchemaMap()
	case base == "multipart/form-data":
		return replaceBinaryLeafSchemas(schema)
	default:
		if schema == nil {
			return map[string]any{}
		}
		return schema
	}
}

func adaptOutputSchemaForContentType(schema map[string]any, contentType string) map[string]any {
	base, _, _ := mime.ParseMediaType(contentType)
	switch {
	case isBinaryMediaType(base):
		return binaryOutputSchemaMap()
	case isTextMediaType(base):
		if schema == nil {
			return map[string]any{"type": "string"}
		}
		return schema
	default:
		if schema == nil {
			return map[string]any{"type": "null"}
		}
		return schema
	}
}

func replaceBinaryLeafSchemas(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object"}
	}
	return walkSchema(schema, func(node map[string]any) map[string]any {
		if format, _ := node["format"].(string); format == "binary" {
			return binaryInputSchemaMap()
		}
		return node
	})
}

func walkSchema(schema map[string]any, visit func(map[string]any) map[string]any) map[string]any {
	schema = cloneSchemaMap(schema)
	if schema == nil {
		return nil
	}

	if props, ok := schema["properties"].(map[string]any); ok {
		next := make(map[string]any, len(props))
		for k, v := range props {
			if child, ok := v.(map[string]any); ok {
				next[k] = walkSchema(child, visit)
			} else {
				next[k] = v
			}
		}
		schema["properties"] = next
	}
	if items, ok := schema["items"].(map[string]any); ok {
		schema["items"] = walkSchema(items, visit)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if list, ok := schema[key].([]any); ok {
			next := make([]any, 0, len(list))
			for _, item := range list {
				if child, ok := item.(map[string]any); ok {
					next = append(next, walkSchema(child, visit))
				} else {
					next = append(next, item)
				}
			}
			schema[key] = next
		}
	}
	return visit(schema)
}

func binaryInputSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{
				"type": "string",
				"enum": []any{"base64", "path"},
			},
			"data_base64": map[string]any{"type": "string"},
			"path":        map[string]any{"type": "string"},
			"filename":    map[string]any{"type": "string"},
			"content_type": map[string]any{
				"type": "string",
			},
		},
		"required":             []string{"source"},
		"additionalProperties": false,
	}
}

func binaryOutputSchemaMap() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"encoding":    map[string]any{"type": "string", "const": "base64"},
			"data_base64": map[string]any{"type": "string"},
			"size_bytes":  map[string]any{"type": "integer"},
		},
		"required":             []string{"encoding", "data_base64", "size_bytes"},
		"additionalProperties": false,
	}
}

func fallbackParamType(param spec.Param) string {
	switch param.Type {
	case "integer", "number", "boolean", "array", "object":
		return param.Type
	default:
		return "string"
	}
}

func hasRequiredParams(params []spec.Param) bool {
	for _, param := range params {
		if param.Required {
			return true
		}
	}
	return false
}

func statusCodeAsNumber(statusCode string) (int, bool) {
	var n int
	if err := json.Unmarshal([]byte(statusCode), &n); err != nil {
		return 0, false
	}
	return n, true
}

func isBinaryMediaType(contentType string) bool {
	switch {
	case contentType == "application/octet-stream":
		return true
	case strings.HasPrefix(contentType, "image/"):
		return true
	case strings.HasPrefix(contentType, "audio/"):
		return true
	case strings.HasPrefix(contentType, "video/"):
		return true
	case contentType == "application/pdf":
		return true
	case contentType == "application/zip":
		return true
	default:
		return false
	}
}

func isTextMediaType(contentType string) bool {
	switch {
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/xml":
		return true
	case strings.HasSuffix(contentType, "+xml"):
		return true
	default:
		return false
	}
}

func mapToJSONSchema(data map[string]any) *jsonschema.Schema {
	if data == nil {
		return nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		return &jsonschema.Schema{Type: "object"}
	}
	if schema.Type == "" {
		schema.Type = "object"
	}
	return &schema
}

func cloneSchemaMap(schema map[string]any) map[string]any {
	if schema == nil {
		return nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		out := make(map[string]any, len(schema))
		for k, v := range schema {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
