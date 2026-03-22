---
name: mcp-openapi-proxy
description: Use when connecting a REST API to an MCP client, setting up mcp-openapi-proxy, configuring OpenAPI specs as MCP tools, or authenticating with OIDC/static tokens for API proxying.
---

# mcp-openapi-proxy

Turns any OpenAPI 3.x spec into a fully functional MCP stdio server. One tool per endpoint, dynamic at startup, no codegen.

For exhaustive reference, see the repository `README.md`. This skill is the short operational guide for installing the binary, wiring it into an MCP client, and using the current tool contract correctly.

## Install

```bash
go install github.com/rendis/mcp-openapi-proxy/cmd/mcp-openapi-proxy@latest
```

## Quick Start

```bash
MCP_SPEC=./openapi.yaml \
MCP_BASE_URL=https://api.example.com \
MCP_AUTH_TOKEN=your-token \
mcp-openapi-proxy
```

`MCP_BASE_URL` is optional when the OpenAPI spec resolves to a single absolute `server`.

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `MCP_SPEC` | Yes | — | Path or URL to OpenAPI 3.x spec (YAML/JSON) |
| `MCP_BASE_URL` | No | — | Explicit API base URL. If omitted, the proxy uses a single absolute OpenAPI server when available |
| `MCP_TOOL_PREFIX` | No | `api` | Prefix for tool names |
| `MCP_AUTH_PROFILE` | No | `MCP_TOOL_PREFIX` or `default` | Namespace for stored OIDC tokens |
| `MCP_AUTH_TOKEN` | No | — | Global bearer token fallback |
| `MCP_OIDC_ISSUER` | No | — | OIDC issuer URL |
| `MCP_OIDC_CLIENT_ID` | No | — | OIDC client ID |
| `MCP_OIDC_SCOPES` | No | Spec scopes or `openid profile email offline_access` | Override OIDC login scopes |
| `MCP_EXTRA_HEADERS` | No | — | Comma-separated `key:value` pairs for every request |
| `MCP_MAX_BODY_BYTES` | No | `10485760` | Maximum response body size to buffer and return |
| `MCP_ALLOW_INSECURE_HTTP` | No | `0` | Allow sending credentials over non-loopback `http://` URLs |
| `MCP_EXCLUDE_DEPRECATED` | No | `0` | Skip deprecated endpoints when generating tools |

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

Credential resolution order is:

1. `MCP_AUTH_<SCHEME>_*`
2. global `MCP_AUTH_TOKEN`
3. OIDC token cache for `MCP_AUTH_PROFILE`

### Static Token (dev/CI)

Set `MCP_AUTH_TOKEN` to provide a global bearer fallback for bearer-compatible schemes.

### Per-Scheme Credentials

Use the OpenAPI security scheme name, uppercased and sanitized by replacing `.`, `-`, `/`, and spaces with underscores.

- `http bearer`, `oauth2`, `openIdConnect` → `MCP_AUTH_<SCHEME>_TOKEN`
- `http basic` → `MCP_AUTH_<SCHEME>_USERNAME` and `MCP_AUTH_<SCHEME>_PASSWORD`
- `apiKey` → `MCP_AUTH_<SCHEME>_KEY`

Example:

```bash
MCP_AUTH_PARTNER_AUTH_V2_TOKEN=secret-token
MCP_AUTH_ADMIN_BASIC_USERNAME=alice
MCP_AUTH_ADMIN_BASIC_PASSWORD=s3cr3t
MCP_AUTH_X_API_KEY_KEY=dev-key
```

### OIDC PKCE (production)

**Standard OIDC discovery (recommended):**

```bash
MCP_OIDC_ISSUER=https://auth.example.com/realms/myrealm \
MCP_OIDC_CLIENT_ID=my-client \
mcp-openapi-proxy login
```

**Application-specific discovery:**

```bash
MCP_BASE_URL=https://api.example.com mcp-openapi-proxy login
```

Commands:

- `mcp-openapi-proxy login`
- `mcp-openapi-proxy status`
- `mcp-openapi-proxy logout`

Scopes come from `MCP_OIDC_SCOPES` when set. Otherwise, if `MCP_SPEC` is available, the proxy unions scopes from the OpenAPI security requirements. If neither source is available, it falls back to `openid profile email offline_access`.

Tokens are stored at `~/.mcp-openapi-proxy/<profile>-tokens.json`, where `<profile>` comes from `MCP_AUTH_PROFILE` or falls back to the tool prefix / `default`.

## Tool Naming

Pattern: `{prefix}_{method}_{sanitized_path}` — lowercase, special chars → `_`, collapsed.

| Method | Path | Prefix | Tool Name |
|---|---|---|---|
| GET | `/users` | `api` | `api_get_users` |
| POST | `/users/{id}/roles` | `api` | `api_post_users_id_roles` |
| DELETE | `/admin/features/{key}` | `fe` | `fe_delete_admin_features_key` |

## Input and Output Contract

Each tool input is grouped by HTTP location:

```json
{
  "path": {},
  "query": {},
  "headers": {},
  "cookies": {},
  "body": {}
}
```

Only the sections used by the operation are present. Each section has `additionalProperties: false`.

Example:

```json
{
  "path": {
    "id": "abc123"
  },
  "query": {
    "include_roles": true
  },
  "body": {
    "name": "Jane Doe"
  }
}
```

If an operation supports multiple request body media types, `body` becomes:

```json
{
  "body": {
    "content_type": "application/json",
    "value": {
      "name": "Jane Doe"
    }
  }
}
```

Every tool returns the same envelope in MCP `StructuredContent` and pretty JSON text:

```json
{
  "status": 200,
  "content_type": "application/json",
  "headers": {
    "X-Trace": ["abc123"]
  },
  "body": {
    "id": "item-1"
  }
}
```

- API `4xx/5xx` responses preserve the real HTTP response and set `IsError=true`
- Proxy/runtime failures return `status: 0` plus `proxy_error`
- Binary payloads are represented as base64 wrappers
- Deprecated endpoints are registered by default and only excluded with `MCP_EXCLUDE_DEPRECATED=1`

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

- **Missing `MCP_SPEC`** → startup fails immediately
- **No usable base URL** → set `MCP_BASE_URL` explicitly or declare a single absolute OpenAPI `server`
- **Swagger 2.0 spec** → not supported; convert with `swagger2openapi` first
- **Missing per-scheme auth** → configure `MCP_AUTH_<SCHEME>_*`, `MCP_AUTH_TOKEN`, or run `mcp-openapi-proxy login`
- **Spec URL unreachable** → fails at startup; check network/VPN
- **Authenticated calls to non-loopback `http://`** → blocked unless `MCP_ALLOW_INSECURE_HTTP=1`
- **Using the old flat input contract** → current tools expect `path/query/headers/cookies/body`, not top-level params

For exhaustive reference, examples, and troubleshooting, see the repository `README.md`.
