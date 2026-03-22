package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
)

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
	t.Setenv("MCP_OIDC_ISSUER", "")
	t.Setenv("MCP_OIDC_CLIENT_ID", "")
	t.Setenv("MCP_BASE_URL", "")
	err := runLogin()
	if err == nil || !strings.Contains(err.Error(), "login requires") {
		t.Fatalf("unexpected error: %v", err)
	}
}
