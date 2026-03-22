package main

import (
	"os"
	"strings"
	"testing"
)

func TestDocumentation_StatesCanonicalToolNamingAndInputContract(t *testing.T) {
	readme := readDoc(t, "README.md")
	skill := readDoc(t, "skills/mcp-openapi-proxy/SKILL.md")

	for _, doc := range []struct {
		name string
		body string
	}{
		{name: "README", body: readme},
		{name: "SKILL", body: skill},
	} {
		t.Run(doc.name, func(t *testing.T) {
			requiredFragments := []string{
				"{prefix}_{method}_{sanitized_path}",
				"discover the actual registered tools first",
				`"path": {}`,
				`"query": {}`,
				`"headers": {}`,
				`"body": {}`,
				`"X-Workspace": "acme"`,
				"MCP_EXTRA_HEADERS",
				"fe_get_admin_features",
				"fe_post_admin_features",
				"fe_get_admin_features_key",
				"fe_patch_admin_features_key_toggle",
				"fe_get_admin_workspaces",
			}
			for _, fragment := range requiredFragments {
				if !strings.Contains(doc.body, fragment) {
					t.Fatalf("%s missing fragment %q", doc.name, fragment)
				}
			}

			forbiddenFragments := []string{
				"fe_list_features",
				"fe_create_feature",
				"fe_get_feature",
				"fe_toggle_feature",
				"fe_list_workspaces",
				"fe_set_workspace",
			}
			for _, fragment := range forbiddenFragments {
				if strings.Contains(doc.body, fragment) {
					t.Fatalf("%s unexpectedly contains forbidden fragment %q", doc.name, fragment)
				}
			}
		})
	}
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
