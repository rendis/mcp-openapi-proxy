<div align="center">

# mcp-openapi-proxy

**Turn any OpenAPI 3.x spec into a fully functional MCP server — automatically.**

Every REST API has an OpenAPI spec. Every AI agent speaks MCP.<br/>
This bridge connects the two with zero code — point it at a spec, get MCP tools.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-stdio-blueviolet)](https://modelcontextprotocol.io)

</div>

---

## The Problem

You have a REST API with 50+ endpoints and an OpenAPI spec that documents every one of them. You want an AI agent (Claude Code, Codex, Gemini CLI) to call your API through MCP. The standard approach: write one MCP tool definition per endpoint — input schemas, handlers, auth wiring — thousands of lines of boilerplate that breaks every time the API changes.

**mcp-openapi-proxy** eliminates that. One binary. One environment variable pointing to your spec. Every endpoint becomes an MCP tool at startup. No codegen, no generated files, no maintenance.

## How It Works

<p align="center">
  <img src="docs/assets/architecture/architecture.svg" alt="Architecture diagram showing OpenAPI spec flowing through parser, tool generator, and MCP server to connect AI agents with target APIs" width="780"/>
</p>

```mermaid
flowchart LR
    A["OpenAPI Spec<br/><small>YAML · JSON · URL</small>"] --> B["Spec Parser<br/><small>kin-openapi</small>"]
    B --> C["Endpoints[]"]
    C --> D["Tool Generator<br/><small>name + schema + handler</small>"]
    D --> E["MCP Server<br/><small>stdio</small>"]
    E <-->|"tool calls"| F["AI Agent<br/><small>Claude · Codex · Gemini</small>"]

    style A fill:#24283b,stroke:#bb9af7,color:#bb9af7
    style B fill:#24283b,stroke:#7dcfff,color:#7dcfff
    style C fill:#24283b,stroke:#e0af68,color:#e0af68
    style D fill:#24283b,stroke:#9ece6a,color:#9ece6a
    style E fill:#24283b,stroke:#bb9af7,color:#bb9af7
    style F fill:#1a1b26,stroke:#7aa2f7,color:#7aa2f7
```

1. The OpenAPI spec is loaded and parsed into a list of endpoints (method, path, parameters, request body)
2. Each endpoint becomes an MCP tool with a JSON Schema input derived from its parameters and body
3. A handler is generated for each tool that builds the HTTP request and calls your API with auth
4. The MCP server runs over stdio, ready to receive tool calls from any MCP client

## Features

- **OpenAPI 3.x** — parses paths, parameters, request bodies, and security schemes via [kin-openapi](https://github.com/getkin/kin-openapi)
- **Local and remote specs** — load from a file path or any `http://` / `https://` URL
- **One tool per endpoint** — auto-generated with full JSON Schema input validation
- **Tool annotations** — `GET` → read-only, `DELETE` → destructive
- **OIDC PKCE authentication** — browser-based login with automatic token refresh
- **Static token authentication** — simple bearer token for development and CI
- **Configurable tool prefix** — namespace tools to avoid collisions when running multiple proxies
- **Extra headers** — inject custom headers (workspace IDs, API versions) into every request
- **stdio transport** — compatible with Claude Code, OpenAI Codex, Gemini CLI, and any MCP client

### What it does NOT do

- **No codegen** — tools are created dynamically at startup, no build step
- **No API modification** — read-only proxy, never changes the spec or backend
- **No response validation** — forwards raw API responses to the agent
- **No OpenAPI 2.0** — only 3.x specs (convert older specs with [swagger2openapi](https://github.com/Mermade/oas-kit))
- **No SSE/WebSocket** — stdio transport only
- **No file uploads** — `multipart/form-data` and `application/x-www-form-urlencoded` endpoints are skipped
- **No cookie parameters** — cookie params in the spec are logged as a warning and ignored

## Quick Start

```bash
# Install
go install github.com/rendis/mcp-openapi-proxy/cmd/mcp-openapi-proxy@latest

# Run with a local spec and static token
MCP_SPEC=./openapi.yaml \
MCP_BASE_URL=https://api.example.com \
MCP_AUTH_TOKEN=your-token \
mcp-openapi-proxy
```

## Installation

```bash
go install github.com/rendis/mcp-openapi-proxy/cmd/mcp-openapi-proxy@latest
```

Or build from source:

```bash
git clone https://github.com/rendis/mcp-openapi-proxy.git
go build -C mcp-openapi-proxy -o bin/mcp-openapi-proxy ./cmd/mcp-openapi-proxy
```

## Configuration

All configuration is done through environment variables.

| Variable | Required | Default | Description |
|---|---|---|---|
| `MCP_SPEC` | Yes | — | Path or URL to an OpenAPI 3.x spec (YAML or JSON) |
| `MCP_BASE_URL` | Yes | — | Base URL of the target API |
| `MCP_TOOL_PREFIX` | No | `api` | Prefix for generated tool names |
| `MCP_AUTH_TOKEN` | No | — | Static bearer token (takes priority over OIDC) |
| `MCP_OIDC_ISSUER` | No | — | OIDC issuer URL (used with `login` command) |
| `MCP_OIDC_CLIENT_ID` | No | — | OIDC client ID (used with `login` command) |
| `MCP_EXTRA_HEADERS` | No | — | Comma-separated `key:value` pairs added to every request |

> [!IMPORTANT]
> **Authentication priority:** Static token (`MCP_AUTH_TOKEN`) → OIDC tokens from disk → No auth (warning to stderr).
>
> Trailing slashes on `MCP_BASE_URL` are stripped automatically.

## Commands

| Command | Description |
|---|---|
| `mcp-openapi-proxy` | Start the MCP server (default, same as `serve`) |
| `mcp-openapi-proxy serve` | Start the MCP server explicitly |
| `mcp-openapi-proxy login` | Browser-based OIDC Authorization Code + PKCE login |
| `mcp-openapi-proxy logout` | Remove stored tokens from disk |
| `mcp-openapi-proxy status` | Display current authentication state |

## Usage with AI Agents

<details>
<summary><strong>Claude Code</strong> — <code>.mcp.json</code></summary>

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

</details>

<details>
<summary><strong>OpenAI Codex</strong> — <code>.codex/config.toml</code></summary>

```toml
[mcp_servers.my-api]
command = "mcp-openapi-proxy"

[mcp_servers.my-api.env]
MCP_SPEC = "./openapi.yaml"
MCP_BASE_URL = "https://api.example.com"
MCP_TOOL_PREFIX = "myapi"
MCP_AUTH_TOKEN = "your-token"
```

</details>

<details>
<summary><strong>Gemini CLI</strong> — <code>~/.gemini/settings.json</code></summary>

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

</details>

## Tool Naming

Each endpoint becomes an MCP tool with the naming pattern:

```
{prefix}_{method}_{sanitized_path}
```

Path segments are lowercased. Special characters (`/`, `-`, `{`, `}`, `.`) are replaced with underscores. Consecutive underscores are collapsed.

| Method | Path | Prefix | Tool Name |
|---|---|---|---|
| GET | `/users` | `api` | `api_get_users` |
| POST | `/users` | `api` | `api_post_users` |
| GET | `/users/{id}` | `api` | `api_get_users_id` |
| PUT | `/users/{id}/roles` | `api` | `api_put_users_id_roles` |
| DELETE | `/admin/features/{key}` | `fe` | `fe_delete_admin_features_key` |
| GET | `/v1/health.check` | `svc` | `svc_get_v1_health_check` |

### Input Schema

Each tool receives a flat JSON object as input:

- **Path parameters** → top-level properties: `{"id": "abc123"}` (values are URL-encoded automatically)
- **Query parameters** → top-level properties: `{"page": 1, "limit": 20}` (arrays use repeated keys: `tags=a&tags=b`)
- **Header parameters** → top-level properties: `{"X-Request-Id": "req-001"}` (injected as HTTP headers)
- **Request body** → nested under `body`: `{"body": {"name": "new user"}}`

Required parameters from the OpenAPI spec are enforced in the tool's JSON Schema.

**Example** — `PUT /users/{id}` with query and body:

```json
{
  "id": "abc123",
  "include_roles": true,
  "body": {
    "name": "Jane Doe",
    "email": "jane@example.com"
  }
}
```

## Authentication

### Static Token (Development)

Set `MCP_AUTH_TOKEN` to any bearer token. The proxy sends it as `Authorization: Bearer <token>` on every request.

```bash
MCP_AUTH_TOKEN=dev-token mcp-openapi-proxy
```

### OIDC PKCE (Production)

For production APIs protected by any OIDC provider (Keycloak, Auth0, Okta, Google, etc.), use the built-in browser-based login flow. Endpoints are discovered automatically via `.well-known/openid-configuration`.

```mermaid
sequenceDiagram
    participant U as User
    participant P as mcp-openapi-proxy
    participant B as Browser
    participant IdP as OIDC Provider

    U->>P: mcp-openapi-proxy login
    P->>P: Generate PKCE verifier + challenge
    P->>P: Start localhost callback server
    P->>B: Open authorization URL
    B->>IdP: Authorization request + PKCE challenge
    IdP->>B: User authenticates
    B->>P: Redirect with authorization code
    P->>IdP: Exchange code + verifier for tokens
    IdP->>P: Access token + refresh token
    P->>P: Save to ~/.mcp-openapi-proxy/
    Note over P: Auto-refreshes when<br/>within 30s of expiry
```

**Login** — opens a browser window for Authorization Code + PKCE:

```bash
# Standard OIDC discovery (recommended — works with any OIDC provider)
# Discovers endpoints via {issuer}/.well-known/openid-configuration
MCP_OIDC_ISSUER=https://auth.example.com/realms/myrealm \
MCP_OIDC_CLIENT_ID=my-client \
mcp-openapi-proxy login

# Application-specific discovery (requires the API to expose /api/v1/auth/config)
MCP_BASE_URL=https://api.example.com mcp-openapi-proxy login
```

**Check status:**

```bash
mcp-openapi-proxy status
```

**Logout** — removes stored tokens:

```bash
mcp-openapi-proxy logout
```

Tokens are stored at `~/.mcp-openapi-proxy/mcp-openapi-proxy-tokens.json` with `0600` permissions. The file is written atomically (temp file + rename).

> [!TIP]
> If token refresh fails but the current access token hasn't expired yet, the existing token is used as a fallback — no error is surfaced to the agent.

## Architecture

<p align="center">
  <img src="docs/assets/flows/request-lifecycle.svg" alt="Request lifecycle showing tool call flowing through argument parsing, path resolution, query building, HTTP request, and response handling" width="780"/>
</p>

```
cmd/mcp-openapi-proxy/       Entry point, CLI subcommands, env var parsing
pkg/
  spec/                      OpenAPI 3.x parser (kin-openapi)
    parser.go                Loads spec from file or URL, extracts endpoints
  server/                    MCP server setup and tool generation
    server.go                Creates MCP server, runs stdio transport
    generator.go             Converts endpoints to MCP tools, builds handlers
  auth/                      Authentication providers
    provider.go              TokenProvider interface + StaticTokenProvider
    oidc_provider.go         OIDC token storage, loading, and transparent refresh
    discovery.go             OIDC .well-known/openid-configuration discovery
    login.go                 Browser-based OIDC Authorization Code + PKCE flow
    logout.go                Token file removal
    status.go                Print current auth state
  client/                    HTTP client for API calls
    client.go                Bearer auth, extra headers, response handling (JSON + raw text)
    errors.go                API error parsing
```

### Request Lifecycle

1. Agent calls a tool (e.g. `api_get_users_id` with `{"id": "abc123", "include_roles": true}`)
2. Handler substitutes path parameters: `/users/{id}` → `/users/abc123` (values are URL-encoded)
3. Query parameters are URL-encoded: `?include_roles=true` (arrays use repeated keys: `tags=a&tags=b`)
4. Request body (if present) is extracted from the `body` property and marshaled to JSON
5. HTTP client sends the request with `Authorization: Bearer <token>` and any extra headers
6. Response handling:
   - JSON responses → parsed and formatted as indented JSON
   - Non-JSON responses (`text/plain`, `text/html`, etc.) → returned as raw text
   - `204 No Content` or any `2xx` with empty body → `{"status": "ok"}`
   - `4xx/5xx` → `APIError` with status code and response body

## Examples

<details>
<summary><strong>Feature Flags API</strong></summary>

```json
{
  "mcpServers": {
    "feature-flags": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "https://api.flags.example.com/openapi.yaml",
        "MCP_BASE_URL": "https://api.flags.example.com",
        "MCP_TOOL_PREFIX": "ff",
        "MCP_EXTRA_HEADERS": "X-Workspace:my-workspace"
      }
    }
  }
}
```

</details>

<details>
<summary><strong>Mock Server (Local Development)</strong></summary>

```json
{
  "mcpServers": {
    "mock": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./api/openapi.yaml",
        "MCP_BASE_URL": "http://localhost:4010",
        "MCP_TOOL_PREFIX": "mock",
        "MCP_AUTH_TOKEN": "dev-token"
      }
    }
  }
}
```

</details>

<details>
<summary><strong>Multiple APIs Side by Side</strong></summary>

Use distinct prefixes to run multiple proxies without tool name collisions:

```json
{
  "mcpServers": {
    "users-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./specs/users.yaml",
        "MCP_BASE_URL": "https://users.example.com",
        "MCP_TOOL_PREFIX": "users",
        "MCP_AUTH_TOKEN": "token-a"
      }
    },
    "billing-api": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_SPEC": "./specs/billing.yaml",
        "MCP_BASE_URL": "https://billing.example.com",
        "MCP_TOOL_PREFIX": "billing",
        "MCP_AUTH_TOKEN": "token-b"
      }
    }
  }
}
```

</details>

## Agent Skill

This project includes an LLM agent skill that teaches agents how to install, configure, and use mcp-openapi-proxy. The skill lives at `skills/mcp-openapi-proxy/SKILL.md`.

Install globally so it's available in every project:

```bash
npx skills add --global https://github.com/rendis/mcp-openapi-proxy --skill mcp-openapi-proxy
```

Once installed, agents automatically know how to set up MCP servers from OpenAPI specs, configure authentication, and wire up multiple APIs. The skill covers installation, environment variable reference, MCP client setup (Claude Code, Codex, Gemini CLI), authentication, tool naming conventions, running multiple APIs, and common mistakes.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `MCP_SPEC environment variable is required` | Missing `MCP_SPEC` env var | Set `MCP_SPEC` to a path or URL pointing to your OpenAPI 3.x spec |
| `MCP_BASE_URL environment variable is required` | Missing `MCP_BASE_URL` env var | Set `MCP_BASE_URL` to your API's base URL |
| `load spec:` ... | Spec file not found or URL unreachable | Check the file path or URL; verify network/VPN if remote |
| `401 Unauthorized` on tool calls | No auth configured or token expired | Set `MCP_AUTH_TOKEN` or run `mcp-openapi-proxy login` for OIDC |
| Tool not found for an endpoint | Endpoint uses `multipart/form-data` or `x-www-form-urlencoded` | These endpoints are skipped; not supported |
| Parse error on spec | Spec is Swagger 2.0, not OpenAPI 3.x | Convert with [swagger2openapi](https://github.com/Mermade/oas-kit) first |
| `warning: no auth token configured` | No `MCP_AUTH_TOKEN` and no prior OIDC login | Expected if your API doesn't require auth; otherwise set a token |

## Tech Stack

| Component | Purpose |
|---|---|
| [Go 1.26+](https://go.dev) | Runtime |
| [kin-openapi](https://github.com/getkin/kin-openapi) | OpenAPI 3.x parsing |
| [go-sdk](https://github.com/modelcontextprotocol/go-sdk) | MCP server implementation |
| [jsonschema-go](https://github.com/google/jsonschema-go) | JSON Schema for tool inputs |

## Contributing

Contributions are welcome.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes
4. Push to the branch and open a Pull Request

> [!NOTE]
> Please include tests for any changes. Run `go test ./...` to verify.

## License

MIT
