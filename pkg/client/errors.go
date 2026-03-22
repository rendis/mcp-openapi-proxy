package client

import "fmt"

// BodyTooLargeError reports a response that exceeded the configured limit.
type BodyTooLargeError struct {
	Limit int64
}

func (e *BodyTooLargeError) Error() string {
	return fmt.Sprintf("response body exceeds MCP_MAX_BODY_BYTES (%d bytes)", e.Limit)
}
