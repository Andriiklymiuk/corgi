package utils

import (
	"testing"

	"github.com/spf13/cobra"
)

func newCobraWithRootFlags() *cobra.Command {
	c := &cobra.Command{}
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		c.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		c.Flags().Bool(f, false, "")
	}
	return c
}

func TestResolveTemplatePathNoFlags(t *testing.T) {
	c := newCobraWithRootFlags()
	got, handled, err := resolveTemplatePath(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Errorf("expected not handled, got %v", got)
	}
}

func TestResolveTemplatePathMissingFlag(t *testing.T) {
	c := &cobra.Command{}
	_, handled, err := resolveTemplatePath(c, "")
	if !handled || err == nil {
		t.Errorf("expected handled+err: handled=%v err=%v", handled, err)
	}
}

func TestDescribeServiceInfo(t *testing.T) {
	describeServiceInfo(map[string]int{"a": 1})
}

func TestCleanFromScratchDisabled(t *testing.T) {
	c := newCobraWithRootFlags()
	c.Flags().Set("fromScratch", "false")
	CleanFromScratch(c, CorgiCompose{})
}

func TestCleanFromScratchMissingFlag(t *testing.T) {
	CleanFromScratch(&cobra.Command{}, CorgiCompose{})
}

func TestResolveGlobalPathEmpty(t *testing.T) {
	prev := storageFilePath
	storageFilePath = "/no/such/zzz.txt"
	t.Cleanup(func() { storageFilePath = prev })
	_, err := resolveGlobalPath()
	if err == nil {
		t.Error("expected err")
	}
}
