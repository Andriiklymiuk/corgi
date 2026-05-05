package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetDumpFilename(t *testing.T) {
	tests := map[string]string{
		"mssql":        "dump.bak",
		"postgres":     "dump.sql",
		"cassandra":    "dump.cql",
		"scylla":       "dump.cql",
		"redis":        "dump.rdb",
		"redis-server": "dump.rdb",
		"keydb":        "dump.rdb",
		"surrealdb":    "dump.surql",
		"neo4j":        "dump.cypher",
		"couchdb":      "dump.json",
		"mongodb":      "dump.sql",
		"":             "dump.sql",
	}
	for driver, want := range tests {
		t.Run(driver, func(t *testing.T) {
			if got := GetDumpFilename(driver); got != want {
				t.Errorf("GetDumpFilename(%q) = %q want %q", driver, got, want)
			}
		})
	}
}

func TestGetExposedPortFromDockerfile(t *testing.T) {
	t.Run("port already set returns it directly", func(t *testing.T) {
		got, err := GetExposedPortFromDockerfile(Service{Port: 8080})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "8080" {
			t.Errorf("got %q want 8080", got)
		}
	})

	t.Run("reads EXPOSE from Dockerfile", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"),
			[]byte("FROM alpine\nEXPOSE 3000\nCMD echo\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := GetExposedPortFromDockerfile(Service{AbsolutePath: dir})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "3000" {
			t.Errorf("got %q want 3000", got)
		}
	})

	t.Run("missing Dockerfile errors", func(t *testing.T) {
		dir := t.TempDir()
		_, err := GetExposedPortFromDockerfile(Service{AbsolutePath: dir})
		if err == nil || !strings.Contains(err.Error(), "dockerfile not found") {
			t.Errorf("want not-found err, got %v", err)
		}
	})

	t.Run("Dockerfile without EXPOSE errors", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"),
			[]byte("FROM alpine\nCMD echo\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := GetExposedPortFromDockerfile(Service{AbsolutePath: dir})
		if err == nil || !strings.Contains(err.Error(), "no EXPOSE") {
			t.Errorf("want no-EXPOSE err, got %v", err)
		}
	})
}

func TestGetDbInfoFromString(t *testing.T) {
	t.Run("postgres env extracted", func(t *testing.T) {
		got := getDbInfoFromString("- POSTGRES_USER=admin", nil)
		if len(got) != 1 || !strings.Contains(got[0], "USER") || !strings.Contains(got[0], "admin") {
			t.Errorf("got %v", got)
		}
	})

	t.Run("rabbitmq env extracted", func(t *testing.T) {
		got := getDbInfoFromString("- RABBITMQ_DEFAULT_USER=guest", nil)
		if len(got) != 1 || !strings.Contains(got[0], "guest") {
			t.Errorf("got %v", got)
		}
	})

	t.Run("unmatched line returns input unchanged", func(t *testing.T) {
		got := getDbInfoFromString("nothing relevant here", nil)
		if got != nil {
			t.Errorf("got %v want nil", got)
		}
	})

	t.Run("port lines parsed", func(t *testing.T) {
		got := getDbInfoFromString(`    - "5432:5432"`, nil)
		if len(got) != 1 || !strings.Contains(got[0], "PORT") {
			t.Errorf("got %v", got)
		}
	})
}

func TestDriverConfigsEnvGenerator(t *testing.T) {
	db := DatabaseService{
		Host:         "localhost",
		User:         "admin",
		Password:     "secret",
		DatabaseName: "mydb",
		Port:         5432,
	}

	for name, cfg := range DriverConfigs {
		t.Run(name, func(t *testing.T) {
			if cfg.EnvGenerator == nil {
				t.Skip("no generator")
			}
			got := cfg.EnvGenerator(cfg.Prefix, db)
			if got == "" && name != "image" {
				t.Errorf("driver %s produced empty env", name)
			}
		})
	}
}

func TestServiceConfigsEnvGenerator(t *testing.T) {
	svc := Service{Port: 8080}
	for name, cfg := range ServiceConfigs {
		t.Run(name, func(t *testing.T) {
			got := cfg.EnvGenerator(cfg.Prefix, svc)
			if !strings.Contains(got, "PORT=8080") {
				t.Errorf("driver %s missing port: %q", name, got)
			}
			if !strings.Contains(got, "HOST=localhost") {
				t.Errorf("driver %s missing host: %q", name, got)
			}
		})
	}
}

func TestImageDriverFallbackPrefix(t *testing.T) {
	cfg := DriverConfigs["image"]
	got := cfg.EnvGenerator("", DatabaseService{
		ServiceName: "my-image",
		Host:        "",
		Port:        9000,
	})
	if !strings.Contains(got, "MY_IMAGE_URL=http://localhost:9000") {
		t.Errorf("missing fallback prefix in %q", got)
	}
}

func TestImageDriverNoPortEmitsBlank(t *testing.T) {
	cfg := DriverConfigs["image"]
	got := cfg.EnvGenerator("FOO_", DatabaseService{Host: "h"})
	if strings.Contains(got, "PORT") {
		t.Errorf("expected no PORT when port is 0, got %q", got)
	}
}
