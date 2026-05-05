package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetServiceInfoMissing(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	_, err := GetServiceInfo("nope")
	if err == nil {
		t.Error("expected err")
	}
}

func TestGetServiceInfoEmptyCompose(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "db1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("nothing relevant"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := GetServiceInfo("db1")
	if err == nil || !strings.Contains(err.Error(), "haven't found db_service info") {
		t.Errorf("expected db not found, got %v", err)
	}
}

func TestGetServiceInfoPostgres(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "db1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `version: "3"
services:
  postgres-db1:
    environment:
      - POSTGRES_USER=admin
      - POSTGRES_PASSWORD=secret
    ports:
      - "5432:5432"
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := GetServiceInfo("db1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Connection info to db1") {
		t.Errorf("got %q", got)
	}
}
