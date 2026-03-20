# Agent Instructions

## Project
Go CLI that converts OpenAPI 3.x specs into MCP stdio servers dynamically — one tool per endpoint, no codegen.

## Architecture
- `cmd/mcp-openapi-proxy/main.go` — CLI entry point, env var parsing, subcommands: serve/login/logout/status
- `pkg/spec/parser.go` — OpenAPI 3.x parser (kin-openapi), extracts Endpoint structs
- `pkg/server/server.go` — MCP server setup, stdio transport
- `pkg/server/generator.go` — Endpoint→MCP Tool conversion, handler generation, JSON Schema building
- `pkg/client/client.go` — HTTP client with bearer auth + extra headers
- `pkg/client/errors.go` — API error parsing
- `pkg/auth/provider.go` — TokenProvider interface + StaticTokenProvider
- `pkg/auth/oidc_provider.go` — OIDC token storage, auto-refresh (30s margin)
- `pkg/auth/login.go` — Browser-based OIDC Authorization Code + PKCE flow
- `pkg/auth/logout.go` — Token file removal
- `pkg/auth/status.go` — Auth state display

## Key Dependencies
- `github.com/getkin/kin-openapi` — OpenAPI 3.x parsing
- `github.com/modelcontextprotocol/go-sdk` — MCP server (go-sdk v0.4.0)
- `github.com/google/jsonschema-go` — JSON Schema for tool inputs

## Build & Run
- `go build -C . -o bin/mcp-openapi-proxy ./cmd/mcp-openapi-proxy`
- Config is 100% env vars: `MCP_SPEC`, `MCP_BASE_URL`, `MCP_TOOL_PREFIX`, `MCP_AUTH_TOKEN`, `MCP_OIDC_ISSUER`, `MCP_OIDC_CLIENT_ID`, `MCP_EXTRA_HEADERS`

## Conventions
- No tests yet — add tests when modifying existing logic
- Tool naming: `{prefix}_{method}_{sanitized_path}` (lowercase, special chars → `_`, collapsed)
- Auth priority: static token > OIDC from disk > no auth (warning)
- Tokens stored at `~/.mcp-openapi-proxy/{prefix}-tokens.json` with 0600 perms
- Request body nested under `"body"` key in tool input schema
- GET tools → readOnly annotation, DELETE tools → destructive annotation
- stdio transport only

## Gotchas
- `go-sdk` API may change — check import paths if build fails
- `jsonschema-go` uses `json.RawMessage` for Default field
- kin-openapi `SchemaRef.Value.Type` returns `*openapi3.Types` (slice), not a string — use `.Slice()`
- Version hardcoded as `0.1.0` in `server.go`
- No `.gitignore` — binary at `bin/` should be ignored
