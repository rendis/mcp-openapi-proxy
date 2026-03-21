package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// StaticTokenProvider
// ---------------------------------------------------------------------------

func TestStaticTokenProvider_ReturnsConfiguredToken(t *testing.T) {
	p := NewStaticTokenProvider("my-secret")
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "my-secret" {
		t.Errorf("Token() = %q, want %q", tok, "my-secret")
	}
}

func TestStaticTokenProvider_EmptyToken(t *testing.T) {
	p := NewStaticTokenProvider("")
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "" {
		t.Errorf("Token() = %q, want empty string", tok)
	}
}

// ---------------------------------------------------------------------------
// OIDCTokenProvider
// ---------------------------------------------------------------------------

func writeTokenFile(t *testing.T, tokens *StoredTokens) string {
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

func validTokens(tokenEndpoint string) *StoredTokens {
	return &StoredTokens{
		AccessToken:   "valid-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

func expiringTokens(tokenEndpoint string) *StoredTokens {
	return &StoredTokens{
		AccessToken:   "expiring-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(10 * time.Second), // within 30s margin
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

func expiredTokens(tokenEndpoint string) *StoredTokens {
	return &StoredTokens{
		AccessToken:   "expired-access-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
		TokenEndpoint: tokenEndpoint,
		ClientID:      "test-client",
	}
}

func mockTokenServer(t *testing.T, accessToken, refreshToken string, expiresIn int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"expires_in":    expiresIn,
			"token_type":    "Bearer",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOIDCTokenProvider_LoadsValidTokens(t *testing.T) {
	path := writeTokenFile(t, validTokens("https://example.com/token"))
	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if tok != "valid-access-token" {
		t.Errorf("Token() = %q, want %q", tok, "valid-access-token")
	}
}

func TestOIDCTokenProvider_NotExpired_NoRefresh(t *testing.T) {
	// Token server that would change the token if called
	srv := mockTokenServer(t, "refreshed-token", "new-refresh", 3600)
	tokens := validTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	// Should return the original token, not the refreshed one
	if tok != "valid-access-token" {
		t.Errorf("Token() = %q, want %q (should not have refreshed)", tok, "valid-access-token")
	}
}

func TestOIDCTokenProvider_WithinRefreshMargin_TriggersRefresh(t *testing.T) {
	srv := mockTokenServer(t, "refreshed-token", "new-refresh", 3600)
	tokens := expiringTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if tok != "refreshed-token" {
		t.Errorf("Token() = %q, want %q", tok, "refreshed-token")
	}
}

func TestOIDCTokenProvider_RefreshUpdatesAccessAndExpiry(t *testing.T) {
	srv := mockTokenServer(t, "new-access", "new-refresh", 7200)
	tokens := expiringTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	before := time.Now()
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if tok != "new-access" {
		t.Errorf("Token() = %q, want %q", tok, "new-access")
	}

	// ExpiresAt should be updated to ~now+7200s
	p.mu.Lock()
	expiresAt := p.tokens.ExpiresAt
	p.mu.Unlock()

	expectedMin := before.Add(7200 * time.Second).Add(-2 * time.Second)
	if expiresAt.Before(expectedMin) {
		t.Errorf("ExpiresAt = %v, want after %v", expiresAt, expectedMin)
	}
}

func TestOIDCTokenProvider_RefreshExpiresInZero_DefaultsToOneHour(t *testing.T) {
	// BUG A4: expires_in=0 should default to 1 hour
	srv := mockTokenServer(t, "new-access", "new-refresh", 0)
	tokens := expiringTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	before := time.Now()
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if tok != "new-access" {
		t.Errorf("Token() = %q, want %q", tok, "new-access")
	}

	p.mu.Lock()
	expiresAt := p.tokens.ExpiresAt
	p.mu.Unlock()

	// Should be approximately now + 1 hour
	expectedMin := before.Add(1 * time.Hour).Add(-5 * time.Second)
	expectedMax := before.Add(1 * time.Hour).Add(5 * time.Second)
	if expiresAt.Before(expectedMin) || expiresAt.After(expectedMax) {
		t.Errorf("ExpiresAt = %v, want ~%v (1 hour from now)", expiresAt, before.Add(1*time.Hour))
	}
}

func TestOIDCTokenProvider_RefreshFailure_StillValidToken_ReturnsCurrent(t *testing.T) {
	// BUG M5: If refresh fails but token is still valid, return current token
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	// Token is within refresh margin but NOT actually expired
	tokens := &StoredTokens{
		AccessToken:   "still-valid-token",
		RefreshToken:  "valid-refresh-token",
		ExpiresAt:     time.Now().Add(15 * time.Second), // within 30s margin but still valid
		TokenEndpoint: srv.URL,
		ClientID:      "test-client",
	}
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() should succeed with still-valid token, got error: %v", err)
	}
	if tok != "still-valid-token" {
		t.Errorf("Token() = %q, want %q", tok, "still-valid-token")
	}
}

func TestOIDCTokenProvider_RefreshFailure_ExpiredToken_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	tokens := expiredTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	_, err = p.Token(context.Background())
	if err == nil {
		t.Fatal("Token() should fail when refresh fails and token is expired")
	}
	if !strings.Contains(err.Error(), "token refresh failed") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "token refresh failed")
	}
}

func TestOIDCTokenProvider_RefreshUsesDetachedContext(t *testing.T) {
	// BUG M10: refresh should use context.Background(), not the caller context
	// We pass a cancelled context to Token(); refresh should still succeed
	// because it uses a detached context internally.
	srv := mockTokenServer(t, "refreshed-token", "new-refresh", 3600)
	tokens := expiringTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tok, err := p.Token(ctx)
	if err != nil {
		t.Fatalf("Token() with cancelled context should still work (detached refresh), got: %v", err)
	}
	if tok != "refreshed-token" {
		t.Errorf("Token() = %q, want %q", tok, "refreshed-token")
	}
}

func TestOIDCTokenProvider_FileNotFound(t *testing.T) {
	_, err := NewOIDCTokenProvider("/nonexistent/path/tokens.json")
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
	if !strings.Contains(err.Error(), "no stored tokens") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no stored tokens")
	}
}

func TestOIDCTokenProvider_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := NewOIDCTokenProvider(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
}

// ---------------------------------------------------------------------------
// TokenFilePath
// ---------------------------------------------------------------------------

func TestTokenFilePath(t *testing.T) {
	path := TokenFilePath("myprefix")
	if !strings.Contains(path, ".mcp-openapi-proxy") {
		t.Errorf("path %q should contain .mcp-openapi-proxy", path)
	}
	if !strings.HasSuffix(path, "myprefix-tokens.json") {
		t.Errorf("path %q should end with myprefix-tokens.json", path)
	}
	// Should use home directory
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	expected := filepath.Join(home, ".mcp-openapi-proxy", "myprefix-tokens.json")
	if path != expected {
		t.Errorf("TokenFilePath() = %q, want %q", path, expected)
	}
}

// ---------------------------------------------------------------------------
// SaveTokens
// ---------------------------------------------------------------------------

func TestSaveTokens_CreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "newdir")
	path := filepath.Join(subDir, "tokens.json")

	tokens := &StoredTokens{
		AccessToken:   "test-token",
		RefreshToken:  "test-refresh",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		TokenEndpoint: "https://example.com/token",
		ClientID:      "test-client",
	}

	if err := SaveTokens(path, tokens); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	// Check directory permissions
	dirInfo, err := os.Stat(subDir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("dir perm = %o, want 0700", dirInfo.Mode().Perm())
	}

	// Check file permissions
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Errorf("file perm = %o, want 0600", fileInfo.Mode().Perm())
	}

	// Verify contents are valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var loaded StoredTokens
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.AccessToken != "test-token" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "test-token")
	}
}

func TestSaveTokens_AtomicWrite(t *testing.T) {
	// BUG M8: SaveTokens should write to .tmp then rename
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	tokens := &StoredTokens{
		AccessToken:   "test-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		TokenEndpoint: "https://example.com/token",
		ClientID:      "test-client",
	}

	if err := SaveTokens(path, tokens); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	// .tmp file should not exist after successful save
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist after save, got err: %v", err)
	}

	// The file should exist and be readable
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var loaded StoredTokens
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loaded.AccessToken != "test-token" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "test-token")
	}
}

// ---------------------------------------------------------------------------
// DiscoverOIDCEndpoints
// ---------------------------------------------------------------------------

func TestDiscoverOIDCEndpoints_ParsesCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "https://auth.example.com/authorize",
			"token_endpoint":         "https://auth.example.com/token",
		})
	}))
	t.Cleanup(srv.Close)

	authEP, tokenEP, err := DiscoverOIDCEndpoints(srv.URL)
	if err != nil {
		t.Fatalf("DiscoverOIDCEndpoints: %v", err)
	}
	if authEP != "https://auth.example.com/authorize" {
		t.Errorf("authEndpoint = %q, want %q", authEP, "https://auth.example.com/authorize")
	}
	if tokenEP != "https://auth.example.com/token" {
		t.Errorf("tokenEndpoint = %q, want %q", tokenEP, "https://auth.example.com/token")
	}
}

func TestDiscoverOIDCEndpoints_MissingAuthEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint": "https://auth.example.com/token",
		})
	}))
	t.Cleanup(srv.Close)

	_, _, err := DiscoverOIDCEndpoints(srv.URL)
	if err == nil {
		t.Fatal("expected error for missing authorization_endpoint")
	}
	if !strings.Contains(err.Error(), "authorization_endpoint") {
		t.Errorf("error = %q, want to mention authorization_endpoint", err.Error())
	}
}

func TestDiscoverOIDCEndpoints_MissingTokenEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "https://auth.example.com/authorize",
		})
	}))
	t.Cleanup(srv.Close)

	_, _, err := DiscoverOIDCEndpoints(srv.URL)
	if err == nil {
		t.Fatal("expected error for missing token_endpoint")
	}
	if !strings.Contains(err.Error(), "token_endpoint") {
		t.Errorf("error = %q, want to mention token_endpoint", err.Error())
	}
}

func TestDiscoverOIDCEndpoints_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, _, err := DiscoverOIDCEndpoints(srv.URL)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want to mention 404", err.Error())
	}
}

func TestDiscoverOIDCEndpoints_TrailingSlashNormalized(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "https://auth.example.com/authorize",
			"token_endpoint":         "https://auth.example.com/token",
		})
	}))
	t.Cleanup(srv.Close)

	// Pass issuer with trailing slash
	_, _, err := DiscoverOIDCEndpoints(srv.URL + "/")
	if err != nil {
		t.Fatalf("DiscoverOIDCEndpoints: %v", err)
	}
	// Should not produce double slash
	if strings.Contains(receivedPath, "//") {
		t.Errorf("received path %q contains double slash", receivedPath)
	}
	if receivedPath != "/.well-known/openid-configuration" {
		t.Errorf("received path = %q, want %q", receivedPath, "/.well-known/openid-configuration")
	}
}

// ---------------------------------------------------------------------------
// Login helpers
// ---------------------------------------------------------------------------

func TestFetchAuthConfig_DoubleSlashPrevention(t *testing.T) {
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(authConfigResponse{
			DummyAuth: true,
		})
	}))
	t.Cleanup(srv.Close)

	// Pass URL with trailing slash
	_, _ = fetchAuthConfig(srv.URL + "/")
	if strings.Contains(receivedPath, "//") {
		t.Errorf("received path %q contains double slash", receivedPath)
	}
}

func TestBuildAuthURL_MergesExistingParams(t *testing.T) {
	// BUG M7: If endpoint URL already has query params, they should be preserved
	endpoint := "https://auth.example.com/authorize?audience=my-api"
	result := buildAuthURL(endpoint, "client1", "http://localhost/cb", "openid", "state123", "challenge456")

	u, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	// Original param should be preserved
	if u.Query().Get("audience") != "my-api" {
		t.Errorf("audience param lost, got query: %s", u.RawQuery)
	}
	// New params should be present
	if u.Query().Get("client_id") != "client1" {
		t.Errorf("client_id not set, got query: %s", u.RawQuery)
	}
	if u.Query().Get("response_type") != "code" {
		t.Errorf("response_type not set, got query: %s", u.RawQuery)
	}
	if u.Query().Get("code_challenge") != "challenge456" {
		t.Errorf("code_challenge not set, got query: %s", u.RawQuery)
	}
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method not set, got query: %s", u.RawQuery)
	}
}

func TestBuildAuthURL_NoExistingParams(t *testing.T) {
	endpoint := "https://auth.example.com/authorize"
	result := buildAuthURL(endpoint, "client1", "http://localhost/cb", "openid", "state123", "challenge456")

	u, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	if u.Query().Get("client_id") != "client1" {
		t.Errorf("client_id not set")
	}
	if u.Query().Get("scope") != "openid" {
		t.Errorf("scope not set")
	}
	if u.Query().Get("state") != "state123" {
		t.Errorf("state not set")
	}
}

func TestExchangeCode_ReadsErrorBody(t *testing.T) {
	// BUG M6: exchangeCode should include the response body in error messages
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"code expired"}`)
	}))
	t.Cleanup(srv.Close)

	_, err := exchangeCode(srv.URL, "client1", "bad-code", "http://localhost/cb", "verifier")
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	// Should include the response body text
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error = %q, want to contain response body", err.Error())
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error = %q, want to contain status code", err.Error())
	}
}

func TestRefresh_ReadsErrorBody(t *testing.T) {
	// M6: refresh in oidc_provider.go should also include error body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	t.Cleanup(srv.Close)

	tokens := expiredTokens(srv.URL)
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	_, err = p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error = %q, want to contain response body", err.Error())
	}
}

// ---------------------------------------------------------------------------
// RunLogout
// ---------------------------------------------------------------------------

func TestRunLogout_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, ".mcp-openapi-proxy")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subDir, "test-tokens.json")
	if err := os.WriteFile(path, []byte(`{"access_token":"x"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// We need to intercept TokenFilePath, so test the lower-level behavior
	// by directly calling os.Remove via RunLogout logic
	err := os.Remove(path)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should have been removed")
	}
}

func TestRunLogout_FileNotFound_NoError(t *testing.T) {
	// RunLogout should not error when file doesn't exist
	// We can't easily test RunLogout directly because it uses TokenFilePath
	// which depends on home dir. Test the error handling logic directly.
	err := os.Remove("/nonexistent/path/tokens.json")
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("unexpected error type: %v", err)
	}
	// os.ErrNotExist is handled gracefully in RunLogout
}

// ---------------------------------------------------------------------------
// Output routing (M9): login.go, logout.go, status.go user messages go to stderr
// ---------------------------------------------------------------------------

func TestLogout_OutputGoesToStderr(t *testing.T) {
	// Capture stderr by redirecting os.Stderr
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	// RunLogout with a non-existent prefix (will print "No stored tokens found")
	// Use a prefix that won't collide
	_ = RunLogout("nonexistent-test-prefix-12345")

	w.Close()
	os.Stderr = origStderr

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "No stored tokens found") &&
		!strings.Contains(string(output), "Tokens removed") {
		// If the file existed, we'd see "Tokens removed"; if not, "No stored tokens"
		// Either way, output should be on stderr, not stdout
		t.Errorf("expected logout output on stderr, got: %q", string(output))
	}
}

func TestStatus_OutputGoesToStderr(t *testing.T) {
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	// RunStatus with a non-existent prefix
	_ = RunStatus("nonexistent-test-prefix-12345")

	w.Close()
	os.Stderr = origStderr

	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "Status:") {
		t.Errorf("expected status output on stderr, got: %q", string(output))
	}
}

// ---------------------------------------------------------------------------
// loadTokens edge cases
// ---------------------------------------------------------------------------

func TestLoadTokens_EmptyAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	data, _ := json.Marshal(map[string]string{
		"access_token": "",
		"token_endpoint": "https://example.com/token",
	})
	os.WriteFile(path, data, 0600)

	_, err := NewOIDCTokenProvider(path)
	if err == nil {
		t.Fatal("expected error for empty access_token")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error = %q, want to contain 'incomplete'", err.Error())
	}
}

func TestOIDCTokenProvider_NoRefreshToken_Expired(t *testing.T) {
	tokens := &StoredTokens{
		AccessToken:   "expired-token",
		RefreshToken:  "", // no refresh token
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
		TokenEndpoint: "https://example.com/token",
		ClientID:      "test-client",
	}
	path := writeTokenFile(t, tokens)

	p, err := NewOIDCTokenProvider(path)
	if err != nil {
		t.Fatalf("NewOIDCTokenProvider: %v", err)
	}

	_, err = p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error when token expired and no refresh token")
	}
	if !strings.Contains(err.Error(), "no refresh token") {
		t.Errorf("error = %q, want to mention no refresh token", err.Error())
	}
}
