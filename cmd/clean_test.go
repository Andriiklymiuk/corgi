package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestCleanServicesNoCompose(t *testing.T) {
	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newTestComposeCommand2()
	cleanServices(c)
}

func TestCleanServicesWithCompose(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	body := `services:
  app:
    path: ./app
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0755); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newTestComposeCommand2()
	cleanServices(c)
}

func TestRunCleanWithoutCompose(t *testing.T) {
	cwd, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newTestComposeCommand2()
	cleanItems = []string{"all"}
	runClean(c, nil)
}

func newTestComposeCommand2() *cobra.Command {
	_, c := newTestComposeCommand()
	return c
}
