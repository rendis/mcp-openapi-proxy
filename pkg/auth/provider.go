package auth

import "context"

// TokenProvider supplies bearer tokens for API requests.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// StaticTokenProvider returns a fixed token (e.g. "dev-token").
type StaticTokenProvider struct {
	token string
}

func NewStaticTokenProvider(token string) *StaticTokenProvider {
	return &StaticTokenProvider{token: token}
}

func (p *StaticTokenProvider) Token(_ context.Context) (string, error) {
	return p.token, nil
}
