package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPreRunEValidGitProvider(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().String("gitProvider", "github", "")
	if err := preRunE(c, nil); err != nil {
		t.Errorf("github should be valid: %v", err)
	}

	c2 := &cobra.Command{}
	c2.Flags().String("gitProvider", "gitlab", "")
	if err := preRunE(c2, nil); err != nil {
		t.Errorf("gitlab should be valid: %v", err)
	}

	c3 := &cobra.Command{}
	c3.Flags().String("gitProvider", "", "")
	if err := preRunE(c3, nil); err != nil {
		t.Errorf("empty should be valid: %v", err)
	}
}

func TestPreRunEInvalidGitProvider(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().String("gitProvider", "bitbucket", "")
	err := preRunE(c, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid gitProvider") {
		t.Errorf("expected invalid gitProvider error, got %v", err)
	}
}

func TestPreRunECaseInsensitive(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().String("gitProvider", "GITHUB", "")
	if err := preRunE(c, nil); err != nil {
		t.Errorf("GITHUB should be valid: %v", err)
	}
}

func TestGetListOfServicesWithClonedFrom(t *testing.T) {
	services := []utils.Service{
		{ServiceName: "a", CloneFrom: "git@github.com:foo/a.git"},
		{ServiceName: "b"},
		{ServiceName: "c", CloneFrom: "https://x.git"},
	}
	got := getListOfServicesWithClonedFrom(services)
	if len(got) != 2 {
		t.Errorf("got %v, want 2", got)
	}
	if got[0] != "a" || got[1] != "c" {
		t.Errorf("got %v", got)
	}
}

func TestReadForkFlags(t *testing.T) {
	c := &cobra.Command{}
	c.Flags().Bool("all", true, "")
	c.Flags().Bool("private", true, "")
	c.Flags().Bool("useSameRepoName", true, "")
	c.Flags().String("gitProvider", "github", "")

	got, err := readForkFlags(c)
	if err != nil {
		t.Fatal(err)
	}
	if !got.all || !got.private || !got.useSameRepoName || got.gitProvider != "github" {
		t.Errorf("got %+v", got)
	}
}

func TestReadForkFlagsMissingFlag(t *testing.T) {
	c := &cobra.Command{}
	_, err := readForkFlags(c)
	if err == nil {
		t.Error("expected error for missing flag")
	}
}

func TestRunCommandToOutputBasic(t *testing.T) {
	var buf bytes.Buffer
	if err := runCommandToOutput(&buf, "", "echo", "hello"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("got %q", buf.String())
	}
}

func TestRunCommandToOutputUnknownCommand(t *testing.T) {
	err := runCommandToOutput(nil, "", "this-cmd-does-not-exist-123456")
	if err == nil {
		t.Error("expected error")
	}
}

func TestDetermineProviderExistingValue(t *testing.T) {
	got, err := determineProvider("github")
	if err != nil {
		t.Fatal(err)
	}
	if got != "github" {
		t.Errorf("got %q", got)
	}
}

func TestDetermineRepoNameUseSame(t *testing.T) {
	got, err := determineRepoName("api", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "api" {
		t.Errorf("got %q", got)
	}
}

func TestDeterminePrivacyForcePrivate(t *testing.T) {
	if !determinePrivacy(true) {
		t.Error("expected true")
	}
}

func TestCreateRepoForProviderUnknown(t *testing.T) {
	got, err := createRepoForProvider("unknown", "name", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q", got)
	}
}
