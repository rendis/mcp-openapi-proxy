package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

// Config for the MCP server.
type Config struct {
	SpecSource string // path or URL to OpenAPI spec
	BaseURL    string // API base URL
	ToolPrefix string // prefix for tool names
}

// Run loads the spec, generates tools, and starts the MCP stdio server.
func Run(cfg Config, tp auth.TokenProvider, extraHeaders map[string]string) error {
	// Load OpenAPI spec.
	endpoints, _, err := spec.LoadSpec(cfg.SpecSource)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	// Create HTTP client.
	c := client.New(cfg.BaseURL, tp, extraHeaders)

	// Create MCP server.
	srv := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcp-openapi-proxy",
			Version: "0.1.0",
		},
		nil,
	)

	// Generate and register tools.
	GenerateTools(srv, endpoints, c, cfg.ToolPrefix)

	fmt.Fprintf(os.Stderr, "mcp-openapi-proxy: registered %d tools from %s\n", len(endpoints), cfg.SpecSource)

	// Run the stdio transport.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Printf("MCP server error: %v", err)
		return err
	}

	return nil
}
