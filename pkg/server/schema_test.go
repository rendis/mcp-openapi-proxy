package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildListEndpointsInputSchema(t *testing.T) {
	schema := buildListEndpointsInputSchema()
	if schema.Type != "object" {
		t.Fatalf("schema.Type = %q", schema.Type)
	}
	for _, key := range []string{"q", "tag", "path_prefix", "method", "auth", "deprecated", "cursor", "limit"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Fatalf("missing property %q", key)
		}
	}
	if len(schema.Required) != 0 {
		t.Fatalf("Required = %#v, want empty", schema.Required)
	}
}

func TestBuildDescribeEndpointInputSchema(t *testing.T) {
	schema := buildDescribeEndpointInputSchema()
	if schema.Type != "object" {
		t.Fatalf("schema.Type = %q", schema.Type)
	}
	if _, ok := schema.Properties["toolName"]; !ok {
		t.Fatal("missing toolName property")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "toolName" {
		t.Fatalf("Required = %#v", schema.Required)
	}
}

func TestBuildCallEndpointInputSchema(t *testing.T) {
	schema := buildCallEndpointInputSchema()
	if schema.Type != "object" {
		t.Fatalf("schema.Type = %q", schema.Type)
	}
	for _, key := range []string{"toolName", "path", "query", "headers", "cookies", "body"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Fatalf("missing property %q", key)
		}
	}
	if len(schema.Required) != 1 || schema.Required[0] != "toolName" {
		t.Fatalf("Required = %#v", schema.Required)
	}
}

func TestMapToJSONSchema_PreservesBooleanFalseAdditionalProperties(t *testing.T) {
	schema := mapToJSONSchema(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	})
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	if !strings.Contains(string(data), `"additionalProperties":false`) {
		t.Fatalf("expected additionalProperties=false, got %s", string(data))
	}
}
