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
	MCPConfigPath string
	Server        string
}

type mcpConfigFile struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers"`
}

type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

var (
	loginPromptInput   io.Reader = os.Stdin
	loginPromptOutput  io.Writer = os.Stderr
	loginIsInteractive           = defaultLoginIsInteractive
)

func parseLoginArgs(args []string) (loginCLIArgs, error) {
	var cfg loginCLIArgs
	var positional []string

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
		case arg == "-h" || arg == "--help":
			return loginCLIArgs{}, fmt.Errorf("usage: mcp-openapi-proxy login [<mcp_name>] [--mcp-config <path>] [--server <mcp_name>]")
		case strings.HasPrefix(arg, "-"):
			return loginCLIArgs{}, fmt.Errorf("unknown login flag: %s", arg)
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) > 1 {
		return loginCLIArgs{}, fmt.Errorf("login accepts at most one positional MCP server name")
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

	if cfg.MCPConfigPath != "" {
		cfg.MCPConfigPath = filepath.Clean(cfg.MCPConfigPath)
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

	if hasSufficientLoginEnv(processEnv) {
		return args, nil
	}

	const defaultConfigPath = ".mcp.json"
	if _, err := os.Stat(defaultConfigPath); err != nil {
		if os.IsNotExist(err) {
			return args, nil
		}
		return loginCLIArgs{}, fmt.Errorf("stat %s: %w", defaultConfigPath, err)
	}

	args.MCPConfigPath = defaultConfigPath
	server, err := selectMCPServer(args.MCPConfigPath)
	if err != nil {
		return loginCLIArgs{}, err
	}
	args.Server = server
	return args, nil
}

func resolveLoginEnv(args loginCLIArgs) (map[string]string, error) {
	env := currentLoginEnv(os.Getenv)
	if args.MCPConfigPath == "" {
		return env, nil
	}

	fileEnv, err := loadMCPServerEnv(args.MCPConfigPath, args.Server)
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

func loadMCPServerEnv(path, serverName string) (map[string]string, error) {
	cfg, err := loadMCPConfig(path)
	if err != nil {
		return nil, err
	}
	if len(cfg.MCPServers) == 0 {
		return nil, fmt.Errorf("mcp config %s does not define mcpServers", path)
	}

	server, ok := cfg.MCPServers[serverName]
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
	if len(cfg.MCPServers) == 0 {
		return "", fmt.Errorf("mcp config %s does not define mcpServers", path)
	}

	names := eligibleMCPServerNames(cfg)
	switch len(names) {
	case 0:
		return "", fmt.Errorf("mcp config %s does not define any direct mcp-openapi-proxy servers", path)
	case 1:
		return names[0], nil
	default:
		if !loginIsInteractive() {
			printEligibleMCPServers(loginPromptOutput, path, names)
			return "", fmt.Errorf("multiple mcp-openapi-proxy servers found in %s; rerun with --server <name> or login <mcp_name>", path)
		}
		return promptForMCPServer(path, names, loginPromptInput, loginPromptOutput)
	}
}

func eligibleMCPServerNames(cfg mcpConfigFile) []string {
	names := make([]string, 0, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
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
