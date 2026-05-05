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

func TestGetServiceInfoRabbitMQ(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "mq")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `version: "3"
services:
  rabbitmq-mq:
    environment:
      - RABBITMQ_DEFAULT_USER=guest
      - RABBITMQ_DEFAULT_PASS=guest
    ports:
      - "5672:5672"
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := GetServiceInfo("mq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Connection info to mq") {
		t.Errorf("got %q", got)
	}
}

func TestGetServiceInfoMongo(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "mongo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `version: "3"
services:
  mongo-db:
    environment:
      - MONGO_INITDB_ROOT_USERNAME=root
      - MONGO_INITDB_ROOT_PASSWORD=pass
    ports:
      - "27017:27017"
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := GetServiceInfo("mongo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Connection info to mongo") {
		t.Errorf("got %q", got)
	}
}

func TestGetServiceInfoMySQL(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	dir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, "mysql")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `version: "3"
services:
  mysql-db:
    environment:
      - MYSQL_USER=user
      - MYSQL_PASSWORD=pass
      - MYSQL_DATABASE=mydb
    ports:
      - "3306:3306"
`
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := GetServiceInfo("mysql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Connection info to mysql") {
		t.Errorf("got %q", got)
	}
}
