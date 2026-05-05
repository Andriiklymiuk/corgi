package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSplitCsv(t *testing.T) {
	prefix, current, already := splitCsv("api,broker,el")
	if prefix != "api,broker," || current != "el" {
		t.Errorf("got prefix=%q current=%q", prefix, current)
	}
	if _, ok := already["api"]; !ok {
		t.Errorf("api missing in already")
	}
	if _, ok := already["broker"]; !ok {
		t.Errorf("broker missing in already")
	}

	prefix, current, _ = splitCsv("api,")
	if prefix != "api," || current != "" {
		t.Errorf("got %q %q", prefix, current)
	}

	prefix, current, _ = splitCsv("api")
	if prefix != "" || current != "api" {
		t.Errorf("got %q %q", prefix, current)
	}
}

func TestWithCsvPrefixNoPrefix(t *testing.T) {
	got, dir := withCsvPrefix("", []string{"a", "b"})
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("got %v", got)
	}
	if dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("dir = %d", dir)
	}
}

func TestWithCsvPrefixWithPrefix(t *testing.T) {
	got, dir := withCsvPrefix("api,", []string{"b", "c"})
	if len(got) != 2 || got[0] != "api,b" || got[1] != "api,c" {
		t.Errorf("got %v", got)
	}
	if dir&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Errorf("missing NoSpace dir")
	}
}

func TestCompleteCleanItems(t *testing.T) {
	got, _ := completeCleanItems(nil, nil, "")
	if len(got) != 4 {
		t.Errorf("got %v", got)
	}
}

func TestCompleteCleanItemsCsvAlready(t *testing.T) {
	got, _ := completeCleanItems(nil, nil, "db,services,")
	for _, item := range got {
		if strings.Contains(item, "db,services,db") || strings.Contains(item, "db,services,services") {
			t.Errorf("dup not filtered: %v", got)
		}
	}
}

func TestCompleteRunOmit(t *testing.T) {
	got, _ := completeRunOmit(nil, nil, "")
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestCompleteTunnelProvider(t *testing.T) {
	got, _ := completeTunnelProvider(nil, nil, "")
	if len(got) == 0 {
		t.Error("expected providers")
	}
}

func TestCompleteDockerContext(t *testing.T) {
	got, _ := completeDockerContext(nil, nil, "")
	if len(got) != 3 {
		t.Errorf("got %v", got)
	}
}

func TestCompleteTemplateName(t *testing.T) {
	got, _ := completeTemplateName(nil, nil, "")
	if len(got) == 0 {
		t.Error("expected examples")
	}
}

func newCompletionCmd(filename string) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String("filename", filename, "")
	return c
}

func TestLoadComposeForCompletionMissingFile(t *testing.T) {
	c := newCompletionCmd("/nonexistent/zzz.yml")
	if got := loadComposeForCompletion(c); got != nil {
		t.Errorf("got %+v", got)
	}
}

func TestLoadComposeForCompletionValid(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(yml, []byte("name: x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := loadComposeForCompletion(newCompletionCmd(yml))
	if got == nil {
		t.Fatal("nil")
	}
}

func TestCompleteServicesNoCompose(t *testing.T) {
	c := newCompletionCmd("/nonexistent/zzz.yml")
	got, _ := completeServices(c, nil, "")
	if got != nil {
		t.Errorf("got %v", got)
	}
}

func TestCompleteServicesWithCompose(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "compose.yml")
	body := `services:
  api:
    port: 3000
  worker:
    port: 4000
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, _ := completeServices(newCompletionCmd(yml), nil, "")
	if len(got) < 2 {
		t.Errorf("got %v", got)
	}
}

func TestCompleteTunnelableServices(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "compose.yml")
	body := `services:
  api:
    port: 3000
  no-port:
    {}
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, _ := completeTunnelableServices(newCompletionCmd(yml), nil, "")
	for _, n := range got {
		if n == "no-port" {
			t.Errorf("no-port should be filtered")
		}
	}
}

func TestCompleteDbServices(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "compose.yml")
	body := `db_services:
  pg:
    driver: postgres
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	got, _ := completeDbServices(newCompletionCmd(yml), nil, "")
	found := false
	for _, n := range got {
		if n == "pg" {
			found = true
		}
	}
	if !found {
		t.Errorf("got %v", got)
	}
}

func TestCompleteScriptNamesWithCompose(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "compose.yml")
	body := `services:
  api:
    scripts:
      - name: deploy
        commands: [echo hi]
      - name: test
        commands: [echo bye]
`
	if err := os.WriteFile(yml, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	c := newCompletionCmd(yml)
	c.Flags().StringSlice("services", nil, "")
	got, _ := completeScriptNames(c, nil, "")
	if len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestCollectScriptNameSeenAndAlready(t *testing.T) {
	already := map[string]struct{}{}
	seen := map[string]struct{}{}
	if _, ok := collectScriptName(utils.Script{Name: "x"}, already, seen); !ok {
		t.Error("first should succeed")
	}
	if _, ok := collectScriptName(utils.Script{Name: "x"}, already, seen); ok {
		t.Error("dup should fail")
	}
}
