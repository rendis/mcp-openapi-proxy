package server

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// GenerateTools creates MCP tools from parsed endpoints and registers them on the server.
func GenerateTools(srv *mcp.Server, endpoints []spec.Endpoint, httpClient *client.Client, authResolver *auth.Resolver, cfg Config) {
	for _, ep := range endpoints {
		if cfg.ExcludeDeprecated && ep.Deprecated {
			continue
		}
		ep := ep
		tool := buildTool(ep, cfg.ToolPrefix)
		handler := buildHandler(ep, httpClient, authResolver, cfg)
		srv.AddTool(tool, handler)
	}
}

func buildTool(ep spec.Endpoint, prefix string) *mcp.Tool {
	return &mcp.Tool{
		Name:         toolName(prefix, ep.Method, ep.Path),
		Description:  buildDescription(ep),
		InputSchema:  buildInputSchema(ep),
		OutputSchema: buildOutputSchema(ep),
		Annotations:  toolAnnotations(ep.Method),
	}
}

func buildDescription(ep spec.Endpoint) string {
	var parts []string

	header := fmt.Sprintf("%s %s", ep.Method, ep.Path)
	if ep.Summary != "" {
		header += " - " + ep.Summary
	}
	parts = append(parts, header)

	if ep.Description != "" {
		parts = append(parts, ep.Description)
	}
	if ep.Deprecated {
		parts = append(parts, "Deprecated: true")
	}
	if ep.OperationID != "" {
		parts = append(parts, "Operation ID: "+ep.OperationID)
	}

	if len(ep.SecurityRequirements) == 0 {
		parts = append(parts, "Auth: none")
	} else {
		var authVariants []string
		for _, req := range ep.SecurityRequirements {
			if len(req.Schemes) == 0 {
				authVariants = append(authVariants, "none")
				continue
			}
			var schemes []string
			for _, scheme := range req.Schemes {
				label := scheme.Name
				if scheme.Type != "" {
					label += " (" + scheme.Type
					if scheme.Scheme != "" {
						label += "/" + scheme.Scheme
					}
					if scheme.In != "" {
						label += " in " + scheme.In
					}
					label += ")"
				}
				if len(scheme.Scopes) > 0 {
					label += " scopes=[" + strings.Join(scheme.Scopes, ", ") + "]"
				}
				schemes = append(schemes, label)
			}
			authVariants = append(authVariants, strings.Join(schemes, " AND "))
		}
		parts = append(parts, "Auth: "+strings.Join(authVariants, " OR "))
	}

	if ep.RequestBody != nil && len(ep.RequestBody.Content) > 0 {
		parts = append(parts, "Request body media types: "+joinContentTypes(ep.RequestBody.Content))
	}
	if len(ep.Responses) > 0 {
		var responses []string
		for _, resp := range ep.Responses {
			label := resp.StatusCode
			if resp.Description != "" {
				label += " " + resp.Description
			}
			if len(resp.Content) > 0 {
				label += " [" + joinContentTypes(resp.Content) + "]"
			}
			responses = append(responses, label)
		}
		parts = append(parts, "Responses: "+strings.Join(responses, "; "))
	}
	if ep.ExternalDocs != "" {
		parts = append(parts, "Docs: "+ep.ExternalDocs)
	}

	return strings.Join(parts, "\n")
}

func joinContentTypes(mediaTypes []spec.MediaType) string {
	if len(mediaTypes) == 0 {
		return "none"
	}
	items := make([]string, 0, len(mediaTypes))
	for _, mt := range mediaTypes {
		items = append(items, mt.ContentType)
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func toolAnnotations(method string) *mcp.ToolAnnotations {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	case "DELETE":
		destructive := true
		return &mcp.ToolAnnotations{DestructiveHint: &destructive}
	default:
		return nil
	}
}

func toolName(prefix, method, path string) string {
	sanitized := sanitizePath(path)
	if sanitized == "" {
		return strings.ToLower(fmt.Sprintf("%s_%s", prefix, method))
	}
	return strings.ToLower(fmt.Sprintf("%s_%s_%s", prefix, method, sanitized))
}

func sanitizePath(path string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"-", "_",
		"{", "_",
		"}", "_",
		".", "_",
	)
	s := replacer.Replace(path)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(strings.ToLower(s), "_")
}

func buildHandler(ep spec.Endpoint, httpClient *client.Client, authResolver *auth.Resolver, cfg Config) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return callEndpoint(ctx, ep, httpClient, authResolver, cfg, req)
	}
}
