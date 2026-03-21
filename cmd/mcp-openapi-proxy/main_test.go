package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
)

// ---------------------------------------------------------------------------
// parseExtraHeaders
// ---------------------------------------------------------------------------

func TestParseExtraHeaders(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{
			name: "basic key:value",
			raw:  "key:value",
			want: map[string]string{"key": "value"},
		},
		{
			name: "multiple pairs",
			raw:  "A:1,B:2",
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "value with colon",
			raw:  "Auth:Bearer tok:en",
			want: map[string]string{"Auth": "Bearer tok:en"},
		},
		{
			name: "empty string",
			raw:  "",
			want: map[string]string{},
		},
		{
			name: "whitespace trimmed",
			raw:  " key : value ",
			want: map[string]string{"key": "value"},
		},
		{
			name: "malformed no colon is skipped",
			raw:  "novalue",
			want: map[string]string{},
		},
		{
			name: "mixed valid and invalid",
			raw:  "A:1,bad,B:2",
			want: map[string]string{"A": "1", "B": "2"},
		},
		{
			name: "value with comma limitation",
			// Comma splits first, so "X-B:val,with,comma" becomes
			// "X-B:val", "with", "comma" — X-B only gets "val".
			raw:  "X-A:val1,X-B:val,with,comma",
			want: map[string]string{"X-A": "val1", "X-B": "val"},
		},
		{
			name: "trailing comma",
			raw:  "A:1,",
			want: map[string]string{"A": "1"},
		},
		{
			name: "leading comma",
			raw:  ",A:1",
			want: map[string]string{"A": "1"},
		},
		{
			name: "empty key after trim is skipped",
			raw:  " :value",
			want: map[string]string{},
		},
		{
			name: "only whitespace pair",
			raw:  " , , ",
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExtraHeaders(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("len mismatch: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("missing key %q", k)
				} else if gotV != wantV {
					t.Errorf("key %q: got %q, want %q", k, gotV, wantV)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveTokenProvider
// ---------------------------------------------------------------------------

func TestResolveTokenProvider_StaticToken(t *testing.T) {
	t.Setenv("MCP_AUTH_TOKEN", "my-secret-token")

	tp := resolveTokenProvider()

	tok, err := tp.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "my-secret-token" {
		t.Errorf("got %q, want %q", tok, "my-secret-token")
	}
}

func TestResolveTokenProvider_NoTokenNoFile(t *testing.T) {
	// Ensure MCP_AUTH_TOKEN is unset.
	t.Setenv("MCP_AUTH_TOKEN", "")

	// Override HOME so TokenFilePath points to a non-existent directory.
	t.Setenv("HOME", t.TempDir())

	// Capture stderr to verify warning.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	tp := resolveTokenProvider()

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	stderrOutput := buf.String()

	if stderrOutput == "" {
		t.Error("expected warning on stderr, got nothing")
	}

	tok, err := tp.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
}

func TestResolveTokenProvider_OIDCTokenFile(t *testing.T) {
	// Ensure MCP_AUTH_TOKEN is unset so we fall through to OIDC path.
	t.Setenv("MCP_AUTH_TOKEN", "")

	// Create a temp HOME with valid token file.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	tokenDir := filepath.Join(tmpHome, ".mcp-openapi-proxy")
	if err := os.MkdirAll(tokenDir, 0700); err != nil {
		t.Fatal(err)
	}

	stored := &auth.StoredTokens{
		AccessToken:   "oidc-access-token",
		RefreshToken:  "oidc-refresh-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
		TokenEndpoint: "https://example.com/token",
		ClientID:      "test-client",
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	tokenFile := filepath.Join(tokenDir, tokenPrefix+"-tokens.json")
	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		t.Fatal(err)
	}

	tp := resolveTokenProvider()

	// It should be an OIDCTokenProvider, returning the stored access token.
	tok, err := tp.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "oidc-access-token" {
		t.Errorf("got %q, want %q", tok, "oidc-access-token")
	}
}

// ---------------------------------------------------------------------------
// runLogin — config resolution paths (will fail at network calls, but we
// verify that the right code path is chosen based on env vars)
// ---------------------------------------------------------------------------

func TestRunLogin_OIDCDiscoveryPath(t *testing.T) {
	// Set OIDC vars pointing to a non-existent issuer.
	t.Setenv("MCP_OIDC_ISSUER", "http://127.0.0.1:1/nonexistent")
	t.Setenv("MCP_OIDC_CLIENT_ID", "test-client")
	// Clear MCP_BASE_URL so we don't fall through.
	t.Setenv("MCP_BASE_URL", "")

	err := runLogin()
	if err == nil {
		t.Fatal("expected error from OIDC discovery, got nil")
	}

	// The error should mention OIDC discovery.
	errMsg := err.Error()
	if !containsAny(errMsg, "OIDC discovery", "fetch") {
		t.Errorf("expected error mentioning OIDC discovery, got: %s", errMsg)
	}
}

func TestRunLogin_APIBaseURLPath(t *testing.T) {
	// No OIDC vars, but MCP_BASE_URL is set.
	t.Setenv("MCP_OIDC_ISSUER", "")
	t.Setenv("MCP_OIDC_CLIENT_ID", "")
	t.Setenv("MCP_BASE_URL", "http://127.0.0.1:1/nonexistent-api")

	err := runLogin()
	if err == nil {
		t.Fatal("expected error from API config fetch, got nil")
	}

	// Should have tried to fetch auth config from the API.
	errMsg := err.Error()
	if !containsAny(errMsg, "auth config", "fetch", "connection refused") {
		t.Errorf("expected error from API config path, got: %s", errMsg)
	}
}

func TestRunLogin_NeitherSet(t *testing.T) {
	t.Setenv("MCP_OIDC_ISSUER", "")
	t.Setenv("MCP_OIDC_CLIENT_ID", "")
	t.Setenv("MCP_BASE_URL", "")

	err := runLogin()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !containsAny(errMsg, "login requires") {
		t.Errorf("expected 'login requires' in error, got: %s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// runServe — env var validation
// ---------------------------------------------------------------------------

func TestRunServe_MissingMCPSpec(t *testing.T) {
	t.Setenv("MCP_SPEC", "")
	t.Setenv("MCP_BASE_URL", "http://example.com")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing MCP_SPEC")
	}
	if !containsAny(err.Error(), "MCP_SPEC") {
		t.Errorf("expected error mentioning MCP_SPEC, got: %s", err)
	}
}

func TestRunServe_MissingMCPBaseURL(t *testing.T) {
	t.Setenv("MCP_SPEC", "/some/spec.yaml")
	t.Setenv("MCP_BASE_URL", "")

	err := runServe()
	if err == nil {
		t.Fatal("expected error for missing MCP_BASE_URL")
	}
	if !containsAny(err.Error(), "MCP_BASE_URL") {
		t.Errorf("expected error mentioning MCP_BASE_URL, got: %s", err)
	}
}

// ---------------------------------------------------------------------------
// runLogin — partial OIDC config (only issuer OR only client_id)
// ---------------------------------------------------------------------------

func TestRunLogin_OnlyIssuerSet(t *testing.T) {
	t.Setenv("MCP_OIDC_ISSUER", "http://issuer.example.com")
	t.Setenv("MCP_OIDC_CLIENT_ID", "")
	t.Setenv("MCP_BASE_URL", "")

	err := runLogin()
	if err == nil {
		t.Fatal("expected error when only issuer is set")
	}
	if !containsAny(err.Error(), "login requires") {
		t.Errorf("expected 'login requires' error, got: %s", err)
	}
}

func TestRunLogin_OnlyClientIDSet(t *testing.T) {
	t.Setenv("MCP_OIDC_ISSUER", "")
	t.Setenv("MCP_OIDC_CLIENT_ID", "my-client")
	t.Setenv("MCP_BASE_URL", "")

	err := runLogin()
	if err == nil {
		t.Fatal("expected error when only client_id is set")
	}
	if !containsAny(err.Error(), "login requires") {
		t.Errorf("expected 'login requires' error, got: %s", err)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
