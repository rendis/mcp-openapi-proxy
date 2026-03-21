package testutil

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
)

// MockTokenProvider returns a TokenProvider that always returns the given token.
func MockTokenProvider(token string) auth.TokenProvider {
	return auth.NewStaticTokenProvider(token)
}

// WriteTempTokenFile creates a temp token file and returns its path.
func WriteTempTokenFile(t *testing.T, tokens *auth.StoredTokens) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	data, err := json.Marshal(tokens)
	if err != nil {
		t.Fatalf("marshal tokens: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

// ValidTokens returns a StoredTokens that won't expire for 1 hour.
func ValidTokens(tokenEndpoint string) *auth.StoredTokens {
	return &auth.StoredTokens{
		AccessToken:   "valid-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

// ExpiringTokens returns a StoredTokens that expires within the refresh margin.
func ExpiringTokens(tokenEndpoint string) *auth.StoredTokens {
	return &auth.StoredTokens{
		AccessToken:   "expiring-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(10 * time.Second), // within 30s margin
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

// ExpiredTokens returns a StoredTokens that already expired.
func ExpiredTokens(tokenEndpoint string) *auth.StoredTokens {
	return &auth.StoredTokens{
		AccessToken:   "expired-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

// AssertToken calls Token() and checks it matches expected.
func AssertToken(t *testing.T, tp auth.TokenProvider, expected string) {
	t.Helper()
	token, err := tp.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if token != expected {
		t.Errorf("Token() = %q, want %q", token, expected)
	}
}
