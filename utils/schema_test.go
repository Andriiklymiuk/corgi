package utils

import (
	"encoding/json"
	"testing"
)

func TestComposeJSONSchemaValid(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(ComposeJSONSchema()), &m); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if m["$schema"] == nil {
		t.Error("missing $schema")
	}
	props, ok := m["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties object")
	}
	if props["services"] == nil || props["db_services"] == nil {
		t.Error("schema missing top-level services/db_services")
	}
}
