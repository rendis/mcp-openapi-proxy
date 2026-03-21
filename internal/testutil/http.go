package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// MockAPI creates a test HTTP server that routes requests by "METHOD /path".
func MockAPI(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := handlers[key]; ok {
			h(w, r)
			return
		}
		// Fallback: try path only
		if h, ok := handlers[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// JSONHandler returns an http.HandlerFunc that responds with the given status and JSON body.
func JSONHandler(status int, body any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			json.NewEncoder(w).Encode(body)
		}
	}
}

// TextHandler returns an http.HandlerFunc that responds with text/plain.
func TextHandler(status int, text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		w.Write([]byte(text))
	}
}

// EmptyHandler returns an http.HandlerFunc that responds with just a status code and no body.
func EmptyHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}
}

// MockTokenEndpoint creates a test server that acts as an OIDC token endpoint.
func MockTokenEndpoint(t *testing.T, accessToken, refreshToken string, expiresIn int) *httptest.Server {
	t.Helper()
	return MockAPI(t, map[string]http.HandlerFunc{
		"POST /token": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"expires_in":    expiresIn,
				"token_type":    "Bearer",
			})
		},
	})
}

// MockWellKnown creates a test server that serves an OIDC discovery document.
func MockWellKnown(t *testing.T, authURL, tokenURL string) *httptest.Server {
	t.Helper()
	return MockAPI(t, map[string]http.HandlerFunc{
		"GET /.well-known/openid-configuration": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"authorization_endpoint": authURL,
				"token_endpoint":         tokenURL,
			})
		},
	})
}

// RecordingHandler captures the request for later inspection.
type RecordingHandler struct {
	Requests []*http.Request
	Status   int
	Body     any
}

// ServeHTTP implements http.Handler.
func (rh *RecordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rh.Requests = append(rh.Requests, r)
	w.Header().Set("Content-Type", "application/json")
	if rh.Status != 0 {
		w.WriteHeader(rh.Status)
	}
	if rh.Body != nil {
		json.NewEncoder(w).Encode(rh.Body)
	}
}
