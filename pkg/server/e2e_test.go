package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
)

func newGeneratedServer(t *testing.T, specPath string, httpClient *client.Client, authResolver *auth.Resolver, cfg Config) *mcp.Server {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "test"}, nil)
	GenerateTools(srv, loadEndpoints(t, specPath), httpClient, authResolver, cfg)
	return srv
}

func TestE2E_Petstore_ListsToolsAndInvokesEndpoints(t *testing.T) {
	var gotQuery string
	var gotBody map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pets":
			gotQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

		case r.Method == http.MethodPost && r.URL.Path == "/pets":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("Decode body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "pet-001", "name": gotBody["name"]})

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	srv := newGeneratedServer(
		t,
		"../../testdata/petstore.yaml",
		client.New(nil, 1<<20),
		auth.NewResolver("default"),
		Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"},
	)
	session := newClientSession(t, srv)

	names := listToolNames(t, session)
	requireToolNamesContain(t, names, "api_get_pets", "api_post_pets")

	getRes := callToolViaSession(t, session, "api_get_pets", map[string]any{
		"query": map[string]any{
			"limit": 10,
			"tags":  []string{"cat", "dog"},
		},
	})
	if getRes.IsError {
		t.Fatalf("unexpected error result: %#v", envelopeFromResult(t, getRes))
	}
	getEnv := envelopeFromResult(t, getRes)
	if statusCode(t, getEnv) != 200 {
		t.Fatalf("status = %#v", getEnv["status"])
	}
	if getEnv["content_type"] != "application/json" {
		t.Fatalf("content_type = %#v", getEnv["content_type"])
	}
	if gotQuery != "limit=10&tags=cat&tags=dog" && gotQuery != "tags=cat&tags=dog&limit=10" {
		t.Fatalf("unexpected query = %q", gotQuery)
	}

	postRes := callToolViaSession(t, session, "api_post_pets", map[string]any{
		"body": map[string]any{"name": "Buddy"},
	})
	if postRes.IsError {
		t.Fatalf("unexpected error result: %#v", envelopeFromResult(t, postRes))
	}
	postEnv := envelopeFromResult(t, postRes)
	if statusCode(t, postEnv) != 201 {
		t.Fatalf("status = %#v", postEnv["status"])
	}
	body := postEnv["body"].(map[string]any)
	if body["id"] != "pet-001" || body["name"] != "Buddy" {
		t.Fatalf("unexpected body = %#v", body)
	}
}

func TestE2E_HeadersAndAuth_PreservesErrorEnvelope(t *testing.T) {
	var authz, tenant string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authz = r.Header.Get("Authorization")
		tenant = r.Header.Get("X-Tenant")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer api.Close()

	t.Setenv("MCP_AUTH_TOKEN", "secret")
	srv := newGeneratedServer(
		t,
		"../../testdata/headers-and-auth.yaml",
		client.New(nil, 1<<20),
		auth.NewResolver("default"),
		Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"},
	)
	session := newClientSession(t, srv)

	names := listToolNames(t, session)
	requireToolNamesContain(t, names, "api_get_resources")

	res := callToolViaSession(t, session, "api_get_resources", map[string]any{
		"headers": map[string]any{"X-Tenant": "acme"},
	})
	if !res.IsError {
		t.Fatal("expected IsError=true for 400 response")
	}
	if authz != "Bearer secret" {
		t.Fatalf("Authorization = %q", authz)
	}
	if tenant != "acme" {
		t.Fatalf("X-Tenant = %q", tenant)
	}

	env := envelopeFromResult(t, res)
	if statusCode(t, env) != 400 {
		t.Fatalf("status = %#v", env["status"])
	}
	if env["content_type"] != "application/json" {
		t.Fatalf("content_type = %#v", env["content_type"])
	}
	body := env["body"].(map[string]any)
	if body["error"] != "bad request" {
		t.Fatalf("body = %#v", body)
	}
}

func TestE2E_EdgeCases_FormsAndMultipart(t *testing.T) {
	var (
		uploadFilename string
		uploadContent  string
		submitForm     url.Values
		submitCT       string
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/upload":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatalf("MultipartReader: %v", err)
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			uploadFilename = part.FileName()
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("ReadAll(part): %v", err)
			}
			uploadContent = string(data)
			_ = json.NewEncoder(w).Encode(map[string]any{"uploaded": true})

		case r.Method == http.MethodPost && r.URL.Path == "/submit":
			submitCT = r.Header.Get("Content-Type")
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			submitForm = r.PostForm
			_ = json.NewEncoder(w).Encode(map[string]any{"submitted": true})

		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	srv := newGeneratedServer(
		t,
		"../../testdata/edge-cases.yaml",
		client.New(nil, 1<<20),
		auth.NewResolver("default"),
		Config{BaseURL: api.URL, ToolPrefix: "api", AuthProfile: "default"},
	)
	session := newClientSession(t, srv)

	names := listToolNames(t, session)
	requireToolNamesContain(t, names, "api_post_upload", "api_post_submit")

	uploadRes := callToolViaSession(t, session, "api_post_upload", map[string]any{
		"body": map[string]any{
			"file": map[string]any{
				"source":       "base64",
				"data_base64":  base64.StdEncoding.EncodeToString([]byte("hello")),
				"filename":     "hello.txt",
				"content_type": "text/plain",
			},
		},
	})
	if uploadRes.IsError {
		t.Fatalf("unexpected upload error: %#v", envelopeFromResult(t, uploadRes))
	}
	if uploadFilename != "hello.txt" || uploadContent != "hello" {
		t.Fatalf("unexpected upload payload: %q %q", uploadFilename, uploadContent)
	}

	submitRes := callToolViaSession(t, session, "api_post_submit", map[string]any{
		"body": map[string]any{
			"name":  "Alice",
			"email": "alice@example.com",
		},
	})
	if submitRes.IsError {
		t.Fatalf("unexpected submit error: %#v", envelopeFromResult(t, submitRes))
	}
	if base, _, _ := mime.ParseMediaType(submitCT); base != "application/x-www-form-urlencoded" {
		t.Fatalf("submit content type = %q", submitCT)
	}
	if submitForm.Get("name") != "Alice" || submitForm.Get("email") != "alice@example.com" {
		t.Fatalf("unexpected form = %#v", submitForm)
	}
}

func TestE2E_FullContract_DeprecatedToolRegistration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		exclude bool
		present bool
	}{
		{name: "included by default", exclude: false, present: true},
		{name: "excluded when requested", exclude: true, present: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newGeneratedServer(
				t,
				"../../testdata/full-contract.yaml",
				client.New(nil, 1<<20),
				auth.NewResolver("default"),
				Config{BaseURL: "https://api.example.com", ToolPrefix: "api", AuthProfile: "default", ExcludeDeprecated: tc.exclude},
			)
			session := newClientSession(t, srv)
			names := listToolNames(t, session)
			if tc.present {
				requireToolNamesContain(t, names, "api_put_items_id")
			} else {
				requireToolNamesOmit(t, names, "api_put_items_id")
			}
		})
	}
}
