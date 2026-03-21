package auth

import (
	"errors"
	"fmt"
	"os"
)

// RunLogout removes stored OIDC tokens for the given prefix.
func RunLogout(prefix string) error {
	if prefix == "" {
		prefix = "default"
	}

	filePath := TokenFilePath(prefix)
	if err := os.Remove(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "No stored tokens found -- already logged out.")
			return nil
		}
		return fmt.Errorf("remove token file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Tokens removed from %s\n", filePath)
	return nil
}
