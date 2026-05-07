package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSupabaseAuthUserMetadataJSON(t *testing.T) {
	t.Run("nil returns empty object", func(t *testing.T) {
		u := SupabaseAuthUser{}
		if got := u.MetadataJSON(); got != "{}" {
			t.Errorf("got %q want {}", got)
		}
	})

	t.Run("populated metadata serializes", func(t *testing.T) {
		u := SupabaseAuthUser{Metadata: map[string]interface{}{
			"role": "admin",
			"n":    float64(42),
		}}
		got := u.MetadataJSON()
		var back map[string]interface{}
		if err := json.Unmarshal([]byte(got), &back); err != nil {
			t.Fatalf("not JSON: %v", err)
		}
		if back["role"] != "admin" || back["n"] != float64(42) {
			t.Errorf("got %v", back)
		}
	})
}

func TestProcessAdditionalDatabaseConfigNoDefinitionPath(t *testing.T) {
	add, user, pass := ProcessAdditionalDatabaseConfig(DatabaseService{
		User:     "u",
		Password: "p",
	}, "svc")
	if add.DefinitionPath != "" {
		t.Errorf("expected empty DefinitionPath, got %q", add.DefinitionPath)
	}
	if user != "u" || pass != "p" {
		t.Errorf("user/pass not preserved: %s / %s", user, pass)
	}
}

func TestProcessAdditionalDatabaseConfigMissingFile(t *testing.T) {
	add, user, _ := ProcessAdditionalDatabaseConfig(DatabaseService{
		User: "u",
		Additional: AdditionalDatabaseConfig{
			DefinitionPath: filepath.Join(t.TempDir(), "nope.json"),
		},
	}, "svc")
	if add.DefinitionPath != "" {
		t.Errorf("expected empty when file missing, got %q", add.DefinitionPath)
	}
	if user != "u" {
		t.Errorf("user not preserved")
	}
}

func TestProcessAdditionalDatabaseConfigRabbitMQDefinition(t *testing.T) {
	dir := t.TempDir()
	defPath := filepath.Join(dir, "definitions.json")
	body := `{"users":[{"name":"alice"}]}`
	if err := os.WriteFile(defPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	origPath := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = origPath })

	add, user, _ := ProcessAdditionalDatabaseConfig(DatabaseService{
		Driver: "rabbitmq",
		User:   "default",
		Additional: AdditionalDatabaseConfig{
			DefinitionPath: defPath,
		},
	}, "rabbit-svc")

	if user != "alice" {
		t.Errorf("expected user from definition (alice), got %q", user)
	}
	if add.DefinitionPath != "./definitions.json" {
		t.Errorf("DefinitionPath = %q, want ./definitions.json", add.DefinitionPath)
	}

	copied := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "rabbit-svc", "definitions.json")
	if _, err := os.Stat(copied); err != nil {
		t.Errorf("expected file copied to %s: %v", copied, err)
	}
}

func TestProcessRabbitMQDefinitionInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	defPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(defPath, []byte("{not valid"), 0644); err != nil {
		t.Fatal(err)
	}
	add, override := processRabbitMQDefinition(DatabaseService{
		Additional: AdditionalDatabaseConfig{DefinitionPath: defPath},
	}, "svc")
	if add.DefinitionPath != "" || override != "" {
		t.Errorf("expected empty result for invalid JSON: %+v %q", add, override)
	}
}

func TestProcessRabbitMQDefinitionEmptyUsers(t *testing.T) {
	dir := t.TempDir()
	defPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(defPath, []byte(`{"users":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	add, override := processRabbitMQDefinition(DatabaseService{
		Additional: AdditionalDatabaseConfig{DefinitionPath: defPath},
	}, "svc")
	if add.DefinitionPath != "" || override != "" {
		t.Errorf("expected empty result for empty users: %+v %q", add, override)
	}
}

func TestProcessRabbitMQDefinitionMissingFile(t *testing.T) {
	add, override := processRabbitMQDefinition(DatabaseService{
		Additional: AdditionalDatabaseConfig{DefinitionPath: "/no/such/file"},
	}, "svc")
	if add.DefinitionPath != "" || override != "" {
		t.Errorf("expected empty result for missing file")
	}
}

func TestCopyDefinitionFileToServiceDirectoryMissingSource(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	err := copyDefinitionFileToServiceDirectory("/no/such/source.json", "svc")
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestResolveAdditionalConfigNonRabbitDriver(t *testing.T) {
	dir := t.TempDir()
	defPath := filepath.Join(dir, "x.json")
	if err := os.WriteFile(defPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	add, override := resolveAdditionalConfig(DatabaseService{
		Driver:     "postgres",
		Additional: AdditionalDatabaseConfig{DefinitionPath: defPath},
	}, "svc")
	if add.DefinitionPath != defPath {
		t.Errorf("expected non-rabbit drivers to passthrough additional, got %q", add.DefinitionPath)
	}
	if override != "" {
		t.Errorf("expected no override for non-rabbit, got %q", override)
	}
}
