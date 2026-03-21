package auth

import (
	"fmt"
	"os"
	"time"
)

// RunStatus prints the current authentication state for the given prefix.
func RunStatus(prefix string) error {
	if prefix == "" {
		prefix = "default"
	}

	filePath := TokenFilePath(prefix)
	tokens, err := loadTokens(filePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Status: not logged in")
		fmt.Fprintf(os.Stderr, "Token file: %s (not found or invalid)\n", filePath)
		fmt.Fprintln(os.Stderr, "\nRun `mcp-openapi-proxy login` to authenticate.")
		return nil
	}

	now := time.Now()
	remaining := tokens.ExpiresAt.Sub(now).Truncate(time.Second)

	fmt.Fprintln(os.Stderr, "Status: logged in")
	fmt.Fprintf(os.Stderr, "Token file:     %s\n", filePath)
	fmt.Fprintf(os.Stderr, "Token endpoint: %s\n", tokens.TokenEndpoint)
	fmt.Fprintf(os.Stderr, "Client ID:      %s\n", tokens.ClientID)
	fmt.Fprintf(os.Stderr, "Expires at:     %s\n", tokens.ExpiresAt.Format(time.RFC3339))

	if remaining > 0 {
		fmt.Fprintf(os.Stderr, "Remaining:      %s\n", remaining)
	} else {
		fmt.Fprintf(os.Stderr, "Remaining:      EXPIRED (%s ago)\n", (-remaining))
	}

	if tokens.RefreshToken != "" {
		fmt.Fprintln(os.Stderr, "Refresh token:  present (auto-refresh enabled)")
	} else {
		fmt.Fprintln(os.Stderr, "Refresh token:  absent (manual re-login required on expiry)")
	}

	return nil
}
