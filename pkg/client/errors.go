package client

import (
	"fmt"
	"io"
	"net/http"
)

// APIError represents a parsed error response from the API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API %d: %s", e.StatusCode, e.Body)
}

func parseAPIError(resp *http.Response) *APIError {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{
			StatusCode: resp.StatusCode,
			Body:       fmt.Sprintf("HTTP %d (failed to read body)", resp.StatusCode),
		}
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}
}
