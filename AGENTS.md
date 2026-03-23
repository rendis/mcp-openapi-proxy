# Agent Instructions

## Project
Go CLI that converts OpenAPI 3.x specs into a lightweight MCP stdio navigator/executor — 3 MCP tools plus indexed endpoint `toolName` identifiers, no codegen.

## Architecture
- `cmd/mcp-openapi-proxy/main.go` — CLI entry point, env var parsing, subcommands: serve/login/logout/status
- `pkg/spec/model.go` — internal OpenAPI model types: endpoints, params, media types, security, servers
- `pkg/spec/parser.go` — OpenAPI 3.x parser (kin-openapi), validates the spec and extracts Endpoint structs
- `pkg/server/server.go` — MCP server setup, stdio transport
- `pkg/server/generator.go` — endpoint `toolName` / path sanitization helpers
- `pkg/server/navigator.go` — endpoint index plus the 3 registered MCP tools: list/describe/call
- `pkg/server/schema.go` — JSON Schema generation for the lightweight navigator tool inputs plus response helpers
- `pkg/server/runtime.go` — request serialization, auth application, HTTP execution, MCP envelope responses
- `pkg/client/client.go` — HTTP client execution, content-type aware decoding, response body limits
- `pkg/client/errors.go` — client transport/body limit errors
- `pkg/auth/provider.go` — TokenProvider interface + StaticTokenProvider
- `pkg/auth/resolver.go` — resolves OpenAPI security requirements to concrete HTTP auth material
- `pkg/auth/oidc_provider.go` — OIDC token storage, auto-refresh (30s margin)
- `pkg/auth/discovery.go` — OIDC .well-known/openid-configuration endpoint discovery
- `pkg/auth/login.go` — Browser-based OIDC Authorization Code + PKCE flow
- `pkg/auth/logout.go` — Token file removal
- `pkg/auth/status.go` — Auth state display

## Key Dependencies
- `github.com/getkin/kin-openapi` — OpenAPI 3.x parsing
- `github.com/modelcontextprotocol/go-sdk` — MCP server (go-sdk v0.4.0)
- `github.com/google/jsonschema-go` — JSON Schema for tool inputs

## Build & Run
- `go build -C . -o bin/mcp-openapi-proxy ./cmd/mcp-openapi-proxy`
- Config is 100% env vars: `MCP_SPEC`, optional `MCP_BASE_URL`, `MCP_TOOL_PREFIX`, `MCP_AUTH_PROFILE`, `MCP_AUTH_TOKEN`, `MCP_OIDC_ISSUER`, `MCP_OIDC_CLIENT_ID`, `MCP_OIDC_SCOPES`, `MCP_EXTRA_HEADERS`, `MCP_MAX_BODY_BYTES`, `MCP_ALLOW_INSECURE_HTTP`, `MCP_EXCLUDE_DEPRECATED`, plus `MCP_AUTH_<SCHEME>_*`

## Conventions
- Tests: `go test ./...` — run before committing
- Registered MCP tools: `{prefix}_list_endpoints`, `{prefix}_describe_endpoint`, `{prefix}_call_endpoint`
- Endpoint IDs: `{prefix}_{method}_{sanitized_path}` (lowercase, special chars → `_`, collapsed)
- Auth resolution priority: per-scheme `MCP_AUTH_<SCHEME>_*` > global `MCP_AUTH_TOKEN` > OIDC token cache for `MCP_AUTH_PROFILE`
- Tokens stored at `~/.mcp-openapi-proxy/{profile}-tokens.json` with 0600 perms
- `list_endpoints` returns lightweight discovery items with `toolName`, `method`, `path`, `description`, `requiredAuth`, `tags`, `deprecated`
- `describe_endpoint` returns the full normalized OpenAPI contract for one endpoint
- `call_endpoint` input uses `toolName` plus `path`, `query`, `headers`, `cookies`, `body`
- `call_endpoint` output is always an envelope: `{status, content_type, headers, body}` and marks API `4xx/5xx` as `IsError=true`
- Deprecated endpoints are registered by default and skipped only when `MCP_EXCLUDE_DEPRECATED=1`
- `client.Do()` returns `*client.Response` with StatusCode, Headers, ContentType, RawContentType, Body
- stdio transport only

## Gotchas
- `go-sdk` API may change — check import paths if build fails
- `jsonschema-go` uses `json.RawMessage` for Default field
- kin-openapi `SchemaRef.Value.Type` returns `*openapi3.Types` (slice), not a string — use `.Slice()`
- Version hardcoded as `0.1.0` in `server.go`
- `swag init` generates Swagger 2.0 output; convert `swagger.json` with `swagger2openapi` before `LoadSpec` / `MCP_SPEC` because this repo only supports OpenAPI 3.x at runtime
- Authenticated requests to non-loopback `http://` URLs are blocked unless `MCP_ALLOW_INSECURE_HTTP=1`
- The registered MCP tools intentionally omit `OutputSchema` to keep `tools/list` small; detailed endpoint contracts come from `describe_endpoint`
- `multipart/form-data`, `application/x-www-form-urlencoded`, text bodies, and `application/octet-stream` are supported
- Path/query/header/cookie serialization follows OpenAPI `style` / `explode`; path params are URL-encoded
- Non-JSON API responses are returned as raw text strings; binary responses are wrapped as base64 objects
- Trailing slash on `MCP_BASE_URL` is stripped automatically
- OIDC token refresh uses detached context (context.Background)
- Token file writes are atomic (tmp + rename)
