package server

import (
	"testing"

	"github.com/rendis/mcp-openapi-proxy/pkg/spec"
)

func TestEndpointCatalog_FiltersAndPaginationHelpers(t *testing.T) {
	catalog := newEndpointCatalog([]spec.Endpoint{
		{
			Method:     "GET",
			Path:       "/admin/features",
			Summary:    "List feature flags",
			Tags:       []string{"flags", "admin"},
			Responses:  []spec.ResponseInfo{{StatusCode: "200"}},
			Deprecated: false,
			SecurityRequirements: []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{{Name: "bearerAuth", Type: "http", Scheme: "bearer"}}},
			},
		},
		{
			Method:      "PATCH",
			Path:        "/admin/features/{key}/toggle",
			Description: "Toggle a feature flag for a workspace",
			Tags:        []string{"flags"},
			Responses:   []spec.ResponseInfo{{StatusCode: "200"}},
			Deprecated:  true,
			SecurityRequirements: []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{{Name: "apiKeyHeader", Type: "apiKey", In: "header"}}},
			},
		},
		{
			Method:    "GET",
			Path:      "/status",
			Summary:   "Health status",
			Responses: []spec.ResponseInfo{{StatusCode: "200"}},
		},
	}, "api", false)

	if got := catalog.count(); got != 3 {
		t.Fatalf("catalog.count() = %d, want 3", got)
	}

	tagFiltered := filterEndpointEntries(catalog.entries, endpointListFilter{Tag: "flags", Limit: defaultEndpointListLimit})
	if len(tagFiltered) != 2 {
		t.Fatalf("tagFiltered = %d, want 2", len(tagFiltered))
	}

	queryFiltered := filterEndpointEntries(catalog.entries, endpointListFilter{Query: "workspace", Limit: defaultEndpointListLimit})
	if len(queryFiltered) != 1 || queryFiltered[0].ToolName != "api_patch_admin_features_key_toggle" {
		t.Fatalf("queryFiltered = %#v", queryFiltered)
	}

	authFiltered := filterEndpointEntries(catalog.entries, endpointListFilter{Auth: "bearer", Limit: defaultEndpointListLimit})
	if len(authFiltered) != 1 || authFiltered[0].ToolName != "api_get_admin_features" {
		t.Fatalf("authFiltered = %#v", authFiltered)
	}

	deprecated := true
	deprecatedFiltered := filterEndpointEntries(catalog.entries, endpointListFilter{Deprecated: &deprecated, Limit: defaultEndpointListLimit})
	if len(deprecatedFiltered) != 1 || deprecatedFiltered[0].ToolName != "api_patch_admin_features_key_toggle" {
		t.Fatalf("deprecatedFiltered = %#v", deprecatedFiltered)
	}

	cursor := encodeListCursor(2)
	offset, err := decodeListCursor(cursor)
	if err != nil {
		t.Fatalf("decodeListCursor: %v", err)
	}
	if offset != 2 {
		t.Fatalf("offset = %d, want 2", offset)
	}
}

func TestSummarizeRequiredAuth(t *testing.T) {
	tests := []struct {
		name string
		reqs []spec.SecurityRequirement
		want string
	}{
		{name: "none", want: "none"},
		{
			name: "single bearer",
			reqs: []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{{Name: "bearerAuth", Type: "http", Scheme: "bearer"}}},
			},
			want: "bearer",
		},
		{
			name: "and requirement",
			reqs: []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{
					{Name: "bearerAuth", Type: "http", Scheme: "bearer"},
					{Name: "apiKeyHeader", Type: "apiKey", In: "header"},
				}},
			},
			want: "bearer + apiKey",
		},
		{
			name: "or requirement",
			reqs: []spec.SecurityRequirement{
				{Schemes: []spec.SecurityInfo{{Name: "bearerAuth", Type: "http", Scheme: "bearer"}}},
				{Schemes: []spec.SecurityInfo{{Name: "apiKeyHeader", Type: "apiKey", In: "header"}}},
			},
			want: "bearer OR apiKey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarizeRequiredAuth(tt.reqs); got != tt.want {
				t.Fatalf("summarizeRequiredAuth() = %q, want %q", got, tt.want)
			}
		})
	}
}
