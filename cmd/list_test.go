package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrintExecPathPathOnly(t *testing.T) {
	printExecPath(utils.CorgiExecPath{Path: "/tmp/x"})
}

func TestPrintExecPathFull(t *testing.T) {
	printExecPath(utils.CorgiExecPath{Name: "n", Description: "d", Path: "/tmp/x"})
}

func TestPrintExecPathNameOnly(t *testing.T) {
	printExecPath(utils.CorgiExecPath{Name: "x", Path: "/tmp/x"})
}

func TestListRunCleanFlag(t *testing.T) {
	c := &cobra.Command{}
	cleanList = true
	t.Cleanup(func() { cleanList = false })
	listRun(c, nil)
}

func TestListRunNoEntries(t *testing.T) {
	prevPath := storageFilePathRef
	storageFilePathRef = filepath.Join(t.TempDir(), "exec_paths.txt")
	t.Cleanup(func() { storageFilePathRef = prevPath })

	c := &cobra.Command{}
	cleanList = false
	listRun(c, nil)
}

// Cannot directly access utils.storageFilePath from cmd package — test
// degraded path indirectly via clean.
var storageFilePathRef = ""

func TestListEntriesJSONShape(t *testing.T) {
	entries := toListEntries([]utils.CorgiExecPath{
		{Path: "/a/corgi-compose.yml", Name: "a", Description: "first"},
	})
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, entries)
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got[0]["path"] != "/a/corgi-compose.yml" {
		t.Errorf("path field missing/wrong: %v", got)
	}
	if got[0]["name"] != "a" {
		t.Errorf("name field missing/wrong: %v", got)
	}
	if got[0]["description"] != "first" {
		t.Errorf("description field missing/wrong: %v", got)
	}
}
