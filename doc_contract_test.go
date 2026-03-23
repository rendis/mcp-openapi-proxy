package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDocumentation_StatesNavigatorContract(t *testing.T) {
	readme := readDoc(t, "README.md")
	agents := readDoc(t, "AGENTS.md")
	skill := readDoc(t, "skills/mcp-openapi-proxy/SKILL.md")

	for _, doc := range []struct {
		name string
		body string
	}{
		{name: "README", body: readme},
		{name: "AGENTS", body: agents},
		{name: "SKILL", body: skill},
	} {
		t.Run(doc.name, func(t *testing.T) {
			requiredFragments := []string{
				"{prefix}_{method}_{sanitized_path}",
				"list_endpoints",
				"describe_endpoint",
				"call_endpoint",
				"toolName",
				"3 MCP tools",
			}
			for _, fragment := range requiredFragments {
				if !strings.Contains(doc.body, fragment) {
					t.Fatalf("%s missing fragment %q", doc.name, fragment)
				}
			}
		})
	}
}

func TestDocumentation_ExplainsEndpointDiscoveryAndCallContract(t *testing.T) {
	readme := readDoc(t, "README.md")
	skill := readDoc(t, "skills/mcp-openapi-proxy/SKILL.md")

	for _, doc := range []struct {
		name string
		body string
	}{
		{name: "README", body: readme},
		{name: "SKILL", body: skill},
	} {
		t.Run(doc.name, func(t *testing.T) {
			requiredFragments := []string{
				`"toolName": "fe_patch_admin_features_key_toggle"`,
				`"requiredAuth": "bearer"`,
				`"path_prefix": "/admin"`,
				`"X-Workspace": "acme"`,
				"MCP_EXTRA_HEADERS",
			}
			for _, fragment := range requiredFragments {
				if !strings.Contains(doc.body, fragment) {
					t.Fatalf("%s missing fragment %q", doc.name, fragment)
				}
			}

			forbiddenFragments := []string{
				"One tool per endpoint",
				"Each endpoint becomes an MCP tool",
				"configuring OpenAPI specs as MCP tools",
			}
			for _, fragment := range forbiddenFragments {
				if strings.Contains(doc.body, fragment) {
					t.Fatalf("%s unexpectedly contains forbidden fragment %q", doc.name, fragment)
				}
			}
		})
	}
}

func TestDocumentation_StatesSwagger2ConversionRequirement(t *testing.T) {
	readme := readDoc(t, "README.md")
	agents := readDoc(t, "AGENTS.md")
	skill := readDoc(t, "skills/mcp-openapi-proxy/SKILL.md")

	for _, doc := range []struct {
		name string
		body string
	}{
		{name: "README", body: readme},
		{name: "AGENTS", body: agents},
		{name: "SKILL", body: skill},
	} {
		t.Run(doc.name, func(t *testing.T) {
			requiredFragments := []string{
				"swag init",
				"Swagger 2.0",
				"OpenAPI 3.x",
				"swagger2openapi",
			}
			for _, fragment := range requiredFragments {
				if !strings.Contains(doc.body, fragment) {
					t.Fatalf("%s missing fragment %q", doc.name, fragment)
				}
			}
		})
	}
}

func TestExampleMCPJSON_ExistsAndIsGeneric(t *testing.T) {
	data, err := os.ReadFile(".mcp.json.example")
	if err != nil {
		t.Fatalf("ReadFile(.mcp.json.example): %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal(.mcp.json.example): %v", err)
	}

	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("mcpServers = %#v", parsed["mcpServers"])
	}

	server, ok := servers["my-api"].(map[string]any)
	if !ok {
		t.Fatalf("my-api server = %#v", servers["my-api"])
	}
	if server["command"] != "mcp-openapi-proxy" {
		t.Fatalf("command = %#v", server["command"])
	}

	env, ok := server["env"].(map[string]any)
	if !ok {
		t.Fatalf("env = %#v", server["env"])
	}
	for _, key := range []string{"MCP_SPEC", "MCP_BASE_URL", "MCP_TOOL_PREFIX", "MCP_AUTH_PROFILE"} {
		if _, ok := env[key]; !ok {
			t.Fatalf(".mcp.json.example missing %q in env", key)
		}
	}
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
