package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDo_JSONResponseAndHeaders(t *testing.T) {
	var gotRequestID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-Id")
		w.Header().Add("X-Trace", "a")
		w.Header().Add("X-Trace", "b")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"123"}`))
	}))
	defer srv.Close()

	c := New(map[string]string{"X-Default": "1"}, 1024)
	resp, err := c.Do(context.Background(), &Request{
		Method: "GET",
		URL:    srv.URL,
		Headers: http.Header{
			"X-Request-Id": []string{"req-1"},
		},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.ContentType != "application/json" {
		t.Fatalf("content type = %q", resp.ContentType)
	}
	if gotRequestID != "req-1" {
		t.Fatalf("X-Request-Id = %q", gotRequestID)
	}
	body, ok := resp.Body.(map[string]any)
	if !ok || body["id"] != "123" {
		t.Fatalf("unexpected body: %#v", resp.Body)
	}
	if len(resp.Headers["X-Trace"]) != 2 {
		t.Fatalf("expected repeated header values, got %#v", resp.Headers["X-Trace"])
	}
}

func TestClientDo_4xxIsReturnedNotRaised(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := New(nil, 1024)
	resp, err := c.Do(context.Background(), &Request{Method: "GET", URL: srv.URL})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body := resp.Body.(map[string]any)
	if body["error"] != "bad request" {
		t.Fatalf("unexpected body: %#v", resp.Body)
	}
}

func TestClientDo_TextAndBinaryResponses(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello"))
		}))
		defer srv.Close()

		c := New(nil, 1024)
		resp, err := c.Do(context.Background(), &Request{Method: "GET", URL: srv.URL})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if text, ok := resp.Body.(string); !ok || text != "hello" {
			t.Fatalf("unexpected body: %#v", resp.Body)
		}
	})

	t.Run("binary", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x01, 0x02, 0x03})
		}))
		defer srv.Close()

		c := New(nil, 1024)
		resp, err := c.Do(context.Background(), &Request{Method: "GET", URL: srv.URL})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		body, ok := resp.Body.(BinaryBody)
		if !ok {
			t.Fatalf("expected binary body wrapper, got %#v", resp.Body)
		}
		if body.Encoding != "base64" || body.SizeBytes != 3 {
			t.Fatalf("unexpected binary body: %#v", body)
		}
	})
}

func TestSelectContentType_UsesExpectedWhenMissing(t *testing.T) {
	if got := selectContentType("", []string{"application/json"}); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
}

func TestClientDo_EnforcesBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("too-large"))
	}))
	defer srv.Close()

	c := New(nil, 4)
	_, err := c.Do(context.Background(), &Request{Method: "GET", URL: srv.URL})
	var tooLarge *BodyTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected BodyTooLargeError, got %v", err)
	}
}
