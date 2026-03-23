package server

import "testing"

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

func TestNavigatorToolNames(t *testing.T) {
	if got := listEndpointsToolName("api"); got != "api_list_endpoints" {
		t.Fatalf("listEndpointsToolName = %q", got)
	}
	if got := describeEndpointToolName("api"); got != "api_describe_endpoint" {
		t.Fatalf("describeEndpointToolName = %q", got)
	}
	if got := callEndpointToolName("api"); got != "api_call_endpoint" {
		t.Fatalf("callEndpointToolName = %q", got)
	}
}

func TestNavigatorTools_AreLightweight(t *testing.T) {
	listTool := buildListEndpointsTool("svc")
	if listTool.Name != "svc_list_endpoints" {
		t.Fatalf("listTool.Name = %q", listTool.Name)
	}
	if listTool.InputSchema == nil || listTool.InputSchema.Type != "object" {
		t.Fatalf("listTool.InputSchema = %#v", listTool.InputSchema)
	}
	if listTool.OutputSchema != nil {
		t.Fatalf("listTool.OutputSchema = %#v, want nil", listTool.OutputSchema)
	}
	if listTool.Annotations == nil || !listTool.Annotations.ReadOnlyHint {
		t.Fatalf("listTool.Annotations = %#v", listTool.Annotations)
	}

	describeTool := buildDescribeEndpointTool("svc")
	if describeTool.Name != "svc_describe_endpoint" {
		t.Fatalf("describeTool.Name = %q", describeTool.Name)
	}
	if describeTool.InputSchema == nil || describeTool.InputSchema.Type != "object" {
		t.Fatalf("describeTool.InputSchema = %#v", describeTool.InputSchema)
	}
	if describeTool.OutputSchema != nil {
		t.Fatalf("describeTool.OutputSchema = %#v, want nil", describeTool.OutputSchema)
	}
	if describeTool.Annotations == nil || !describeTool.Annotations.ReadOnlyHint {
		t.Fatalf("describeTool.Annotations = %#v", describeTool.Annotations)
	}

	callTool := buildCallEndpointTool("svc")
	if callTool.Name != "svc_call_endpoint" {
		t.Fatalf("callTool.Name = %q", callTool.Name)
	}
	if callTool.InputSchema == nil || callTool.InputSchema.Type != "object" {
		t.Fatalf("callTool.InputSchema = %#v", callTool.InputSchema)
	}
	if callTool.OutputSchema != nil {
		t.Fatalf("callTool.OutputSchema = %#v, want nil", callTool.OutputSchema)
	}
	if callTool.Annotations != nil {
		t.Fatalf("callTool.Annotations = %#v, want nil", callTool.Annotations)
	}
}
