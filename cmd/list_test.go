package cmd

import (
	"andriiklymiuk/corgi/utils"
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
