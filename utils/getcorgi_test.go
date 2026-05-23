package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newCobraFull() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("global", false, "")
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		c.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		c.Flags().Bool(f, false, "")
	}
	return c
}

func TestGetCorgiServicesEmpty(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: testproj\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newCobraFull()
	got, err := GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "testproj" {
		t.Errorf("got %q", got.Name)
	}
}

func TestGetCorgiServicesWithDbAndService(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	body := `name: full
db_services:
  pg:
    driver: postgres
    port: 5432
services:
  api:
    port: 3000
required:
  node:
    install:
      - brew install node
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newCobraFull()
	got, err := GetCorgiServices(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DatabaseServices) != 1 {
		t.Errorf("dbs %v", got.DatabaseServices)
	}
	if len(got.Services) != 1 {
		t.Errorf("svcs %v", got.Services)
	}
	if len(got.Required) != 1 {
		t.Errorf("required %v", got.Required)
	}
}

func TestLoadCorgiComposeFileMissing(t *testing.T) {
	c := newCobraFull()
	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c.Flags().Set("filename", "no-such-file.yml")
	_, _, err := loadCorgiComposeFile(c)
	if err == nil {
		t.Error("expected err")
	}
}

func TestLoadCorgiComposeFileInvalidYaml(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(yml, []byte(":\n  : invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newCobraFull()
	c.Flags().Set("filename", yml)
	_, _, err := loadCorgiComposeFile(c)
	if err == nil {
		t.Error("expected unmarshal err")
	}
}

func TestGetCorgiServicesInterpolatesFromSiblingDotEnv(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	body := "name: full\ndb_services:\n  pg:\n    driver: postgres\n    password: ${DB_PASSWORD}\n"
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_PASSWORD=secret\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	got, err := GetCorgiServices(newCobraFull())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DatabaseServices) != 1 || got.DatabaseServices[0].Password != "secret" {
		t.Errorf("password not interpolated: %#v", got.DatabaseServices)
	}
}

func TestGetCorgiServicesLeavesUnsetVarUnresolved(t *testing.T) {
	// An unset var with no default must NOT fail the load (non-breaking): the
	// ${VAR} token is left literal (silently) so later tunnel / cross-service
	// resolvers can still handle it.
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: ${UNSET_NO_DEFAULT_VAR}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	got, err := GetCorgiServices(newCobraFull())
	if err != nil {
		t.Fatalf("load should succeed, leaving token unresolved: %v", err)
	}
	if !strings.Contains(got.Name, "${UNSET_NO_DEFAULT_VAR}") {
		t.Errorf("token should be left literal, got name %q", got.Name)
	}
}

func TestDetermineCorgiComposePathFilenameFlag(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "x.yml")
	if err := os.WriteFile(yml, []byte("name: x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := newCobraFull()
	c.Flags().Set("filename", yml)
	got, err := determineCorgiComposePath(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != yml {
		t.Errorf("got %q", got)
	}
}
