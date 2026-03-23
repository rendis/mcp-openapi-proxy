package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rendis/mcp-openapi-proxy/pkg/auth"
	"github.com/rendis/mcp-openapi-proxy/pkg/client"
)

const featureEvaluatorSpecPath = "../../testdata/upstream/feature-evaluator/openapi.yaml"

func newFeatureEvaluatorSession(t *testing.T, baseURL string) *mcp.ClientSession {
	t.Helper()
	srv := newGeneratedServer(
		t,
		featureEvaluatorSpecPath,
		client.New(nil, 1<<20),
		auth.NewResolver("default"),
		Config{BaseURL: baseURL, ToolPrefix: "fe", AuthProfile: "default"},
	)
	return newClientSession(t, srv)
}

func TestE2E_FeatureEvaluator_ToolsList_IsCompact(t *testing.T) {
	api := httptest.NewServer(http.NotFoundHandler())
	defer api.Close()

	session := newFeatureEvaluatorSession(t, api.URL+"/features")

	res := listToolsResult(t, session)
	names := listToolNames(t, session)
	requireToolNamesContain(t, names, "fe_list_endpoints", "fe_describe_endpoint", "fe_call_endpoint")
	if len(names) != 3 {
		t.Fatalf("len(names) = %d, want 3", len(names))
	}

	for _, tool := range res.Tools {
		if tool.OutputSchema != nil {
			t.Fatalf("tool %q unexpectedly has OutputSchema", tool.Name)
		}
	}

	payload, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("Marshal(ListToolsResult): %v", err)
	}
	if len(payload) >= 20*1024 {
		t.Fatalf("ListTools payload = %d bytes, want < %d", len(payload), 20*1024)
	}
}

func TestE2E_FeatureEvaluator_ListEndpoints_AdminFeatures(t *testing.T) {
	api := httptest.NewServer(http.NotFoundHandler())
	defer api.Close()

	session := newFeatureEvaluatorSession(t, api.URL+"/features")

	listRes := callToolViaSession(t, session, "fe_list_endpoints", map[string]any{
		"path_prefix": "/admin/features",
		"method":      "GET",
		"limit":       50,
	})
	payload := envelopeFromResult(t, listRes)
	items := payload["items"].([]any)
	item := requireListedEndpointItem(t, items, "fe_get_admin_features", "GET", "/admin/features")

	for _, key := range []string{"method", "path", "description", "requiredAuth", "tags", "deprecated"} {
		if _, ok := item[key]; !ok {
			t.Fatalf("item missing key %q: %#v", key, item)
		}
	}
	if item["description"] != "List features" {
		t.Fatalf("description = %#v, want %q", item["description"], "List features")
	}
	if _, ok := item["requiredAuth"].(string); !ok {
		t.Fatalf("requiredAuth = %#v", item["requiredAuth"])
	}
	if _, ok := item["tags"].([]any); !ok {
		t.Fatalf("tags = %#v", item["tags"])
	}

	pageRes := callToolViaSession(t, session, "fe_list_endpoints", map[string]any{
		"path_prefix": "/admin/features",
		"method":      "GET",
		"limit":       1,
	})
	pagePayload := envelopeFromResult(t, pageRes)
	if pagePayload["nextCursor"] == "" {
		t.Fatalf("nextCursor = %#v, want non-empty", pagePayload["nextCursor"])
	}
}

func TestE2E_FeatureEvaluator_DescribeEndpoint_Eval(t *testing.T) {
	api := httptest.NewServer(http.NotFoundHandler())
	defer api.Close()

	session := newFeatureEvaluatorSession(t, api.URL+"/features")

	res := callToolViaSession(t, session, "fe_describe_endpoint", map[string]any{
		"toolName": "fe_post_eval",
	})
	payload := envelopeFromResult(t, res)

	if payload["method"] != "POST" {
		t.Fatalf("method = %#v, want POST", payload["method"])
	}
	if payload["path"] != "/eval" {
		t.Fatalf("path = %#v, want /eval", payload["path"])
	}
	if payload["summary"] != "Evaluate a single feature" {
		t.Fatalf("summary = %#v", payload["summary"])
	}

	securityRequirements := payload["securityRequirements"].([]any)
	var schemeNames []string
	for _, rawRequirement := range securityRequirements {
		requirement := rawRequirement.(map[string]any)
		for _, rawScheme := range requirement["schemes"].([]any) {
			scheme := rawScheme.(map[string]any)
			schemeNames = append(schemeNames, scheme["name"].(string))
		}
	}
	if !slices.Contains(schemeNames, "BearerAuth") || !slices.Contains(schemeNames, "ApiKeyAuth") {
		t.Fatalf("security requirement scheme names = %v", schemeNames)
	}

	parameters := payload["parameters"].(map[string]any)
	headers := parameters["headers"].(map[string]any)
	headerProps := headers["properties"].(map[string]any)
	for _, headerName := range []string{"X-Environment", "X-Tenant-Id", "X-Campus-Id", "X-Program-Id"} {
		if _, ok := headerProps[headerName]; !ok {
			t.Fatalf("header %q not found in %#v", headerName, headerProps)
		}
	}

	requestBody := payload["requestBody"].(map[string]any)
	required, ok := requestBody["required"].(bool)
	if !ok || !required {
		t.Fatalf("requestBody.required = %#v", requestBody["required"])
	}
	if !hasContentType(requestBody["content"].([]any), "application/json") {
		t.Fatalf("requestBody.content = %#v", requestBody["content"])
	}

	if !hasResponseStatuses(payload["responses"].([]any), "200", "400", "401") {
		t.Fatalf("responses = %#v", payload["responses"])
	}
}

func TestE2E_FeatureEvaluator_CallEndpoint_EvalWithAPIKey(t *testing.T) {
	t.Setenv("MCP_AUTH_TOKEN", "")
	t.Setenv("MCP_AUTH_BEARERAUTH_KEY", "")
	t.Setenv("MCP_AUTH_APIKEYAUTH_KEY", "test-key")

	var (
		gotAuthKey     string
		gotEnvironment string
		gotPath        string
		gotBody        map[string]any
	)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthKey = r.Header.Get("X-Api-Key")
		gotEnvironment = r.Header.Get("X-Environment")

		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-123")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"featureKey":  "checkout-v2",
			"environment": "production",
			"value":       true,
		})
	}))
	defer api.Close()

	session := newFeatureEvaluatorSession(t, api.URL+"/features")

	res := callToolViaSession(t, session, "fe_call_endpoint", map[string]any{
		"toolName": "fe_post_eval",
		"headers": map[string]any{
			"X-Environment": "production",
		},
		"body": map[string]any{
			"featureKey": "checkout-v2",
			"context": map[string]any{
				"user": map[string]any{
					"id": "u-123",
				},
				"tenant": map[string]any{
					"id": "acme-corp",
				},
			},
			"environment": "production",
		},
	})
	if res.IsError {
		t.Fatalf("unexpected error result: %#v", envelopeFromResult(t, res))
	}

	if gotPath != "/features/eval" {
		t.Fatalf("request path = %q, want %q", gotPath, "/features/eval")
	}
	if gotAuthKey != "test-key" {
		t.Fatalf("X-Api-Key = %q, want %q", gotAuthKey, "test-key")
	}
	if gotEnvironment != "production" {
		t.Fatalf("X-Environment = %q, want %q", gotEnvironment, "production")
	}
	if gotBody["featureKey"] != "checkout-v2" {
		t.Fatalf("featureKey = %#v", gotBody["featureKey"])
	}
	contextMap, ok := gotBody["context"].(map[string]any)
	if !ok {
		t.Fatalf("context = %#v", gotBody["context"])
	}
	if nestedID(t, contextMap, "user") != "u-123" {
		t.Fatalf("context.user = %#v", contextMap["user"])
	}
	if nestedID(t, contextMap, "tenant") != "acme-corp" {
		t.Fatalf("context.tenant = %#v", contextMap["tenant"])
	}

	env := envelopeFromResult(t, res)
	if statusCode(t, env) != 200 {
		t.Fatalf("status = %#v", env["status"])
	}
	if env["content_type"] != "application/json" {
		t.Fatalf("content_type = %#v", env["content_type"])
	}
	headers, ok := env["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers = %#v", env["headers"])
	}
	if !strings.Contains(strings.Join(headerValues(headers, "X-Request-Id"), ","), "req-123") {
		t.Fatalf("headers = %#v", headers)
	}
	body := env["body"].(map[string]any)
	if body["featureKey"] != "checkout-v2" || body["environment"] != "production" || body["value"] != true {
		t.Fatalf("body = %#v", body)
	}
}

func requireListedEndpointItem(t *testing.T, items []any, toolName, method, path string) map[string]any {
	t.Helper()
	for _, item := range items {
		entry := item.(map[string]any)
		if entry["toolName"] == toolName && entry["method"] == method && entry["path"] == path {
			return entry
		}
	}
	t.Fatalf("endpoint %q %s %s not found in %#v", toolName, method, path, items)
	return nil
}

func hasContentType(items []any, contentType string) bool {
	for _, item := range items {
		entry := item.(map[string]any)
		if entry["contentType"] == contentType {
			return true
		}
	}
	return false
}

func hasResponseStatuses(items []any, statuses ...string) bool {
	found := make(map[string]bool, len(statuses))
	for _, item := range items {
		entry := item.(map[string]any)
		if status, ok := entry["status"].(string); ok {
			found[status] = true
		}
	}
	for _, status := range statuses {
		if !found[status] {
			return false
		}
	}
	return true
}

func nestedID(t *testing.T, ctx map[string]any, namespace string) string {
	t.Helper()
	raw, ok := ctx[namespace]
	if !ok {
		t.Fatalf("namespace %q missing from %#v", namespace, ctx)
	}
	ns, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("namespace %q = %#v", namespace, raw)
	}
	value, ok := ns["id"].(string)
	if !ok {
		t.Fatalf("%s.id = %#v", namespace, ns["id"])
	}
	return value
}

func headerValues(headers map[string]any, name string) []string {
	raw, ok := headers[name]
	if !ok {
		return nil
	}
	switch value := raw.(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return value
	case string:
		return []string{value}
	default:
		return nil
	}
}
