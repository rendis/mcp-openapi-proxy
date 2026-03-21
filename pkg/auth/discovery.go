package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// wellKnownConfig represents the relevant fields from an OpenID Connect
// Discovery document at {issuer}/.well-known/openid-configuration.
type wellKnownConfig struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// DiscoverOIDCEndpoints fetches the OpenID Connect Discovery document from the
// issuer's .well-known/openid-configuration endpoint and returns the
// authorization and token endpoints.
func DiscoverOIDCEndpoints(issuer string) (authEndpoint, tokenEndpoint string, err error) {
	wellKnownURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wellKnownURL)
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", wellKnownURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GET %s returned %d", wellKnownURL, resp.StatusCode)
	}

	var cfg wellKnownConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return "", "", fmt.Errorf("decode discovery document: %w", err)
	}

	if cfg.AuthorizationEndpoint == "" {
		return "", "", fmt.Errorf("discovery document missing authorization_endpoint")
	}
	if cfg.TokenEndpoint == "" {
		return "", "", fmt.Errorf("discovery document missing token_endpoint")
	}

	return cfg.AuthorizationEndpoint, cfg.TokenEndpoint, nil
}
