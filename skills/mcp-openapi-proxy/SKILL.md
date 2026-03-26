---
name: mcp-openapi-proxy
description: Use when connecting a REST API to an MCP client, setting up mcp-openapi-proxy, exposing an OpenAPI-backed navigator/executor MCP server, or authenticating with OIDC/static tokens for API proxying.
---

# mcp-openapi-proxy

Turns any OpenAPI 3.x spec into a lightweight MCP stdio navigator/executor. The server always exposes 3 MCP tools and uses `toolName` identifiers to refer to individual endpoints.

For exhaustive reference, see the repository `README.md`. This skill is the short operational guide for installing the binary, wiring it into an MCP client, and using the current tool contract correctly.

Always discover the actual registered tools first; never assume semantic tool names.

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

Recommended setup flow:

1. Install `mcp-openapi-proxy`
2. Create `.mcp.json` from the generic example in [`.mcp.json.example`](../../.mcp.json.example)
3. Choose auth:
   - Static token: add `MCP_AUTH_TOKEN`
   - OIDC: add `MCP_OIDC_ISSUER` and `MCP_OIDC_CLIENT_ID`, then run one of:
     - `mcp-openapi-proxy login`
     - `mcp-openapi-proxy login <mcp_name>`
     - `mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json`
     - `mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json --server <mcp_name>`
4. Start the MCP client
5. Use `list_endpoints`, `describe_endpoint`, and `call_endpoint`

When `login` is invoked with `.mcp.json` support, it reads the selected server’s `env` from `.mcp.json`. Shell env still overrides `.mcp.json` when both are present.

If the server name is omitted, `login` discovers eligible servers from `.mcp.json`. It only considers direct `command` values that point to the `mcp-openapi-proxy` binary (plain name, full path, or `.exe` path). Wrapper commands such as `go`, `env`, `docker`, or shell scripts are ignored. If more than one eligible server exists, `login` prints the options and prompts you to choose one.

If the source spec comes from `swag init`, convert it before using the proxy. `swag init` emits Swagger 2.0, while `mcp-openapi-proxy` expects OpenAPI 3.x:

```bash
swag init -g cmd/api/main.go
swagger2openapi ./docs/swagger.json -o ./docs/openapi.json
MCP_SPEC=./docs/openapi.json mcp-openapi-proxy
```

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `MCP_SPEC` | Yes | — | Path or URL to OpenAPI 3.x spec (YAML/JSON) |
| `MCP_BASE_URL` | No | — | Explicit API base URL. If omitted, the proxy uses a single absolute OpenAPI server when available |
| `MCP_TOOL_PREFIX` | No | `api` | Prefix for the 3 MCP tools and endpoint `toolName` identifiers |
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

Generic example:

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./openapi.yaml",
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "myapi",
        "MCP_AUTH_PROFILE": "myapi"
      }
    }
  }
}
```

Static token variant:

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./openapi.yaml",
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "myapi",
        "MCP_AUTH_PROFILE": "myapi",
        "MCP_AUTH_TOKEN": "your-token"
      }
    }
  }
}
```

OIDC variant:

```json
{
  "mcpServers": {
    "my-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./openapi.yaml",
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "myapi",
        "MCP_AUTH_PROFILE": "myapi",
        "MCP_OIDC_ISSUER": "https://auth.example.com/realms/myrealm",
        "MCP_OIDC_CLIENT_ID": "my-client"
      }
    }
  }
}
```

OIDC login from `.mcp.json`:

```bash
mcp-openapi-proxy login
```

Or select the server explicitly:

```bash
mcp-openapi-proxy login my-api
```

Or with an explicit config path:

```bash
mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json
```

Or with an explicit config path plus explicit server:

```bash
mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json --server my-api
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

For Claude Code, the usual place is `.mcp.json -> mcpServers.<name>.env.MCP_AUTH_TOKEN`.

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

If your MCP client uses `.mcp.json`, these forms now read the selected server’s `env` automatically:

- `mcp-openapi-proxy login`
- `mcp-openapi-proxy login my-api`
- `mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json`
- `mcp-openapi-proxy login --mcp-config ./path/to/.mcp.json --server my-api`

Keep the same `MCP_AUTH_PROFILE`, `MCP_OIDC_ISSUER`, and `MCP_OIDC_CLIENT_ID` values there so the running server uses the token cache created by `login`.

If the server name is omitted, `login` only considers direct `mcp-openapi-proxy` commands from `.mcp.json`. Wrapper commands such as `go`, `env`, `docker`, or shell scripts are not eligible for login discovery. When multiple eligible entries exist, `login` lists them and prompts you to choose one.

If you also set those values in the shell when invoking `login`, the shell values win.

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

## Registered Tools and Endpoint IDs

The MCP server always exposes exactly these tools:

- `{prefix}_list_endpoints`
- `{prefix}_describe_endpoint`
- `{prefix}_call_endpoint`

Always inspect the registered tools first. Do not assume the server exposes one MCP tool per endpoint anymore.

Each OpenAPI endpoint still has a stable `toolName`:

```text
{prefix}_{method}_{sanitized_path}
```

That `toolName` is the identifier you pass into `describe_endpoint` and `call_endpoint`.

**Feature Flags API example** with `MCP_TOOL_PREFIX=fe`:

| Operation | Endpoint `toolName` |
|---|---|
| `GET /admin/features` | `fe_get_admin_features` |
| `POST /admin/features` | `fe_post_admin_features` |
| `GET /admin/features/{key}` | `fe_get_admin_features_key` |
| `PATCH /admin/features/{key}/toggle` | `fe_patch_admin_features_key_toggle` |
| `GET /admin/workspaces` | `fe_get_admin_workspaces` |

Names like `fe_list_features` or `fe_toggle_feature` are not part of the product contract.

## Input and Output Contract

Recommended agent flow:

1. Call `{prefix}_list_endpoints`
2. Pick a `toolName`
3. Call `{prefix}_describe_endpoint` if you need the exact contract
4. Call `{prefix}_call_endpoint`

### `list_endpoints`

Input:

```json
{
  "q": "feature",
  "path_prefix": "/admin",
  "method": "PATCH",
  "auth": "bearer",
  "limit": 25
}
```

Output:

```json
{
  "items": [
    {
      "toolName": "fe_patch_admin_features_key_toggle",
      "method": "PATCH",
      "path": "/admin/features/{key}/toggle",
      "description": "Toggle a feature flag",
      "requiredAuth": "bearer",
      "tags": ["flags", "admin"],
      "deprecated": false
    }
  ],
  "nextCursor": ""
}
```

### `describe_endpoint`

Input:

```json
{
  "toolName": "fe_patch_admin_features_key_toggle"
}
```

Output includes:

- `toolName`, `method`, `path`, `summary`, `description`, `deprecated`
- `requiredAuth` and full `securityRequirements`
- `parameters.path/query/headers/cookies`
- `requestBody` with media types, schema, examples, and encoding
- `responses` with status, description, headers, and body schemas
- `externalDocs`, `servers`, `baseURLHint`

### `call_endpoint`

Input:

```json
{
  "toolName": "fe_patch_admin_features_key_toggle",
  "path": {
    "key": "checkout"
  },
  "headers": {
    "X-Workspace": "acme"
  },
  "body": {
    "enabled": true
  }
}
```

`X-Workspace` is sent in `headers` per request or configured globally with `MCP_EXTRA_HEADERS`. Workspace selection is header-based, not a dedicated tool.

`call_endpoint` returns the HTTP envelope:

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
- Deprecated endpoints are listed by default and only excluded with `MCP_EXCLUDE_DEPRECATED=1`

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
- **Spec generated by `swag init`** → detect `swagger: "2.0"` or a generated `swagger.json`, convert it with `swagger2openapi`, then point `MCP_SPEC` at the resulting OpenAPI 3.x file
- **Missing per-scheme auth** → configure `MCP_AUTH_<SCHEME>_*`, `MCP_AUTH_TOKEN`, or run `mcp-openapi-proxy login`
- **Spec URL unreachable** → fails at startup; check network/VPN
- **Authenticated calls to non-loopback `http://`** → blocked unless `MCP_ALLOW_INSECURE_HTTP=1`
- **Treating endpoints as MCP tools** → endpoints are discovered as `toolName` values; only `list_endpoints`, `describe_endpoint`, and `call_endpoint` are registered
- **Calling `call_endpoint` without discovery** → list first, then describe if needed, then call with `toolName`

For exhaustive reference, examples, and troubleshooting, see the repository `README.md`.
