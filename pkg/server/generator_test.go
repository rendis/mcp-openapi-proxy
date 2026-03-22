package server

import (
	"testing"

	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

func TestToolName(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		method string
		path   string
		want   string
	}{
		{name: "basic", prefix: "api", method: "GET", path: "/users", want: "api_get_users"},
		{name: "special chars", prefix: "api", method: "POST", path: "/users/{id}/roles-admin", want: "api_post_users_id_roles_admin"},
		{name: "dots", prefix: "svc", method: "GET", path: "/v1/health.check", want: "svc_get_v1_health_check"},
		{name: "trailing slash", prefix: "api", method: "GET", path: "/users/", want: "api_get_users"},
		{name: "empty path", prefix: "api", method: "GET", path: "", want: "api_get"},
		{name: "feature flags list", prefix: "fe", method: "GET", path: "/admin/features", want: "fe_get_admin_features"},
		{name: "feature flags create", prefix: "fe", method: "POST", path: "/admin/features", want: "fe_post_admin_features"},
		{name: "feature flag by key", prefix: "fe", method: "GET", path: "/admin/features/{key}", want: "fe_get_admin_features_key"},
		{name: "feature flag toggle", prefix: "fe", method: "PATCH", path: "/admin/features/{key}/toggle", want: "fe_patch_admin_features_key_toggle"},
		{name: "workspaces list", prefix: "fe", method: "GET", path: "/admin/workspaces", want: "fe_get_admin_workspaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolName(tt.prefix, tt.method, tt.path); got != tt.want {
				t.Fatalf("toolName(%q, %q, %q) = %q, want %q", tt.prefix, tt.method, tt.path, got, tt.want)
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/users/{id}", want: "users_id"},
		{path: "/users/{id}/roles-admin", want: "users_id_roles_admin"},
		{path: "/v1/health.check", want: "v1_health_check"},
		{path: "/users//nested", want: "users_nested"},
		{path: "/users/", want: "users"},
		{path: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := sanitizePath(tt.path); got != tt.want {
				t.Fatalf("sanitizePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestToolAnnotations(t *testing.T) {
	tests := []struct {
		method          string
		wantReadOnly    bool
		wantDestructive bool
		wantNil         bool
	}{
		{method: "GET", wantReadOnly: true},
		{method: "HEAD", wantReadOnly: true},
		{method: "OPTIONS", wantReadOnly: true},
		{method: "DELETE", wantDestructive: true},
		{method: "POST", wantNil: true},
		{method: "PUT", wantNil: true},
		{method: "PATCH", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := toolAnnotations(tt.method)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("toolAnnotations(%q) = %#v, want nil", tt.method, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("toolAnnotations(%q) = nil", tt.method)
			}
			if got.ReadOnlyHint != tt.wantReadOnly {
				t.Fatalf("ReadOnlyHint = %v, want %v", got.ReadOnlyHint, tt.wantReadOnly)
			}
			if tt.wantDestructive {
				if got.DestructiveHint == nil || !*got.DestructiveHint {
					t.Fatalf("DestructiveHint = %#v, want true", got.DestructiveHint)
				}
			} else if got.DestructiveHint != nil {
				t.Fatalf("DestructiveHint = %#v, want nil", got.DestructiveHint)
			}
		})
	}
}

func TestBuildTool_AssemblesNameSchemasAndAnnotations(t *testing.T) {
	ep := spec.Endpoint{
		Method: "GET",
		Path:   "/v1/health.check",
		Responses: []spec.ResponseInfo{
			{
				StatusCode: "200",
				Content: []spec.MediaType{
					{ContentType: "application/json", Schema: map[string]any{"type": "object"}},
				},
			},
		},
	}

	tool := buildTool(ep, "svc")
	if tool.Name != "svc_get_v1_health_check" {
		t.Fatalf("tool.Name = %q", tool.Name)
	}
	if tool.InputSchema == nil || tool.InputSchema.Type != "object" {
		t.Fatalf("InputSchema = %#v", tool.InputSchema)
	}
	if tool.OutputSchema == nil || tool.OutputSchema.Type != "object" {
		t.Fatalf("OutputSchema = %#v", tool.OutputSchema)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("Annotations = %#v", tool.Annotations)
	}
}
