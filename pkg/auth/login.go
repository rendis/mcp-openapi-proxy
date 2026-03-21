package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const loginTimeout = 5 * time.Minute

// LoginConfig holds all parameters needed for an OIDC PKCE login.
type LoginConfig struct {
	// APIBaseURL is the base URL used to fetch OIDC config from
	// {APIBaseURL}/api/v1/auth/config. Used only when AuthEndpoint,
	// TokenEndpoint, and ClientID are not provided directly.
	APIBaseURL string

	// Direct OIDC endpoints — when set, these skip the API config fetch.
	AuthEndpoint  string
	TokenEndpoint string
	ClientID      string

	// Scopes to request. Defaults to "openid profile email offline_access".
	Scopes string

	// TokenPrefix is used to namespace the on-disk token file:
	// ~/.mcp-openapi-proxy/{TokenPrefix}-tokens.json
	TokenPrefix string
}

// authConfigResponse mirrors the API's /auth/config response.
type authConfigResponse struct {
	DummyAuth     bool                 `json:"dummyAuth"`
	PanelProvider *panelProviderConfig `json:"panelProvider,omitempty"`
}

type panelProviderConfig struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorizationEndpoint"`
	TokenEndpoint         string `json:"tokenEndpoint"`
	ClientID              string `json:"clientId"`
	Scopes                string `json:"scopes"`
	// snake_case aliases
	AuthorizationEndpointAlias string `json:"authorization_endpoint"`
	TokenEndpointAlias         string `json:"token_endpoint"`
	ClientIDAlias              string `json:"client_id"`
}

func (p *panelProviderConfig) resolvedAuthorizationEndpoint() string {
	if p.AuthorizationEndpoint != "" {
		return p.AuthorizationEndpoint
	}
	return p.AuthorizationEndpointAlias
}

func (p *panelProviderConfig) resolvedTokenEndpoint() string {
	if p.TokenEndpoint != "" {
		return p.TokenEndpoint
	}
	return p.TokenEndpointAlias
}

func (p *panelProviderConfig) resolvedClientID() string {
	if p.ClientID != "" {
		return p.ClientID
	}
	return p.ClientIDAlias
}

// RunLogin performs browser-based OIDC login using Authorization Code + PKCE.
//
// It supports two modes:
//   - API discovery: set cfg.APIBaseURL to fetch OIDC endpoints from the API.
//   - Direct endpoints: set cfg.AuthEndpoint, cfg.TokenEndpoint, and cfg.ClientID.
func RunLogin(cfg LoginConfig) error {
	authEndpoint, tokenEndpoint, clientID, scopes, err := resolveOIDCConfig(cfg)
	if err != nil {
		return err
	}

	// PKCE
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return fmt.Errorf("generate PKCE: %w", err)
	}

	state, err := randomString(32)
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}

	// Start localhost callback server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			errCh <- fmt.Errorf("authorization error: %s -- %s", errMsg, desc)
			fmt.Fprintf(w, "<html><body><h2>Login failed</h2><p>%s: %s</p><p>You can close this tab.</p></body></html>", errMsg, desc)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}
		codeCh <- code
		fmt.Fprint(w, "<html><body><h2>Login successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>")
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()

	// Build authorization URL
	authURL := buildAuthURL(authEndpoint, clientID, redirectURI, scopes, state, challenge)

	fmt.Fprintf(os.Stderr, "\nOpening browser for login...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit this URL manually:\n\n  %s\n\n", authURL)

	_ = openBrowser(authURL)

	// Wait for callback or timeout
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		_ = srv.Shutdown(ctx)
		return err
	case <-ctx.Done():
		_ = srv.Shutdown(ctx)
		return fmt.Errorf("login timed out after %v", loginTimeout)
	}

	_ = srv.Shutdown(ctx)

	// Exchange code for tokens
	fmt.Fprintln(os.Stderr, "Exchanging authorization code for tokens...")

	tokens, err := exchangeCode(tokenEndpoint, clientID, code, redirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}

	stored := &StoredTokens{
		AccessToken:   tokens.AccessToken,
		RefreshToken:  tokens.RefreshToken,
		ExpiresAt:     time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		TokenEndpoint: tokenEndpoint,
		ClientID:      clientID,
	}

	prefix := cfg.TokenPrefix
	if prefix == "" {
		prefix = "default"
	}

	filePath := TokenFilePath(prefix)
	if err := SaveTokens(filePath, stored); err != nil {
		return fmt.Errorf("save tokens: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nLogin successful! Tokens saved to %s\n", filePath)
	if tokens.RefreshToken != "" {
		fmt.Fprintln(os.Stderr, "Refresh token obtained -- tokens will auto-refresh.")
	} else {
		fmt.Fprintln(os.Stderr, "Warning: no refresh token received. You may need to login again when the token expires.")
		fmt.Fprintln(os.Stderr, "Tip: ensure your OIDC provider returns refresh tokens (scope: offline_access).")
	}

	return nil
}

// resolveOIDCConfig determines OIDC endpoints either from direct config or by
// fetching them from the API.
func resolveOIDCConfig(cfg LoginConfig) (authEndpoint, tokenEndpoint, clientID, scopes string, err error) {
	// Mode B: direct endpoints provided
	if cfg.AuthEndpoint != "" && cfg.TokenEndpoint != "" && cfg.ClientID != "" {
		scopes = cfg.Scopes
		if scopes == "" {
			scopes = "openid profile email offline_access"
		}
		if !strings.Contains(scopes, "offline_access") {
			scopes += " offline_access"
		}
		return cfg.AuthEndpoint, cfg.TokenEndpoint, cfg.ClientID, scopes, nil
	}

	// Mode A: fetch from API
	if cfg.APIBaseURL == "" {
		return "", "", "", "", fmt.Errorf("either APIBaseURL or direct OIDC endpoints (AuthEndpoint, TokenEndpoint, ClientID) must be provided")
	}

	fmt.Fprintln(os.Stderr, "Fetching OIDC configuration from API...")

	apiCfg, err := fetchAuthConfig(cfg.APIBaseURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("fetch auth config: %w", err)
	}
	if apiCfg.DummyAuth {
		return "", "", "", "", fmt.Errorf("API is in dummy-auth mode -- login is not required; use a static token instead")
	}
	if apiCfg.PanelProvider == nil {
		return "", "", "", "", fmt.Errorf("API returned no OIDC provider configuration")
	}

	authEndpoint = apiCfg.PanelProvider.resolvedAuthorizationEndpoint()
	tokenEndpoint = apiCfg.PanelProvider.resolvedTokenEndpoint()
	clientID = apiCfg.PanelProvider.resolvedClientID()
	scopes = apiCfg.PanelProvider.Scopes
	if scopes == "" {
		scopes = "openid profile email offline_access"
	}
	if !strings.Contains(scopes, "offline_access") {
		scopes += " offline_access"
	}

	// Allow caller-provided scopes to override
	if cfg.Scopes != "" {
		scopes = cfg.Scopes
		if !strings.Contains(scopes, "offline_access") {
			scopes += " offline_access"
		}
	}

	if authEndpoint == "" || tokenEndpoint == "" || clientID == "" {
		return "", "", "", "", fmt.Errorf("incomplete OIDC config: authorization_endpoint=%q, token_endpoint=%q, client_id=%q",
			authEndpoint, tokenEndpoint, clientID)
	}

	return authEndpoint, tokenEndpoint, clientID, scopes, nil
}

func fetchAuthConfig(apiBaseURL string) (*authConfigResponse, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(strings.TrimRight(apiBaseURL, "/") + "/api/v1/auth/config")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /auth/config returned %d", resp.StatusCode)
	}
	var apiCfg authConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiCfg); err != nil {
		return nil, err
	}
	return &apiCfg, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func randomString(n int) (string, error) {
	b, err := randomBytes(n)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n], nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func buildAuthURL(endpoint, clientID, redirectURI, scopes, state, challenge string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		params := url.Values{
			"response_type":         {"code"},
			"client_id":             {clientID},
			"redirect_uri":          {redirectURI},
			"scope":                 {scopes},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}
		return endpoint + "?" + params.Encode()
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", scopes)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String()
}

func exchangeCode(tokenEndpoint, clientID, code, redirectURI, verifier string) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(tokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token in response")
	}
	return &tok, nil
}

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", rawURL).Start()
	case "linux":
		return exec.Command("xdg-open", rawURL).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
