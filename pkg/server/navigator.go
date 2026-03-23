package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

const (
	defaultEndpointListLimit = 50
	maxEndpointListLimit     = 200
)

type endpointEntry struct {
	ToolName      string
	Endpoint      spec.Endpoint
	Description   string
	RequiredAuth  string
	authKinds     map[string]bool
	searchContent string
}

type endpointCatalog struct {
	entries    []endpointEntry
	byToolName map[string]endpointEntry
	toolPrefix string
}

type endpointListFilter struct {
	Query      string
	Tag        string
	PathPrefix string
	Method     string
	Auth       string
	Deprecated *bool
	Cursor     string
	Limit      int
}

// GenerateTools registers the lightweight navigator/executor tool surface.
func GenerateTools(srv *mcp.Server, endpoints []spec.Endpoint, httpClient *client.Client, authResolver *auth.Resolver, cfg Config) {
	catalog := newEndpointCatalog(endpoints, cfg.ToolPrefix, cfg.ExcludeDeprecated)

	srv.AddTool(buildListEndpointsTool(cfg.ToolPrefix), buildListEndpointsHandler(catalog))
	srv.AddTool(buildDescribeEndpointTool(cfg.ToolPrefix), buildDescribeEndpointHandler(catalog, cfg))
	srv.AddTool(buildCallEndpointTool(cfg.ToolPrefix), buildCallEndpointHandler(catalog, httpClient, authResolver, cfg))
}

func newEndpointCatalog(endpoints []spec.Endpoint, prefix string, excludeDeprecated bool) *endpointCatalog {
	entries := make([]endpointEntry, 0, len(endpoints))
	byToolName := make(map[string]endpointEntry, len(endpoints))

	for _, ep := range endpoints {
		if excludeDeprecated && ep.Deprecated {
			continue
		}
		entry := endpointEntry{
			ToolName:      toolName(prefix, ep.Method, ep.Path),
			Endpoint:      ep,
			Description:   describeListEndpoint(ep),
			RequiredAuth:  summarizeRequiredAuth(ep.SecurityRequirements),
			authKinds:     authKinds(ep.SecurityRequirements),
			searchContent: endpointSearchContent(prefix, ep),
		}
		entries = append(entries, entry)
		byToolName[entry.ToolName] = entry
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ToolName < entries[j].ToolName
	})

	return &endpointCatalog{
		entries:    entries,
		byToolName: byToolName,
		toolPrefix: prefix,
	}
}

func (c *endpointCatalog) count() int {
	if c == nil {
		return 0
	}
	return len(c.entries)
}

func buildListEndpointsTool(prefix string) *mcp.Tool {
	return &mcp.Tool{
		Name:        listEndpointsToolName(prefix),
		Description: "List indexed OpenAPI endpoints with lightweight metadata and pagination.",
		InputSchema: buildListEndpointsInputSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func buildDescribeEndpointTool(prefix string) *mcp.Tool {
	return &mcp.Tool{
		Name:        describeEndpointToolName(prefix),
		Description: "Return the full OpenAPI contract for one indexed endpoint.",
		InputSchema: buildDescribeEndpointInputSchema(),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func buildCallEndpointTool(prefix string) *mcp.Tool {
	return &mcp.Tool{
		Name:        callEndpointToolName(prefix),
		Description: "Execute one indexed endpoint by toolName using path/query/headers/cookies/body arguments.",
		InputSchema: buildCallEndpointInputSchema(),
	}
}

func buildListEndpointsHandler(catalog *endpointCatalog) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := decodeArguments(req)
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		filter, err := parseEndpointListFilter(args)
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		filtered := filterEndpointEntries(catalog.entries, filter)
		start, err := decodeListCursor(filter.Cursor)
		if err != nil {
			return errorResult("invalid_cursor", err.Error(), nil), nil
		}
		if start < 0 || start > len(filtered) {
			return errorResult("invalid_cursor", "cursor is out of range", map[string]any{"cursor": filter.Cursor}), nil
		}

		end := start + filter.Limit
		if end > len(filtered) {
			end = len(filtered)
		}
		nextCursor := ""
		if end < len(filtered) {
			nextCursor = encodeListCursor(end)
		}

		items := make([]any, 0, end-start)
		for _, entry := range filtered[start:end] {
			items = append(items, map[string]any{
				"toolName":     entry.ToolName,
				"method":       entry.Endpoint.Method,
				"path":         entry.Endpoint.Path,
				"description":  entry.Description,
				"requiredAuth": entry.RequiredAuth,
				"tags":         cloneStrings(entry.Endpoint.Tags),
				"deprecated":   entry.Endpoint.Deprecated,
			})
		}

		return toolResult(map[string]any{
			"items":      items,
			"nextCursor": nextCursor,
		}, false), nil
	}
}

func buildDescribeEndpointHandler(catalog *endpointCatalog, cfg Config) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := decodeArguments(req)
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		name, err := requiredStringArg(args, "toolName")
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		entry, ok := catalog.byToolName[name]
		if !ok {
			return errorResult("unknown_tool_name", fmt.Sprintf("endpoint %q is not indexed", name), nil), nil
		}

		return toolResult(describeEndpointPayload(entry, cfg), false), nil
	}
}

func buildCallEndpointHandler(catalog *endpointCatalog, httpClient *client.Client, authResolver *auth.Resolver, cfg Config) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := decodeArguments(req)
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		name, err := requiredStringArg(args, "toolName")
		if err != nil {
			return errorResult("invalid_arguments", err.Error(), nil), nil
		}

		entry, ok := catalog.byToolName[name]
		if !ok {
			return errorResult("unknown_tool_name", fmt.Sprintf("endpoint %q is not indexed", name), nil), nil
		}

		return callEndpointArgs(ctx, entry.Endpoint, httpClient, authResolver, cfg, args)
	}
}

func listEndpointsToolName(prefix string) string {
	return strings.ToLower(prefix + "_list_endpoints")
}

func describeEndpointToolName(prefix string) string {
	return strings.ToLower(prefix + "_describe_endpoint")
}

func callEndpointToolName(prefix string) string {
	return strings.ToLower(prefix + "_call_endpoint")
}

func parseEndpointListFilter(args map[string]any) (endpointListFilter, error) {
	filter := endpointListFilter{Limit: defaultEndpointListLimit}
	var err error

	if filter.Query, err = optionalStringArg(args, "q"); err != nil {
		return endpointListFilter{}, err
	}
	if filter.Tag, err = optionalStringArg(args, "tag"); err != nil {
		return endpointListFilter{}, err
	}
	if filter.PathPrefix, err = optionalStringArg(args, "path_prefix"); err != nil {
		return endpointListFilter{}, err
	}
	if filter.Method, err = optionalStringArg(args, "method"); err != nil {
		return endpointListFilter{}, err
	}
	filter.Method = strings.ToUpper(filter.Method)
	if filter.Auth, err = optionalStringArg(args, "auth"); err != nil {
		return endpointListFilter{}, err
	}
	filter.Auth = normalizeAuthFilter(filter.Auth)
	if filter.Cursor, err = optionalStringArg(args, "cursor"); err != nil {
		return endpointListFilter{}, err
	}
	if filter.Deprecated, err = optionalBoolArg(args, "deprecated"); err != nil {
		return endpointListFilter{}, err
	}
	if limit, ok, err := optionalPositiveIntArg(args, "limit"); err != nil {
		return endpointListFilter{}, err
	} else if ok {
		if limit > maxEndpointListLimit {
			limit = maxEndpointListLimit
		}
		filter.Limit = limit
	}
	return filter, nil
}

func filterEndpointEntries(entries []endpointEntry, filter endpointListFilter) []endpointEntry {
	out := make([]endpointEntry, 0, len(entries))
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	tag := strings.ToLower(strings.TrimSpace(filter.Tag))

	for _, entry := range entries {
		if filter.Method != "" && entry.Endpoint.Method != filter.Method {
			continue
		}
		if filter.PathPrefix != "" && !strings.HasPrefix(entry.Endpoint.Path, filter.PathPrefix) {
			continue
		}
		if filter.Auth != "" && !entry.authKinds[filter.Auth] {
			continue
		}
		if filter.Deprecated != nil && entry.Endpoint.Deprecated != *filter.Deprecated {
			continue
		}
		if tag != "" && !hasTag(entry.Endpoint.Tags, tag) {
			continue
		}
		if query != "" && !strings.Contains(entry.searchContent, query) {
			continue
		}
		out = append(out, entry)
	}

	return out
}

func describeEndpointPayload(entry endpointEntry, cfg Config) map[string]any {
	ep := entry.Endpoint

	parameters := map[string]any{}
	if section := buildParameterSectionSchema(ep.PathParams); section != nil {
		parameters["path"] = section
	}
	if section := buildParameterSectionSchema(ep.QueryParams); section != nil {
		parameters["query"] = section
	}
	if section := buildParameterSectionSchema(ep.HeaderParams); section != nil {
		parameters["headers"] = section
	}
	if section := buildParameterSectionSchema(ep.CookieParams); section != nil {
		parameters["cookies"] = section
	}

	return map[string]any{
		"toolName":             entry.ToolName,
		"method":               ep.Method,
		"path":                 ep.Path,
		"summary":              ep.Summary,
		"description":          ep.Description,
		"operationId":          ep.OperationID,
		"tags":                 cloneStrings(ep.Tags),
		"deprecated":           ep.Deprecated,
		"requiredAuth":         entry.RequiredAuth,
		"securityRequirements": securityRequirementsToList(ep.SecurityRequirements),
		"parameters":           parameters,
		"requestBody":          requestBodyDescription(ep.RequestBody),
		"responses":            responsesDescription(ep.Responses),
		"externalDocs":         ep.ExternalDocs,
		"servers":              serversDescription(ep.Servers),
		"baseURLHint":          describeBaseURLHint(ep, cfg),
	}
}

func requestBodyDescription(body *spec.RequestBody) any {
	if body == nil {
		return nil
	}
	content := make([]any, 0, len(body.Content))
	for _, mt := range body.Content {
		content = append(content, map[string]any{
			"contentType": mt.ContentType,
			"schema":      adaptBodySchema(mt),
			"examples":    cloneAnySlice(mt.Examples),
			"encoding":    encodingDescription(mt.Encoding),
		})
	}
	return map[string]any{
		"required": body.Required,
		"content":  content,
	}
}

func responsesDescription(responses []spec.ResponseInfo) []any {
	out := make([]any, 0, len(responses))
	for _, resp := range responses {
		content := make([]any, 0, len(resp.Content))
		for _, mt := range resp.Content {
			content = append(content, map[string]any{
				"contentType": mt.ContentType,
				"schema":      adaptOutputSchemaForContentType(mt.Schema, mt.ContentType),
				"examples":    cloneAnySlice(mt.Examples),
			})
		}
		out = append(out, map[string]any{
			"status":      resp.StatusCode,
			"description": resp.Description,
			"headers":     responseHeadersDescription(resp.Headers),
			"content":     content,
		})
	}
	return out
}

func responseHeadersDescription(headers []spec.ResponseHeader) map[string]any {
	out := make(map[string]any, len(headers))
	for _, header := range headers {
		out[header.Name] = map[string]any{
			"description": header.Description,
			"required":    header.Required,
			"schema":      cloneSchemaMap(header.Schema),
		}
	}
	return out
}

func encodingDescription(encodings map[string]spec.Encoding) map[string]any {
	if len(encodings) == 0 {
		return map[string]any{}
	}
	keys := make([]string, 0, len(encodings))
	for key := range encodings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(keys))
	for _, key := range keys {
		enc := encodings[key]
		out[key] = map[string]any{
			"contentType":   enc.ContentType,
			"style":         enc.Style,
			"explode":       enc.Explode,
			"allowReserved": enc.AllowReserved,
		}
	}
	return out
}

func securityRequirementsToList(reqs []spec.SecurityRequirement) []any {
	out := make([]any, 0, len(reqs))
	for _, req := range reqs {
		schemes := make([]any, 0, len(req.Schemes))
		for _, scheme := range req.Schemes {
			schemes = append(schemes, map[string]any{
				"name":             scheme.Name,
				"type":             scheme.Type,
				"in":               scheme.In,
				"parameterName":    scheme.ParameterName,
				"scheme":           scheme.Scheme,
				"bearerFormat":     scheme.BearerFormat,
				"description":      scheme.Description,
				"openIdConnectUrl": scheme.OpenIDConnectURL,
				"scopes":           cloneStrings(scheme.Scopes),
			})
		}
		out = append(out, map[string]any{"schemes": schemes})
	}
	return out
}

func serversDescription(servers []spec.ServerInfo) []any {
	out := make([]any, 0, len(servers))
	for _, server := range servers {
		out = append(out, map[string]any{
			"url":         server.URL,
			"description": server.Description,
		})
	}
	return out
}

func describeBaseURLHint(ep spec.Endpoint, cfg Config) string {
	if cfg.BaseURL != "" {
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	if ep.BaseURL != "" {
		return strings.TrimRight(ep.BaseURL, "/")
	}
	return ""
}

func describeListEndpoint(ep spec.Endpoint) string {
	if summary := firstNonEmptyLine(ep.Summary); summary != "" {
		return summary
	}
	return firstNonEmptyLine(ep.Description)
}

func endpointSearchContent(prefix string, ep spec.Endpoint) string {
	parts := []string{
		toolName(prefix, ep.Method, ep.Path),
		ep.Method,
		ep.Path,
		ep.OperationID,
		ep.Summary,
		ep.Description,
	}
	parts = append(parts, ep.Tags...)
	return strings.ToLower(strings.Join(parts, "\n"))
}

func summarizeRequiredAuth(reqs []spec.SecurityRequirement) string {
	if len(reqs) == 0 {
		return "none"
	}

	var variants []string
	for _, req := range reqs {
		if len(req.Schemes) == 0 {
			variants = append(variants, "none")
			continue
		}

		parts := make([]string, 0, len(req.Schemes))
		seen := map[string]bool{}
		for _, scheme := range req.Schemes {
			label := canonicalAuthKind(scheme)
			if seen[label] {
				continue
			}
			seen[label] = true
			parts = append(parts, label)
		}
		variants = append(variants, strings.Join(parts, " + "))
	}

	return strings.Join(dedupeStrings(variants), " OR ")
}

func authKinds(reqs []spec.SecurityRequirement) map[string]bool {
	out := map[string]bool{}
	if len(reqs) == 0 {
		out["none"] = true
		return out
	}
	for _, req := range reqs {
		if len(req.Schemes) == 0 {
			out["none"] = true
			continue
		}
		for _, scheme := range req.Schemes {
			out[canonicalAuthKind(scheme)] = true
		}
	}
	return out
}

func canonicalAuthKind(scheme spec.SecurityInfo) string {
	switch {
	case strings.EqualFold(scheme.Type, "http") && strings.EqualFold(scheme.Scheme, "bearer"):
		return "bearer"
	case strings.EqualFold(scheme.Type, "http") && strings.EqualFold(scheme.Scheme, "basic"):
		return "basic"
	case strings.EqualFold(scheme.Type, "apikey"):
		return "apiKey"
	case strings.EqualFold(scheme.Type, "oauth2"):
		return "oauth2"
	case strings.EqualFold(scheme.Type, "openidconnect"):
		return "openIdConnect"
	case scheme.Scheme != "":
		return scheme.Scheme
	case scheme.Type != "":
		return scheme.Type
	default:
		return scheme.Name
	}
}

func normalizeAuthFilter(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer("-", "", "_", "", " ", "")
	normalized = replacer.Replace(normalized)

	switch normalized {
	case "", "all":
		return ""
	case "none":
		return "none"
	case "bearer", "bearertoken":
		return "bearer"
	case "basic":
		return "basic"
	case "apikey", "apikeyauth":
		return "apiKey"
	case "oauth2":
		return "oauth2"
	case "openidconnect", "oidc":
		return "openIdConnect"
	default:
		return raw
	}
}

func encodeListCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeListCursor(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, fmt.Errorf("parse cursor: %w", err)
	}
	return offset, nil
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value, err := optionalStringArg(args, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalStringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(value), nil
}

func optionalBoolArg(args map[string]any, key string) (*bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return nil, fmt.Errorf("%s must be a boolean", key)
	}
	return &value, nil
}

func optionalPositiveIntArg(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false, nil
	}

	switch value := raw.(type) {
	case int:
		if value <= 0 {
			return 0, false, fmt.Errorf("%s must be a positive integer", key)
		}
		return value, true, nil
	case float64:
		if value <= 0 || value != float64(int(value)) {
			return 0, false, fmt.Errorf("%s must be a positive integer", key)
		}
		return int(value), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be a positive integer", key)
	}
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	out := make([]string, len(items))
	copy(out, items)
	return out
}

func cloneAnySlice(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	out := make([]any, len(items))
	copy(out, items)
	return out
}

func errorResult(code, message string, details any) *mcp.CallToolResult {
	return toolResult(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	}, true)
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
