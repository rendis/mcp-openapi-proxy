package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/server"
)

const tokenPrefix = "mcp-openapi-proxy"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve", "":
		err = runServe()
	case "login":
		err = runLogin()
	case "logout":
		err = runLogout()
	case "status":
		err = runStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		fmt.Fprintf(os.Stderr, "usage: mcp-openapi-proxy [serve|login|logout|status]\n")
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runServe() error {
	specSource := os.Getenv("MCP_SPEC")
	if specSource == "" {
		return fmt.Errorf("MCP_SPEC environment variable is required (path or URL to OpenAPI spec)")
	}

	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		return fmt.Errorf("MCP_BASE_URL environment variable is required (API base URL)")
	}

	toolPrefix := os.Getenv("MCP_TOOL_PREFIX")
	if toolPrefix == "" {
		toolPrefix = "api"
	}

	tp := resolveTokenProvider()
	extraHeaders := parseExtraHeaders(os.Getenv("MCP_EXTRA_HEADERS"))

	cfg := server.Config{
		SpecSource: specSource,
		BaseURL:    baseURL,
		ToolPrefix: toolPrefix,
	}

	return server.Run(cfg, tp, extraHeaders)
}

func runLogin() error {
	cfg := auth.LoginConfig{
		TokenPrefix: tokenPrefix,
	}

	issuer := os.Getenv("MCP_OIDC_ISSUER")
	clientID := os.Getenv("MCP_OIDC_CLIENT_ID")

	if issuer != "" && clientID != "" {
		// Discover endpoints via .well-known/openid-configuration (standard OIDC).
		authEP, tokenEP, err := auth.DiscoverOIDCEndpoints(issuer)
		if err != nil {
			return fmt.Errorf("OIDC discovery from %s: %w", issuer, err)
		}
		cfg.AuthEndpoint = authEP
		cfg.TokenEndpoint = tokenEP
		cfg.ClientID = clientID
	} else if baseURL := os.Getenv("MCP_BASE_URL"); baseURL != "" {
		// Fetch auth config from API.
		cfg.APIBaseURL = baseURL
	} else {
		return fmt.Errorf("login requires MCP_OIDC_ISSUER + MCP_OIDC_CLIENT_ID, or MCP_BASE_URL")
	}

	return auth.RunLogin(cfg)
}

func runLogout() error {
	return auth.RunLogout(tokenPrefix)
}

func runStatus() error {
	return auth.RunStatus(tokenPrefix)
}

// resolveTokenProvider resolves the token provider from environment variables.
// Priority:
//  1. MCP_AUTH_TOKEN → StaticTokenProvider
//  2. OIDC tokens from disk → OIDCTokenProvider
//  3. Fallback → empty token with warning
func resolveTokenProvider() auth.TokenProvider {
	// Static token (dev mode).
	if token := os.Getenv("MCP_AUTH_TOKEN"); token != "" {
		return auth.NewStaticTokenProvider(token)
	}

	// OIDC tokens from disk.
	filePath := auth.TokenFilePath(tokenPrefix)
	tp, err := auth.NewOIDCTokenProvider(filePath)
	if err == nil {
		return tp
	}

	// Fallback: warn and use empty token.
	fmt.Fprintf(os.Stderr, "warning: no auth token configured (set MCP_AUTH_TOKEN or run login)\n")
	return auth.NewStaticTokenProvider("")
}

// parseExtraHeaders parses a comma-separated list of key:value pairs.
func parseExtraHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	if raw == "" {
		return headers
	}

	pairs := strings.Split(raw, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				headers[key] = value
			}
		}
	}

	return headers
}
