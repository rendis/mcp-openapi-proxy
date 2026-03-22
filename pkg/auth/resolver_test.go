package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

type fakeTokenProvider struct {
	token string
	err   error
	calls int
}

func (f *fakeTokenProvider) Token(context.Context) (string, error) {
	f.calls++
	return f.token, f.err
}

func bearerScheme(name string) spec.SecurityInfo {
	return spec.SecurityInfo{Name: name, Type: "http", Scheme: "bearer"}
}

func oauth2Scheme(name string) spec.SecurityInfo {
	return spec.SecurityInfo{Name: name, Type: "oauth2"}
}

func oidcScheme(name string) spec.SecurityInfo {
	return spec.SecurityInfo{Name: name, Type: "openIdConnect"}
}

func basicScheme(name string) spec.SecurityInfo {
	return spec.SecurityInfo{Name: name, Type: "http", Scheme: "basic"}
}

func apiKeyScheme(name, in, paramName string) spec.SecurityInfo {
	return spec.SecurityInfo{Name: name, Type: "apiKey", In: in, ParameterName: paramName}
}

func TestResolverResolve_PublicEndpointsReturnEmptyAuth(t *testing.T) {
	r := NewResolver("profile")

	for _, requirements := range [][]spec.SecurityRequirement{
		nil,
		{{Schemes: nil}},
	} {
		got, err := r.Resolve(context.Background(), requirements)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if got == nil {
			t.Fatal("Resolve() returned nil auth")
		}
		if len(got.Headers) != 0 || len(got.Query) != 0 || len(got.Cookies) != 0 {
			t.Fatalf("expected empty auth, got %#v", got)
		}
	}
}

func TestResolverResolve_BearerSources(t *testing.T) {
	t.Run("scheme token env wins", func(t *testing.T) {
		t.Setenv("MCP_AUTH_PARTNER_AUTH_TOKEN", "scheme-token")
		t.Setenv("MCP_AUTH_TOKEN", "global-token")
		r := NewResolver("profile")

		got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{bearerScheme("partner-auth")}},
		})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if authz := got.Headers.Get("Authorization"); authz != "Bearer scheme-token" {
			t.Fatalf("Authorization = %q", authz)
		}
	})

	t.Run("global token fallback", func(t *testing.T) {
		t.Setenv("MCP_AUTH_TOKEN", "global-token")
		r := NewResolver("profile")

		got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{bearerScheme("bearerAuth")}},
		})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if authz := got.Headers.Get("Authorization"); authz != "Bearer global-token" {
			t.Fatalf("Authorization = %q", authz)
		}
	})

	t.Run("oidc fallback", func(t *testing.T) {
		fake := &fakeTokenProvider{token: "oidc-token"}
		r := &Resolver{
			profile:    "profile",
			oidc:       fake,
			oidcLoaded: true,
		}

		got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{bearerScheme("bearerAuth")}},
		})
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if authz := got.Headers.Get("Authorization"); authz != "Bearer oidc-token" {
			t.Fatalf("Authorization = %q", authz)
		}
		if fake.calls != 1 {
			t.Fatalf("OIDC provider calls = %d, want 1", fake.calls)
		}
	})
}

func TestResolverResolve_BasicAuth(t *testing.T) {
	t.Setenv("MCP_AUTH_ADMIN_BASIC_USERNAME", "alice")
	t.Setenv("MCP_AUTH_ADMIN_BASIC_PASSWORD", "s3cr3t")
	r := NewResolver("profile")

	got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
		{Schemes: []spec.SecurityInfo{basicScheme("admin-basic")}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cr3t"))
	if authz := got.Headers.Get("Authorization"); authz != want {
		t.Fatalf("Authorization = %q, want %q", authz, want)
	}
}

func TestResolverResolve_APIKeys(t *testing.T) {
	tests := []struct {
		name      string
		envKey    string
		scheme    spec.SecurityInfo
		headerKey string
		queryKey  string
		cookieKey string
	}{
		{
			name:      "header",
			envKey:    "MCP_AUTH_X_API_KEY_KEY",
			scheme:    apiKeyScheme("x-api-key", "header", "X-API-Key"),
			headerKey: "X-API-Key",
		},
		{
			name:     "query",
			envKey:   "MCP_AUTH_QUERY_KEY_KEY",
			scheme:   apiKeyScheme("query-key", "query", "api_key"),
			queryKey: "api_key",
		},
		{
			name:      "cookie",
			envKey:    "MCP_AUTH_SESSION_COOKIE_KEY",
			scheme:    apiKeyScheme("session-cookie", "cookie", "session_id"),
			cookieKey: "session_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, "secret")
			r := NewResolver("profile")

			got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{tt.scheme}},
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if tt.headerKey != "" && got.Headers.Get(tt.headerKey) != "secret" {
				t.Fatalf("header %q = %q", tt.headerKey, got.Headers.Get(tt.headerKey))
			}
			if tt.queryKey != "" && got.Query.Get(tt.queryKey) != "secret" {
				t.Fatalf("query %q = %q", tt.queryKey, got.Query.Get(tt.queryKey))
			}
			if tt.cookieKey != "" && got.Cookies[tt.cookieKey] != "secret" {
				t.Fatalf("cookie %q = %q", tt.cookieKey, got.Cookies[tt.cookieKey])
			}
		})
	}
}

func TestResolverResolve_OAuthDelegatesToBearer(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		scheme spec.SecurityInfo
	}{
		{
			name:   "oauth2",
			envKey: "MCP_AUTH_OAUTH_LOGIN_TOKEN",
			scheme: oauth2Scheme("oauth-login"),
		},
		{
			name:   "openid-connect",
			envKey: "MCP_AUTH_OIDC_LOGIN_TOKEN",
			scheme: oidcScheme("oidc-login"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, "scheme-token")
			r := NewResolver("profile")

			got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{tt.scheme}},
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if authz := got.Headers.Get("Authorization"); authz != "Bearer scheme-token" {
				t.Fatalf("Authorization = %q", authz)
			}
		})
	}
}

func TestResolverResolve_ORSemantics(t *testing.T) {
	t.Setenv("MCP_AUTH_FIRST_AUTH_USERNAME", "alice")
	t.Setenv("MCP_AUTH_FIRST_AUTH_PASSWORD", "pw")
	t.Setenv("MCP_AUTH_TOKEN", "global-token")
	r := NewResolver("profile")

	got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
		{Schemes: []spec.SecurityInfo{basicScheme("first auth")}},
		{Schemes: []spec.SecurityInfo{bearerScheme("bearerAuth")}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if authz := got.Headers.Get("Authorization"); !strings.HasPrefix(authz, "Basic ") {
		t.Fatalf("Authorization = %q, want Basic", authz)
	}
}

func TestResolverResolve_ANDSemantics(t *testing.T) {
	t.Setenv("MCP_AUTH_BEARERAUTH_TOKEN", "bearer-token")
	t.Setenv("MCP_AUTH_HEADER_KEY_KEY", "header-key")
	t.Setenv("MCP_AUTH_SESSION_COOKIE_KEY", "cookie-key")
	r := NewResolver("profile")

	got, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
		{Schemes: []spec.SecurityInfo{
			bearerScheme("bearerAuth"),
			apiKeyScheme("header-key", "header", "X-API-Key"),
			apiKeyScheme("session-cookie", "cookie", "session_id"),
		}},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Headers.Get("Authorization") != "Bearer bearer-token" {
		t.Fatalf("Authorization = %q", got.Headers.Get("Authorization"))
	}
	if got.Headers.Get("X-API-Key") != "header-key" {
		t.Fatalf("X-API-Key = %q", got.Headers.Get("X-API-Key"))
	}
	if got.Cookies["session_id"] != "cookie-key" {
		t.Fatalf("session_id = %q", got.Cookies["session_id"])
	}
}

func TestResolverResolve_ErrorPropagation(t *testing.T) {
	t.Run("missing credentials", func(t *testing.T) {
		r := NewResolver("profile")
		_, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{basicScheme("admin-basic")}},
		})
		if err == nil || !strings.Contains(err.Error(), "MCP_AUTH_ADMIN_BASIC_USERNAME") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("oidc provider unavailable", func(t *testing.T) {
		r := &Resolver{
			profile:    "prod",
			oidcErr:    errors.New("token file missing"),
			oidcLoaded: true,
		}
		_, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{bearerScheme("bearerAuth")}},
		})
		if err == nil || !strings.Contains(err.Error(), `profile "prod"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("oidc token empty", func(t *testing.T) {
		r := &Resolver{
			profile:    "prod",
			oidc:       &fakeTokenProvider{},
			oidcLoaded: true,
		}
		_, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{bearerScheme("bearerAuth")}},
		})
		if err == nil || !strings.Contains(err.Error(), "resolved to an empty bearer token") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unsupported apiKey location", func(t *testing.T) {
		t.Setenv("MCP_AUTH_WEIRD_KEY_KEY", "secret")
		r := NewResolver("profile")
		_, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{apiKeyScheme("weird-key", "body", "api_key")}},
		})
		if err == nil || !strings.Contains(err.Error(), `unsupported apiKey location "body"`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unsupported scheme type", func(t *testing.T) {
		r := NewResolver("profile")
		_, err := r.Resolve(context.Background(), []spec.SecurityRequirement{
			{Schemes: []spec.SecurityInfo{{Name: "mutualTLS", Type: "mutualTLS"}}},
		})
		if err == nil || !strings.Contains(err.Error(), `unsupported security scheme`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResolverHelpers(t *testing.T) {
	if got := normalizeProfile(""); got != "default" {
		t.Fatalf("normalizeProfile(\"\") = %q", got)
	}
	if got := envSchemeName("partner-auth/v2 login"); got != "PARTNER_AUTH_V2_LOGIN" {
		t.Fatalf("envSchemeName() = %q", got)
	}
	got := dedupeStrings([]string{"b", "a", "b", "c", "a"})
	if want := []string{"b", "a", "c"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dedupeStrings() = %v, want %v", got, want)
	}
}
