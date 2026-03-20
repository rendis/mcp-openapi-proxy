package auth

import (
	"fmt"
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
		fmt.Println("Status: not logged in")
		fmt.Printf("Token file: %s (not found or invalid)\n", filePath)
		fmt.Println("\nRun `mcp-openapi-proxy login` to authenticate.")
		return nil
	}

	now := time.Now()
	remaining := tokens.ExpiresAt.Sub(now).Truncate(time.Second)

	fmt.Println("Status: logged in")
	fmt.Printf("Token file:     %s\n", filePath)
	fmt.Printf("Token endpoint: %s\n", tokens.TokenEndpoint)
	fmt.Printf("Client ID:      %s\n", tokens.ClientID)
	fmt.Printf("Expires at:     %s\n", tokens.ExpiresAt.Format(time.RFC3339))

	if remaining > 0 {
		fmt.Printf("Remaining:      %s\n", remaining)
	} else {
		fmt.Printf("Remaining:      EXPIRED (%s ago)\n", (-remaining))
	}

	if tokens.RefreshToken != "" {
		fmt.Println("Refresh token:  present (auto-refresh enabled)")
	} else {
		fmt.Println("Refresh token:  absent (manual re-login required on expiry)")
	}

	return nil
}
