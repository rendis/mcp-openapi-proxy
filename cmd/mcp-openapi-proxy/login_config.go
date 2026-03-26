package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

var loginEnvKeys = []string{
	"MCP_SPEC",
	"MCP_BASE_URL",
	"MCP_TOOL_PREFIX",
	"MCP_AUTH_PROFILE",
	"MCP_OIDC_ISSUER",
	"MCP_OIDC_CLIENT_ID",
	"MCP_OIDC_SCOPES",
}

type loginCLIArgs struct {
	MCPConfigPath   string
	CodexConfigPath string
	Server          string
}

type mcpConfigFile struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type codexConfigFile struct {
	MCPServers map[string]mcpServerConfig `toml:"mcp_servers"`
}

type mcpServerConfig struct {
	Command string            `json:"command" toml:"command"`
	Args    []string          `json:"args" toml:"args"`
	Env     map[string]string `json:"env" toml:"env"`
}

var (
	loginPromptInput   io.Reader = os.Stdin
	loginPromptOutput  io.Writer = os.Stderr
	loginIsInteractive           = defaultLoginIsInteractive
)

func parseLoginArgs(args []string) (loginCLIArgs, error) {
	var cfg loginCLIArgs
	var positional []string
	var codexServer string

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "":
			continue
		case arg == "--mcp-config":
			if i+1 >= len(args) {
				return loginCLIArgs{}, fmt.Errorf("--mcp-config requires a path")
			}
			i++
			cfg.MCPConfigPath = strings.TrimSpace(args[i])
			if cfg.MCPConfigPath == "" {
				return loginCLIArgs{}, fmt.Errorf("--mcp-config requires a path")
			}
		case strings.HasPrefix(arg, "--mcp-config="):
			cfg.MCPConfigPath = strings.TrimSpace(strings.TrimPrefix(arg, "--mcp-config="))
			if cfg.MCPConfigPath == "" {
				return loginCLIArgs{}, fmt.Errorf("--mcp-config requires a path")
			}
		case arg == "--codex-config":
			if i+1 >= len(args) {
				return loginCLIArgs{}, fmt.Errorf("--codex-config requires a path")
			}
			i++
			cfg.CodexConfigPath = strings.TrimSpace(args[i])
			if cfg.CodexConfigPath == "" {
				return loginCLIArgs{}, fmt.Errorf("--codex-config requires a path")
			}
		case strings.HasPrefix(arg, "--codex-config="):
			cfg.CodexConfigPath = strings.TrimSpace(strings.TrimPrefix(arg, "--codex-config="))
			if cfg.CodexConfigPath == "" {
				return loginCLIArgs{}, fmt.Errorf("--codex-config requires a path")
			}
		case arg == "--server":
			if i+1 >= len(args) {
				return loginCLIArgs{}, fmt.Errorf("--server requires a name")
			}
			i++
			cfg.Server = strings.TrimSpace(args[i])
			if cfg.Server == "" {
				return loginCLIArgs{}, fmt.Errorf("--server requires a name")
			}
		case strings.HasPrefix(arg, "--server="):
			cfg.Server = strings.TrimSpace(strings.TrimPrefix(arg, "--server="))
			if cfg.Server == "" {
				return loginCLIArgs{}, fmt.Errorf("--server requires a name")
			}
		case arg == "--codex-server":
			if i+1 >= len(args) {
				return loginCLIArgs{}, fmt.Errorf("--codex-server requires a name")
			}
			i++
			codexServer = strings.TrimSpace(args[i])
			if codexServer == "" {
				return loginCLIArgs{}, fmt.Errorf("--codex-server requires a name")
			}
		case strings.HasPrefix(arg, "--codex-server="):
			codexServer = strings.TrimSpace(strings.TrimPrefix(arg, "--codex-server="))
			if codexServer == "" {
				return loginCLIArgs{}, fmt.Errorf("--codex-server requires a name")
			}
		case arg == "-h" || arg == "--help":
			return loginCLIArgs{}, fmt.Errorf("usage: mcp-openapi-proxy login [<mcp_name>] [--mcp-config <path>] [--codex-config <path>] [--server <name>] [--codex-server <name>]")
		case strings.HasPrefix(arg, "-"):
			return loginCLIArgs{}, fmt.Errorf("unknown login flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) > 1 {
		return loginCLIArgs{}, fmt.Errorf("login accepts at most one positional MCP server name")
	}
	if cfg.MCPConfigPath != "" && cfg.CodexConfigPath != "" {
		return loginCLIArgs{}, fmt.Errorf("login accepts only one config source: --mcp-config or --codex-config")
	}
	if codexServer != "" && cfg.Server != "" {
		return loginCLIArgs{}, fmt.Errorf("login accepts only one explicit server selector: --server or --codex-server")
	}
	if len(positional) == 1 && codexServer != "" {
		return loginCLIArgs{}, fmt.Errorf("login positional MCP server name cannot be combined with --codex-server")
	}
	if len(positional) == 1 && cfg.CodexConfigPath != "" {
		return loginCLIArgs{}, fmt.Errorf("login positional MCP server name cannot be combined with --codex-config")
	}

	if len(positional) == 1 {
		if cfg.Server != "" && cfg.Server != positional[0] {
			return loginCLIArgs{}, fmt.Errorf("login server mismatch: positional %q does not match --server %q", positional[0], cfg.Server)
		}
		cfg.Server = positional[0]
		if cfg.MCPConfigPath == "" {
			cfg.MCPConfigPath = ".mcp.json"
		}
	}
	if codexServer != "" {
		cfg.Server = codexServer
		cfg.CodexConfigPath = defaultCodexConfigPath(os.Getenv)
	}

	if cfg.MCPConfigPath != "" {
		cfg.MCPConfigPath = filepath.Clean(cfg.MCPConfigPath)
	}
	if cfg.CodexConfigPath != "" {
		cfg.CodexConfigPath = filepath.Clean(cfg.CodexConfigPath)
	}

	return cfg, nil
}

func completeLoginArgs(args loginCLIArgs, processEnv map[string]string) (loginCLIArgs, error) {
	if args.Server != "" {
		return args, nil
	}

	if args.MCPConfigPath != "" {
		server, err := selectMCPServer(args.MCPConfigPath)
		if err != nil {
			return loginCLIArgs{}, err
		}
		args.Server = server
		return args, nil
	}
	if args.CodexConfigPath != "" {
		server, err := selectCodexServer(args.CodexConfigPath)
		if err != nil {
			return loginCLIArgs{}, err
		}
		args.Server = server
		return args, nil
	}

	if hasSufficientLoginEnv(processEnv) {
		return args, nil
	}

	if _, err := os.Stat(".mcp.json"); err == nil {
		args.MCPConfigPath = ".mcp.json"
		server, err := selectMCPServer(args.MCPConfigPath)
		if err != nil {
			return loginCLIArgs{}, err
		}
		args.Server = server
		return args, nil
	} else if !os.IsNotExist(err) {
		return loginCLIArgs{}, fmt.Errorf("stat .mcp.json: %w", err)
	}

	codexPath := defaultCodexConfigPath(os.Getenv)
	if _, err := os.Stat(codexPath); err == nil {
		args.CodexConfigPath = codexPath
		server, err := selectCodexServer(args.CodexConfigPath)
		if err != nil {
			return loginCLIArgs{}, err
		}
		args.Server = server
		return args, nil
	} else if !os.IsNotExist(err) {
		return loginCLIArgs{}, fmt.Errorf("stat %s: %w", codexPath, err)
	}

	return args, nil
}

func resolveLoginEnv(args loginCLIArgs) (map[string]string, error) {
	env := currentLoginEnv(os.Getenv)
	if args.MCPConfigPath == "" && args.CodexConfigPath == "" {
		return env, nil
	}

	var (
		fileEnv map[string]string
		err     error
	)
	switch {
	case args.MCPConfigPath != "":
		fileEnv, err = loadMCPServerEnv(args.MCPConfigPath, args.Server)
	case args.CodexConfigPath != "":
		fileEnv, err = loadCodexServerEnv(args.CodexConfigPath, args.Server)
	}
	if err != nil {
		return nil, err
	}
	return mergeLoginEnv(env, fileEnv), nil
}

func currentLoginEnv(getenv func(string) string) map[string]string {
	env := make(map[string]string, len(loginEnvKeys))
	for _, key := range loginEnvKeys {
		env[key] = strings.TrimSpace(getenv(key))
	}
	return env
}

func loadMCPConfig(path string) (mcpConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return mcpConfigFile{}, fmt.Errorf("read mcp config %s: %w", path, err)
	}

	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return mcpConfigFile{}, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	return cfg, nil
}

func loadCodexConfig(path string) (codexConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return codexConfigFile{}, fmt.Errorf("read codex config %s: %w", path, err)
	}

	var cfg codexConfigFile
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return codexConfigFile{}, fmt.Errorf("parse codex config %s: %w", path, err)
	}
	return cfg, nil
}

func loadMCPServerEnv(path, serverName string) (map[string]string, error) {
	cfg, err := loadMCPConfig(path)
	if err != nil {
		return nil, err
	}
	return loadServerEnvFromMap("mcp config", path, cfg.MCPServers, serverName)
}

func loadCodexServerEnv(path, serverName string) (map[string]string, error) {
	cfg, err := loadCodexConfig(path)
	if err != nil {
		return nil, err
	}
	return loadServerEnvFromMap("codex config", path, cfg.MCPServers, serverName)
}

func loadServerEnvFromMap(label, path string, servers map[string]mcpServerConfig, serverName string) (map[string]string, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("%s %s does not define any MCP servers", label, path)
	}

	server, ok := servers[serverName]
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found in %s", serverName, path)
	}
	if server.Env == nil {
		return nil, fmt.Errorf("mcp server %q in %s has no env block", serverName, path)
	}

	env := make(map[string]string, len(loginEnvKeys))
	for _, key := range loginEnvKeys {
		env[key] = strings.TrimSpace(server.Env[key])
	}
	return env, nil
}

func selectMCPServer(path string) (string, error) {
	cfg, err := loadMCPConfig(path)
	if err != nil {
		return "", err
	}
	return selectServerFromMap("mcp config", path, cfg.MCPServers, "rerun with --server <name> or login <mcp_name>")
}

func selectCodexServer(path string) (string, error) {
	cfg, err := loadCodexConfig(path)
	if err != nil {
		return "", err
	}
	return selectServerFromMap("codex config", path, cfg.MCPServers, "rerun with --server <name> or --codex-server <name>")
}

func selectServerFromMap(label, path string, servers map[string]mcpServerConfig, retryHint string) (string, error) {
	if len(servers) == 0 {
		return "", fmt.Errorf("%s %s does not define any MCP servers", label, path)
	}

	names := eligibleServerNames(servers)
	switch len(names) {
	case 0:
		return "", fmt.Errorf("%s %s does not define any direct mcp-openapi-proxy servers", label, path)
	case 1:
		return names[0], nil
	default:
		if !loginIsInteractive() {
			printEligibleMCPServers(loginPromptOutput, path, names)
			return "", fmt.Errorf("multiple mcp-openapi-proxy servers found in %s; %s", path, retryHint)
		}
		return promptForMCPServer(path, names, loginPromptInput, loginPromptOutput)
	}
}

func eligibleMCPServerNames(cfg mcpConfigFile) []string {
	return eligibleServerNames(cfg.MCPServers)
}

func eligibleServerNames(servers map[string]mcpServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name, server := range servers {
		if isDirectMCPProxyCommand(server.Command) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func normalizeCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	command = strings.ReplaceAll(command, `\`, "/")
	if idx := strings.LastIndex(command, "/"); idx >= 0 {
		command = command[idx+1:]
	}
	command = strings.ToLower(strings.TrimSpace(command))
	return strings.TrimSuffix(command, ".exe")
}

func isDirectMCPProxyCommand(command string) bool {
	return normalizeCommandName(command) == "mcp-openapi-proxy"
}

func defaultCodexConfigPath(getenv func(string) string) string {
	if codexHome := strings.TrimSpace(getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Clean(filepath.Join(codexHome, "config.toml"))
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Clean(filepath.Join("~", ".codex", "config.toml"))
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func printEligibleMCPServers(out io.Writer, path string, names []string) {
	fmt.Fprintf(out, "Available mcp-openapi-proxy servers in %s:\n", path)
	for i, name := range names {
		fmt.Fprintf(out, "  %d) %s\n", i+1, name)
	}
}

func promptForMCPServer(path string, names []string, in io.Reader, out io.Writer) (string, error) {
	printEligibleMCPServers(out, path, names)
	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "Select server: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", fmt.Errorf("read server selection: %w", err)
			}
			return "", fmt.Errorf("login aborted: no server selected")
		}

		selection := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(selection); err == nil && idx >= 1 && idx <= len(names) {
			return names[idx-1], nil
		}
		for _, name := range names {
			if selection == name {
				return name, nil
			}
		}
		fmt.Fprintln(out, "Invalid selection. Enter a number or exact server name.")
	}
}

func defaultLoginIsInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func mergeLoginEnv(processEnv, fileEnv map[string]string) map[string]string {
	merged := make(map[string]string, len(loginEnvKeys))
	for _, key := range loginEnvKeys {
		if value := strings.TrimSpace(processEnv[key]); value != "" {
			merged[key] = value
			continue
		}
		merged[key] = strings.TrimSpace(fileEnv[key])
	}
	return merged
}
