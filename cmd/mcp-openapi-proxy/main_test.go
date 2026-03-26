package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
)

func clearLoginEnv(t *testing.T) {
	t.Helper()
	for _, key := range loginEnvKeys {
		t.Setenv(key, "")
	}
}

func writeMCPConfig(t *testing.T, dir, raw string) string {
	t.Helper()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func writeCodexConfig(t *testing.T, path, raw string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	return path
}

func chdirTemp(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prev)
	})
}

func setLoginPromptIO(t *testing.T, interactive bool, input string, output *bytes.Buffer) {
	t.Helper()
	prevInput := loginPromptInput
	prevOutput := loginPromptOutput
	prevInteractive := loginIsInteractive
	loginPromptInput = strings.NewReader(input)
	loginPromptOutput = output
	loginIsInteractive = func() bool { return interactive }
	t.Cleanup(func() {
		loginPromptInput = prevInput
		loginPromptOutput = prevOutput
		loginIsInteractive = prevInteractive
	})
}

func TestParseExtraHeaders(t *testing.T) {
	got := parseExtraHeaders("X-A:1, X-B:two:parts, broken")
	if got["X-A"] != "1" {
		t.Fatalf("X-A = %q", got["X-A"])
	}
	if got["X-B"] != "two:parts" {
		t.Fatalf("X-B = %q", got["X-B"])
	}
	if len(got) != 2 {
		t.Fatalf("unexpected headers: %#v", got)
	}
}

func TestResolveAuthProfile(t *testing.T) {
	t.Setenv("MCP_AUTH_PROFILE", "prod")
	if got := resolveAuthProfile("api"); got != "prod" {
		t.Fatalf("profile = %q", got)
	}

	t.Setenv("MCP_AUTH_PROFILE", "")
	if got := resolveAuthProfile("api"); got != "api" {
		t.Fatalf("profile = %q", got)
	}
	if got := resolveAuthProfile(""); got != "default" {
		t.Fatalf("profile = %q", got)
	}
	if got := resolveAuthProfileValues("custom", "api"); got != "custom" {
		t.Fatalf("resolveAuthProfileValues(custom, api) = %q", got)
	}
}

func TestParseInt64Env(t *testing.T) {
	t.Setenv("MCP_MAX_BODY_BYTES", "2048")
	n, err := parseInt64Env("MCP_MAX_BODY_BYTES", 10)
	if err != nil || n != 2048 {
		t.Fatalf("parseInt64Env = (%d, %v)", n, err)
	}

	t.Setenv("MCP_MAX_BODY_BYTES", "0")
	if _, err := parseInt64Env("MCP_MAX_BODY_BYTES", 10); err == nil {
		t.Fatal("expected error for non-positive value")
	}
}

func TestResolveTokenProvider(t *testing.T) {
	t.Run("static token", func(t *testing.T) {
		t.Setenv("MCP_AUTH_TOKEN", "secret")
		t.Setenv("MCP_AUTH_PROFILE", "")
		tp := resolveTokenProvider()
		token, err := tp.Token(context.Background())
		if err != nil || token != "secret" {
			t.Fatalf("token = %q err=%v", token, err)
		}
	})

	t.Run("oidc token file", func(t *testing.T) {
		t.Setenv("MCP_AUTH_TOKEN", "")
		tmpHome := t.TempDir()
		t.Setenv("HOME", tmpHome)
		t.Setenv("MCP_AUTH_PROFILE", "staging")

		path := auth.TokenFilePath("staging")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(&auth.StoredTokens{
			AccessToken:   "oidc-token",
			RefreshToken:  "refresh",
			ExpiresAt:     time.Now().Add(time.Hour),
			TokenEndpoint: "https://example.com/token",
			ClientID:      "client",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}

		tp := resolveTokenProvider()
		token, err := tp.Token(context.Background())
		if err != nil || token != "oidc-token" {
			t.Fatalf("token = %q err=%v", token, err)
		}
	})
}

func TestRunServe_RequiresSpec(t *testing.T) {
	t.Setenv("MCP_SPEC", "")
	if err := runServe(); err == nil || !strings.Contains(err.Error(), "MCP_SPEC") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLogin_RequiresConfiguration(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	chdirTemp(t, dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), ".codex-home"))
	err := runLogin()
	if err == nil || !strings.Contains(err.Error(), "login requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseLoginArgs(t *testing.T) {
	t.Run("positional server uses cwd config", func(t *testing.T) {
		got, err := parseLoginArgs([]string{"feature-evaluator"})
		if err != nil {
			t.Fatalf("parseLoginArgs: %v", err)
		}
		if got.Server != "feature-evaluator" || got.MCPConfigPath != ".mcp.json" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("explicit config and server", func(t *testing.T) {
		got, err := parseLoginArgs([]string{"--mcp-config", "/tmp/.mcp.json", "--server", "feature-evaluator"})
		if err != nil {
			t.Fatalf("parseLoginArgs: %v", err)
		}
		if got.Server != "feature-evaluator" || got.MCPConfigPath != "/tmp/.mcp.json" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("config without server is allowed", func(t *testing.T) {
		got, err := parseLoginArgs([]string{"--mcp-config", "/tmp/.mcp.json"})
		if err != nil {
			t.Fatalf("parseLoginArgs: %v", err)
		}
		if got.Server != "" || got.MCPConfigPath != "/tmp/.mcp.json" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("codex config without server is allowed", func(t *testing.T) {
		got, err := parseLoginArgs([]string{"--codex-config", "/tmp/config.toml"})
		if err != nil {
			t.Fatalf("parseLoginArgs: %v", err)
		}
		if got.Server != "" || got.CodexConfigPath != "/tmp/config.toml" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("codex server uses default codex config", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "/tmp/codex-home")
		got, err := parseLoginArgs([]string{"--codex-server", "feature-evaluator"})
		if err != nil {
			t.Fatalf("parseLoginArgs: %v", err)
		}
		if got.Server != "feature-evaluator" || got.CodexConfigPath != "/tmp/codex-home/config.toml" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("positional and server mismatch", func(t *testing.T) {
		_, err := parseLoginArgs([]string{"feature-evaluator", "--server", "other"})
		if err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("positional and codex config conflict", func(t *testing.T) {
		_, err := parseLoginArgs([]string{"feature-evaluator", "--codex-config", "/tmp/config.toml"})
		if err == nil || !strings.Contains(err.Error(), "--codex-config") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestNormalizeCommandName(t *testing.T) {
	cases := map[string]string{
		"mcp-openapi-proxy":                "mcp-openapi-proxy",
		"/usr/local/bin/mcp-openapi-proxy": "mcp-openapi-proxy",
		`C:\tools\mcp-openapi-proxy.exe`:   "mcp-openapi-proxy",
		" docker ":                         "docker",
		"":                                 "",
		`C:\Program Files\Go\bin\go.exe`:   "go",
	}
	for input, want := range cases {
		if got := normalizeCommandName(input); got != want {
			t.Fatalf("normalizeCommandName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsDirectMCPProxyCommand(t *testing.T) {
	for _, command := range []string{
		"mcp-openapi-proxy",
		"/usr/local/bin/mcp-openapi-proxy",
		`C:\tools\mcp-openapi-proxy.exe`,
	} {
		if !isDirectMCPProxyCommand(command) {
			t.Fatalf("expected eligible command %q", command)
		}
	}

	for _, command := range []string{"", "go", "env", "docker", "bash"} {
		if isDirectMCPProxyCommand(command) {
			t.Fatalf("expected ineligible command %q", command)
		}
	}
}

func TestDefaultCodexConfigPath(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/codex-home")
	if got := defaultCodexConfigPath(os.Getenv); got != "/tmp/codex-home/config.toml" {
		t.Fatalf("defaultCodexConfigPath = %q", got)
	}
}

func TestLoadMCPServerEnv(t *testing.T) {
	dir := t.TempDir()

	t.Run("loads selected server env", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "feature-evaluator": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_BASE_URL": "https://api.example.com",
        "MCP_TOOL_PREFIX": "fe"
      }
    }
  }
}`)
		env, err := loadMCPServerEnv(path, "feature-evaluator")
		if err != nil {
			t.Fatalf("loadMCPServerEnv: %v", err)
		}
		if env["MCP_BASE_URL"] != "https://api.example.com" || env["MCP_TOOL_PREFIX"] != "fe" {
			t.Fatalf("env = %#v", env)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := loadMCPServerEnv(filepath.Join(dir, "missing.json"), "feature-evaluator")
		if err == nil || !strings.Contains(err.Error(), "read mcp config") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(dir, "invalid.json")
		if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadMCPServerEnv(path, "feature-evaluator")
		if err == nil || !strings.Contains(err.Error(), "parse mcp config") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing mcpServers", func(t *testing.T) {
		path := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadMCPServerEnv(path, "feature-evaluator")
		if err == nil || !strings.Contains(err.Error(), "does not define any MCP servers") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown server", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{"mcpServers":{"other":{"env":{"MCP_BASE_URL":"https://api.example.com"}}}}`)
		_, err := loadMCPServerEnv(path, "feature-evaluator")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing env block", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{"mcpServers":{"feature-evaluator":{"command":"mcp-openapi-proxy"}}}`)
		_, err := loadMCPServerEnv(path, "feature-evaluator")
		if err == nil || !strings.Contains(err.Error(), "has no env block") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestEligibleMCPServerNames(t *testing.T) {
	cfg := mcpConfigFile{
		MCPServers: map[string]mcpServerConfig{
			"alpha":   {Command: "mcp-openapi-proxy"},
			"beta":    {Command: "/usr/local/bin/mcp-openapi-proxy"},
			"windows": {Command: `C:\tools\mcp-openapi-proxy.exe`, Args: []string{"ignored"}},
			"go-run":  {Command: "go", Args: []string{"run", "./cmd/mcp-openapi-proxy"}},
			"docker":  {Command: "docker"},
			"shell":   {Command: "bash"},
		},
	}

	got := eligibleMCPServerNames(cfg)
	want := []string{"alpha", "beta", "windows"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("eligibleMCPServerNames = %#v, want %#v", got, want)
	}
}

func TestLoadCodexServerEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeCodexConfig(t, filepath.Join(dir, "config.toml"), `
[mcp_servers.feature-evaluator]
command = "/Users/rendis/go/bin/mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_BASE_URL = "https://api.example.com"
MCP_TOOL_PREFIX = "fe"
`)

	env, err := loadCodexServerEnv(path, "feature-evaluator")
	if err != nil {
		t.Fatalf("loadCodexServerEnv: %v", err)
	}
	if env["MCP_BASE_URL"] != "https://api.example.com" || env["MCP_TOOL_PREFIX"] != "fe" {
		t.Fatalf("env = %#v", env)
	}
}

func TestSelectCodexServer(t *testing.T) {
	dir := t.TempDir()

	t.Run("auto selects only eligible server", func(t *testing.T) {
		path := writeCodexConfig(t, filepath.Join(dir, "single.toml"), `
[mcp_servers.other]
command = "docker"

[mcp_servers.feature-evaluator]
command = "/Users/rendis/go/bin/mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_BASE_URL = "https://api.example.com"
`)
		if got, err := selectCodexServer(path); err != nil || got != "feature-evaluator" {
			t.Fatalf("selectCodexServer = (%q, %v)", got, err)
		}
	})

	t.Run("multiple eligible servers list options when non-interactive", func(t *testing.T) {
		path := writeCodexConfig(t, filepath.Join(dir, "multi.toml"), `
[mcp_servers.alpha]
command = "mcp-openapi-proxy"

[mcp_servers.alpha.env]
MCP_BASE_URL = "https://alpha.example.com"

[mcp_servers.beta]
command = "C:\\tools\\mcp-openapi-proxy.exe"

[mcp_servers.beta.env]
MCP_BASE_URL = "https://beta.example.com"
`)
		var output bytes.Buffer
		setLoginPromptIO(t, false, "", &output)

		_, err := selectCodexServer(path)
		if err == nil || !strings.Contains(err.Error(), "--codex-server") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(output.String(), "alpha") || !strings.Contains(output.String(), "beta") {
			t.Fatalf("output = %q", output.String())
		}
	})
}

func TestSelectMCPServer(t *testing.T) {
	dir := t.TempDir()

	t.Run("auto selects only eligible server", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "not-this-one": {
      "command": "docker",
      "env": {"MCP_BASE_URL": "https://ignore.example.com"}
    },
    "feature-evaluator": {
      "command": "/usr/local/bin/mcp-openapi-proxy",
      "env": {"MCP_BASE_URL": "https://api.example.com"}
    }
  }
}`)
		if got, err := selectMCPServer(path); err != nil || got != "feature-evaluator" {
			t.Fatalf("selectMCPServer = (%q, %v)", got, err)
		}
	})

	t.Run("multiple eligible servers prompts interactively", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://a.example.com"}},
    "beta": {"command": "C:\\tools\\mcp-openapi-proxy.exe", "env": {"MCP_BASE_URL": "https://b.example.com"}},
    "other": {"command": "go", "args": ["run", "./cmd/mcp-openapi-proxy"], "env": {"MCP_BASE_URL": "https://c.example.com"}}
  }
}`)
		var output bytes.Buffer
		setLoginPromptIO(t, true, "2\n", &output)

		got, err := selectMCPServer(path)
		if err != nil {
			t.Fatalf("selectMCPServer: %v", err)
		}
		if got != "beta" {
			t.Fatalf("server = %q", got)
		}
		if !strings.Contains(output.String(), "alpha") || !strings.Contains(output.String(), "beta") || strings.Contains(output.String(), "other") {
			t.Fatalf("prompt output = %q", output.String())
		}
	})

	t.Run("interactive prompt accepts exact name and reprompts", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://a.example.com"}},
    "beta": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://b.example.com"}}
  }
}`)
		var output bytes.Buffer
		setLoginPromptIO(t, true, "wrong\nbeta\n", &output)

		got, err := selectMCPServer(path)
		if err != nil {
			t.Fatalf("selectMCPServer: %v", err)
		}
		if got != "beta" {
			t.Fatalf("server = %q", got)
		}
		if !strings.Contains(output.String(), "Invalid selection") {
			t.Fatalf("prompt output = %q", output.String())
		}
	})

	t.Run("interactive prompt aborts on eof", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://a.example.com"}},
    "beta": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://b.example.com"}}
  }
}`)
		var output bytes.Buffer
		setLoginPromptIO(t, true, "", &output)

		_, err := selectMCPServer(path)
		if err == nil || !strings.Contains(err.Error(), "no server selected") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multiple eligible servers list options when non-interactive", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://a.example.com"}},
    "beta": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://b.example.com"}}
  }
}`)
		var output bytes.Buffer
		setLoginPromptIO(t, false, "", &output)

		_, err := selectMCPServer(path)
		if err == nil || !strings.Contains(err.Error(), "multiple mcp-openapi-proxy servers") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(output.String(), "alpha") || !strings.Contains(output.String(), "beta") {
			t.Fatalf("output = %q", output.String())
		}
	})

	t.Run("zero eligible servers errors", func(t *testing.T) {
		path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "docker": {"command": "docker", "env": {"MCP_BASE_URL": "https://a.example.com"}},
    "go-run": {"command": "go", "args": ["run", "./cmd/mcp-openapi-proxy"], "env": {"MCP_BASE_URL": "https://b.example.com"}}
  }
}`)
		_, err := selectMCPServer(path)
		if err == nil || !strings.Contains(err.Error(), "does not define any direct mcp-openapi-proxy servers") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResolveLoginEnv_PrefersProcessEnv(t *testing.T) {
	clearLoginEnv(t)
	t.Setenv("MCP_OIDC_CLIENT_ID", "env-client")
	t.Setenv("MCP_AUTH_PROFILE", "env-profile")

	dir := t.TempDir()
	path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "feature-evaluator": {
      "env": {
        "MCP_OIDC_ISSUER": "https://auth.example.com/realm",
        "MCP_OIDC_CLIENT_ID": "config-client",
        "MCP_TOOL_PREFIX": "fe",
        "MCP_AUTH_PROFILE": "config-profile",
        "MCP_SPEC": "./openapi.yaml"
      }
    }
  }
}`)

	env, err := resolveLoginEnv(loginCLIArgs{MCPConfigPath: path, Server: "feature-evaluator"})
	if err != nil {
		t.Fatalf("resolveLoginEnv: %v", err)
	}
	if env["MCP_OIDC_CLIENT_ID"] != "env-client" {
		t.Fatalf("MCP_OIDC_CLIENT_ID = %q", env["MCP_OIDC_CLIENT_ID"])
	}
	if env["MCP_AUTH_PROFILE"] != "env-profile" {
		t.Fatalf("MCP_AUTH_PROFILE = %q", env["MCP_AUTH_PROFILE"])
	}
	if env["MCP_OIDC_ISSUER"] != "https://auth.example.com/realm" || env["MCP_TOOL_PREFIX"] != "fe" || env["MCP_SPEC"] != "./openapi.yaml" {
		t.Fatalf("env = %#v", env)
	}
}

func TestResolveLoginEnv_PrefersProcessEnvOverCodexConfig(t *testing.T) {
	clearLoginEnv(t)
	t.Setenv("MCP_OIDC_CLIENT_ID", "env-client")
	t.Setenv("MCP_AUTH_PROFILE", "env-profile")

	path := writeCodexConfig(t, filepath.Join(t.TempDir(), "config.toml"), `
[mcp_servers.feature-evaluator]
command = "mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_OIDC_ISSUER = "https://auth.example.com/realm"
MCP_OIDC_CLIENT_ID = "config-client"
MCP_TOOL_PREFIX = "fe"
MCP_AUTH_PROFILE = "config-profile"
MCP_SPEC = "./openapi.yaml"
`)

	env, err := resolveLoginEnv(loginCLIArgs{CodexConfigPath: path, Server: "feature-evaluator"})
	if err != nil {
		t.Fatalf("resolveLoginEnv: %v", err)
	}
	if env["MCP_OIDC_CLIENT_ID"] != "env-client" || env["MCP_AUTH_PROFILE"] != "env-profile" {
		t.Fatalf("env = %#v", env)
	}
	if env["MCP_OIDC_ISSUER"] != "https://auth.example.com/realm" || env["MCP_TOOL_PREFIX"] != "fe" || env["MCP_SPEC"] != "./openapi.yaml" {
		t.Fatalf("env = %#v", env)
	}
}

func TestRunLogin_ReadsCWDMCPConfigFromPositionalServer(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	chdirTemp(t, dir)
	_ = writeMCPConfig(t, dir, `{
  "mcpServers": {
    "feature-evaluator": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_TOOL_PREFIX": "fe",
        "MCP_OIDC_ISSUER": "https://auth.example.com/realms/team",
        "MCP_OIDC_CLIENT_ID": "feature-evaluator",
        "MCP_OIDC_SCOPES": "openid profile"
      }
    }
  }
}`)

	var gotCfg auth.LoginConfig
	var gotIssuer string
	origRunLogin := runAuthLogin
	origDiscover := discoverOIDCEndpoints
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	discoverOIDCEndpoints = func(issuer string) (string, string, error) {
		gotIssuer = issuer
		return "https://auth.example.com/auth", "https://auth.example.com/token", nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
		discoverOIDCEndpoints = origDiscover
	})

	if err := runLogin("feature-evaluator"); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotIssuer != "https://auth.example.com/realms/team" {
		t.Fatalf("discover issuer = %q", gotIssuer)
	}
	if gotCfg.TokenPrefix != "fe" || gotCfg.ClientID != "feature-evaluator" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
	if gotCfg.AuthEndpoint != "https://auth.example.com/auth" || gotCfg.TokenEndpoint != "https://auth.example.com/token" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
	if gotCfg.Scopes != "openid profile" {
		t.Fatalf("cfg.Scopes = %q", gotCfg.Scopes)
	}
}

func TestRunLogin_ReadsCWDMCPConfigWhenEnvIsInsufficient(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	chdirTemp(t, dir)
	_ = writeMCPConfig(t, dir, `{
  "mcpServers": {
    "feature-evaluator": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_AUTH_PROFILE": "feature-evaluator-prod",
        "MCP_BASE_URL": "https://api.example.com"
      }
    }
  }
}`)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin(); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "feature-evaluator-prod" || gotCfg.APIBaseURL != "https://api.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
}

func TestRunLogin_FallsBackToCodexConfigWhenMCPConfigIsAbsent(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	chdirTemp(t, dir)
	codexHome := filepath.Join(t.TempDir(), ".codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	_ = writeCodexConfig(t, filepath.Join(codexHome, "config.toml"), `
[mcp_servers.feature-evaluator]
command = "/Users/rendis/go/bin/mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_AUTH_PROFILE = "fe-profile"
MCP_BASE_URL = "https://codex.example.com"
`)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin(); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "fe-profile" || gotCfg.APIBaseURL != "https://codex.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
}

func TestRunLogin_UsesEnvOnlyWhenAlreadyConfigured(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	chdirTemp(t, dir)
	_ = writeMCPConfig(t, dir, `{invalid json`)
	t.Setenv("MCP_BASE_URL", "https://env.example.com")
	t.Setenv("MCP_AUTH_PROFILE", "env-profile")

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin(); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.APIBaseURL != "https://env.example.com" || gotCfg.TokenPrefix != "env-profile" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
}

func TestRunLogin_CodexServerShortcut(t *testing.T) {
	clearLoginEnv(t)
	codexHome := filepath.Join(t.TempDir(), ".codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	_ = writeCodexConfig(t, filepath.Join(codexHome, "config.toml"), `
[mcp_servers.feature-evaluator]
command = "mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_AUTH_PROFILE = "feature-evaluator-prod"
MCP_BASE_URL = "https://api.example.com/"
`)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin("--codex-server", "feature-evaluator"); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "feature-evaluator-prod" || gotCfg.APIBaseURL != "https://api.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
}

func TestRunLogin_ExplicitMCPConfigAndServer(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "feature-evaluator": {
      "command": "mcp-openapi-proxy",
      "env": {
        "MCP_AUTH_PROFILE": "feature-evaluator-prod",
        "MCP_BASE_URL": "https://api.example.com/"
      }
    }
  }
}`)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin("--mcp-config", path, "--server", "feature-evaluator"); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "feature-evaluator-prod" {
		t.Fatalf("TokenPrefix = %q", gotCfg.TokenPrefix)
	}
	if gotCfg.APIBaseURL != "https://api.example.com" {
		t.Fatalf("APIBaseURL = %q", gotCfg.APIBaseURL)
	}
}

func TestRunLogin_ExplicitCodexConfigAndServer(t *testing.T) {
	clearLoginEnv(t)
	path := writeCodexConfig(t, filepath.Join(t.TempDir(), "config.toml"), `
[mcp_servers.feature-evaluator]
command = "/Users/rendis/go/bin/mcp-openapi-proxy"

[mcp_servers.feature-evaluator.env]
MCP_AUTH_PROFILE = "feature-evaluator-prod"
MCP_BASE_URL = "https://api.example.com/"
`)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin("--codex-config", path, "--server", "feature-evaluator"); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "feature-evaluator-prod" || gotCfg.APIBaseURL != "https://api.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
}

func TestRunLogin_ExplicitMCPConfigWithoutServerPrompts(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {
      "command": "mcp-openapi-proxy",
      "env": {"MCP_BASE_URL": "https://alpha.example.com"}
    },
    "beta": {
      "command": "/usr/local/bin/mcp-openapi-proxy",
      "env": {"MCP_AUTH_PROFILE": "beta-profile", "MCP_BASE_URL": "https://beta.example.com/"}
    }
  }
}`)

	var output bytes.Buffer
	setLoginPromptIO(t, true, "beta\n", &output)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin("--mcp-config", path); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "beta-profile" || gotCfg.APIBaseURL != "https://beta.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
	if !strings.Contains(output.String(), "Select server:") {
		t.Fatalf("prompt output = %q", output.String())
	}
}

func TestRunLogin_ExplicitMCPConfigWithoutServerErrorsWhenNonInteractive(t *testing.T) {
	clearLoginEnv(t)
	dir := t.TempDir()
	path := writeMCPConfig(t, dir, `{
  "mcpServers": {
    "alpha": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://alpha.example.com"}},
    "beta": {"command": "mcp-openapi-proxy", "env": {"MCP_BASE_URL": "https://beta.example.com"}}
  }
}`)

	var output bytes.Buffer
	setLoginPromptIO(t, false, "", &output)

	err := runLogin("--mcp-config", path)
	if err == nil || !strings.Contains(err.Error(), "multiple mcp-openapi-proxy servers") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), "alpha") || !strings.Contains(output.String(), "beta") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunLogin_ExplicitCodexConfigWithoutServerPrompts(t *testing.T) {
	clearLoginEnv(t)
	path := writeCodexConfig(t, filepath.Join(t.TempDir(), "config.toml"), `
[mcp_servers.alpha]
command = "mcp-openapi-proxy"

[mcp_servers.alpha.env]
MCP_BASE_URL = "https://alpha.example.com"

[mcp_servers.beta]
command = "/usr/local/bin/mcp-openapi-proxy"

[mcp_servers.beta.env]
MCP_AUTH_PROFILE = "beta-profile"
MCP_BASE_URL = "https://beta.example.com/"
`)

	var output bytes.Buffer
	setLoginPromptIO(t, true, "beta\n", &output)

	var gotCfg auth.LoginConfig
	origRunLogin := runAuthLogin
	runAuthLogin = func(cfg auth.LoginConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		runAuthLogin = origRunLogin
	})

	if err := runLogin("--codex-config", path); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if gotCfg.TokenPrefix != "beta-profile" || gotCfg.APIBaseURL != "https://beta.example.com" {
		t.Fatalf("cfg = %#v", gotCfg)
	}
	if !strings.Contains(output.String(), "Select server:") {
		t.Fatalf("prompt output = %q", output.String())
	}
}

func TestRunLogin_PositionalAndServerConflict(t *testing.T) {
	err := runLogin("feature-evaluator", "--server", "other")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}
