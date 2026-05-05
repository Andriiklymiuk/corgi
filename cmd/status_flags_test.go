package cmd

import (
	"andriiklymiuk/corgi/utils"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("watch", false, "")
	c.Flags().Duration("interval", 2*time.Second, "")
	c.Flags().Bool("ready", false, "")
	c.Flags().Bool("until-healthy", false, "")
	c.Flags().Duration("timeout", 5*time.Minute, "")
	c.Flags().StringSlice("service", nil, "")
	c.Flags().Bool("json", false, "")
	c.Flags().BoolP("quiet", "q", false, "")
	return c
}

func TestReadStatusFlagsDefaults(t *testing.T) {
	got := readStatusFlags(newStatusCmd())
	if got.watch || got.untilHealthy || got.jsonOut || got.quiet {
		t.Errorf("got %+v", got)
	}
	if got.interval != 2*time.Second {
		t.Errorf("interval = %v", got.interval)
	}
}

func TestReadStatusFlagsReadyAlias(t *testing.T) {
	c := newStatusCmd()
	c.Flags().Set("ready", "true")
	got := readStatusFlags(c)
	if !got.untilHealthy {
		t.Error("ready should set untilHealthy")
	}
}

func TestRunStatusOnceHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{quiet: true, jsonOut: true})
}

func TestRunStatusOnceJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{jsonOut: true})
}

func TestRunStatusOnceQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{quiet: true})
}

func TestResolveStatusRowsEmpty(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: t\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	c := newStatusCmd()
	c.Flags().String("filename", "", "")
	c.Flags().Bool("global", false, "")
	c.Flags().String("fromTemplate", "", "")
	c.Flags().String("fromTemplateName", "", "")
	c.Flags().String("privateToken", "", "")
	c.Flags().Bool("exampleList", false, "")
	c.Flags().Bool("describe", false, "")
	got := resolveStatusRows(c)
	if got != nil {
		t.Errorf("got %v", got)
	}
}

func TestFinalizeQuiet(t *testing.T) {
	finalize(nil, false, true, true)
}

func TestFinalizeHealthy(t *testing.T) {
	finalize(nil, false, false, true)
}

func TestFinalizeUnhealthy(t *testing.T) {
	finalize([]statusRow{{Label: "x", Kind: "tcp", Port: 1}}, false, false, false)
}

func TestFinalizeJSON(t *testing.T) {
	finalize([]statusRow{{Label: "x", Kind: "tcp", Port: 1}}, true, false, true)
}

func TestCollectStatusRowsEmpty(t *testing.T) {
	got := collectStatusRows(&utils.CorgiCompose{})
	if got != nil {
		t.Errorf("got %v", got)
	}
}
