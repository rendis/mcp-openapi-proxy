package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	tokenFileName  = "tokens.json"
	configDirName  = ".mcp-openapi-proxy"
	refreshMargin  = 30 * time.Second
	refreshTimeout = 15 * time.Second
	tokenFilePerm  = 0600
	tokenDirPerm   = 0700
)

// StoredTokens is the on-disk token cache.
type StoredTokens struct {
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
	TokenEndpoint string    `json:"token_endpoint"`
	ClientID      string    `json:"client_id"`
}

// OIDCTokenProvider reads tokens from disk and transparently refreshes them
// when expired.
type OIDCTokenProvider struct {
	mu       sync.Mutex
	tokens   *StoredTokens
	filePath string
	client   *http.Client
}

// TokenFilePath returns the token file path for the given prefix.
// The resulting path is ~/.mcp-openapi-proxy/{prefix}-tokens.json.
func TokenFilePath(prefix string) string {
	home, _ := os.UserHomeDir()
	fileName := prefix + "-" + tokenFileName
	return filepath.Join(home, configDirName, fileName)
}

// NewOIDCTokenProvider loads tokens from disk. Returns an error when the file
// is missing -- callers should prompt the user to run the login command.
func NewOIDCTokenProvider(filePath string) (*OIDCTokenProvider, error) {
	tokens, err := loadTokens(filePath)
	if err != nil {
		return nil, fmt.Errorf("no stored tokens (run login first): %w", err)
	}
	return &OIDCTokenProvider{
		tokens:   tokens,
		filePath: filePath,
		client:   &http.Client{Timeout: refreshTimeout},
	}, nil
}

// Token returns a valid access token, refreshing if close to expiry.
func (p *OIDCTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Now().Before(p.tokens.ExpiresAt.Add(-refreshMargin)) {
		return p.tokens.AccessToken, nil
	}

	if p.tokens.RefreshToken == "" {
		return "", fmt.Errorf("access token expired and no refresh token available -- run login again")
	}

	if err := p.refresh(ctx); err != nil {
		return "", fmt.Errorf("token refresh failed (run login again): %w", err)
	}
	return p.tokens.AccessToken, nil
}

func (p *OIDCTokenProvider) refresh(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {p.tokens.RefreshToken},
		"client_id":     {p.tokens.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.tokens.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", p.tokens.TokenEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}

	p.tokens.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		p.tokens.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		p.tokens.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}

	return p.save()
}

func (p *OIDCTokenProvider) save() error {
	return SaveTokens(p.filePath, p.tokens)
}

// SaveTokens writes tokens to disk with 0600 permissions.
func SaveTokens(filePath string, tokens *StoredTokens) error {
	if err := os.MkdirAll(filepath.Dir(filePath), tokenDirPerm); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, tokenFilePerm)
}

func loadTokens(filePath string) (*StoredTokens, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var tokens StoredTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("stored tokens file is incomplete")
	}
	return &tokens, nil
}

// tokenResponse is the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}
