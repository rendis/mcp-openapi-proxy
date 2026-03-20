---
name: mcp-openapi-proxy
description: Use when connecting a REST API to an MCP client, setting up mcp-openapi-proxy, configuring OpenAPI specs as MCP tools, or authenticating with OIDC/static tokens for API proxying.
---

# mcp-openapi-proxy

Turns any OpenAPI 3.x spec into a fully functional MCP stdio server. One tool per endpoint, dynamic at startup, no codegen.

## Install

```bash
go install github.com/rendis/mcp-openapi-proxy/cmd/mcp-openapi-proxy@latest
```

## Quick Start

```bash
MCP_SPEC=./openapi.yaml MCP_BASE_URL=https://api.example.com MCP_AUTH_TOKEN=your-token mcp-openapi-proxy
```

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `MCP_SPEC` | Yes | — | Path or URL to OpenAPI 3.x spec (YAML/JSON) |
| `MCP_BASE_URL` | Yes | — | Target API base URL |
| `MCP_TOOL_PREFIX` | No | `api` | Prefix for tool names |
| `MCP_AUTH_TOKEN` | No | — | Static bearer token (priority over OIDC) |
| `MCP_OIDC_ISSUER` | No | — | OIDC issuer URL |
| `MCP_OIDC_CLIENT_ID` | No | — | OIDC client ID |
| `MCP_EXTRA_HEADERS` | No | — | Comma-separated `key:value` pairs for every request |

## MCP Client Setup

### Claude Code (`.mcp.json`)

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./openapi.yaml",
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "myapi",
        "MCP_AUTH_TOKEN": "your-token"
      }
    }
  }
}
```

### OpenAI Codex (`.codex/config.toml`)

```toml
[mcp_servers.my-api]
command = "mcp-openapi-proxy"

[mcp_servers.my-api.env]
MCP_SPEC = "./openapi.yaml"
MCP_BASE_URL = "https://api.example.com"
MCP_TOOL_PREFIX = "myapi"
MCP_AUTH_TOKEN = "your-token"
```

### Gemini CLI (`~/.gemini/settings.json`)

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./openapi.yaml",
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "myapi",
        "MCP_AUTH_TOKEN": "your-token"
      }
    }
  }
}
```

## Authentication

**Priority:** static token → OIDC from disk → no auth (warning).

### Static Token (dev/CI)

Set `MCP_AUTH_TOKEN`. Sent as `Authorization: Bearer <token>` on every request.

### OIDC PKCE (production)

**Login** — opens browser for Authorization Code + PKCE:

```bash
# Direct OIDC endpoints
MCP_OIDC_ISSUER=https://auth.example.com/realms/myrealm \
MCP_OIDC_CLIENT_ID=my-client \
mcp-openapi-proxy login

# Or auto-discover from API's /api/v1/auth/config
MCP_BASE_URL=https://api.example.com mcp-openapi-proxy login
```

**Status:** `mcp-openapi-proxy status`

**Logout:** `mcp-openapi-proxy logout`

Tokens stored at `~/.mcp-openapi-proxy/mcp-openapi-proxy-tokens.json` (0600). Auto-refreshes when within 30s of expiry.

## Tool Naming

Pattern: `{prefix}_{method}_{sanitized_path}` — lowercase, special chars → `_`, collapsed.

| Method | Path | Prefix | Tool Name |
|---|---|---|---|
| GET | `/users` | `api` | `api_get_users` |
| POST | `/users/{id}/roles` | `api` | `api_post_users_id_roles` |
| DELETE | `/admin/features/{key}` | `fe` | `fe_delete_admin_features_key` |

## Input Schema

- **Path params** → top-level: `{"id": "abc123"}`
- **Query params** → top-level: `{"page": 1, "limit": 20}`
- **Request body** → nested under `body`: `{"body": {"name": "new user"}}`

## Multiple APIs

Use distinct prefixes to run side-by-side:

```json
{
  "mcpServers": {
    "users-api": { "command": "mcp-openapi-proxy", "env": { "MCP_TOOL_PREFIX": "users", "..." : "..." } },
    "billing-api": { "command": "mcp-openapi-proxy", "env": { "MCP_TOOL_PREFIX": "billing", "..." : "..." } }
  }
}
```

## Common Mistakes

- **Missing `MCP_SPEC` or `MCP_BASE_URL`** → fatal error at startup
- **Swagger 2.0 spec** → not supported; convert with `swagger2openapi` first
- **No auth configured** → runs but prints warning; API calls may 401
- **Spec URL unreachable** → fails at startup; check network/VPN
- **multipart/form-data endpoints** → silently skipped, not generated as tools
