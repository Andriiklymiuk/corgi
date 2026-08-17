package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func withComposeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = orig })
	return dir
}

func providerCmd(t *testing.T, flag string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "init"}
	c.Flags().String("provider", "", "")
	if flag != "" {
		if err := c.Flags().Set("provider", flag); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestResolveCIProviderPrefersTheFlag(t *testing.T) {
	origin := gitOriginURL
	gitOriginURL = func() string { return "git@github.com:o/r.git" }
	t.Cleanup(func() { gitOriginURL = origin })

	got, err := resolveCIProvider(providerCmd(t, "gitlab"))
	if err != nil || got != "gitlab" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolveCIProviderReadsTheRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:o/r.git":          "github",
		"https://gitlab.com/g/p.git":      "gitlab",
		"https://gitlab.internal/g/p.git": "gitlab",
	}
	for remote, want := range cases {
		origin := gitOriginURL
		gitOriginURL = func() string { return remote }
		got, err := resolveCIProvider(providerCmd(t, ""))
		gitOriginURL = origin
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", remote, got, err)
		}
	}
}

// Writing a GitHub workflow into a GitLab project would be silently useless,
// so an unrecognised remote asks rather than guesses.
func TestResolveCIProviderRefusesToGuess(t *testing.T) {
	origin := gitOriginURL
	gitOriginURL = func() string { return "https://bitbucket.org/o/r.git" }
	t.Cleanup(func() { gitOriginURL = origin })

	if _, err := resolveCIProvider(providerCmd(t, "")); err == nil {
		t.Error("expected an error for an unknown forge")
	}
}

func TestResolveCIProviderRejectsAnUnknownFlag(t *testing.T) {
	if _, err := resolveCIProvider(providerCmd(t, "bitbucket")); err == nil {
		t.Error("expected an error for an unsupported provider")
	}
}

func TestCIInitWritesTheGitHubWorkflow(t *testing.T) {
	dir := withComposeDir(t)
	written, err := writeCIFiles("github", &utils.CorgiCompose{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("expected one file, got %v", written)
	}
	assertParsesAsYAML(t, filepath.Join(dir, written[0]))
}

// GitLab needs the committed cache plan alongside the pipeline, because it
// cannot read the plan at runtime.
func TestCIInitWritesThePipelineAndTheCachePlan(t *testing.T) {
	dir := withComposeDir(t)
	written, err := writeCIFiles("gitlab", &utils.CorgiCompose{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("expected the pipeline and the cache plan, got %v", written)
	}
	for _, name := range written {
		assertParsesAsYAML(t, filepath.Join(dir, name))
	}
	cache, err := os.ReadFile(filepath.Join(dir, ".gitlab", "corgi-cache.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(cache), ".corgi-cache:") {
		t.Errorf("expected the generated cache template:\n%s", cache)
	}
}

// Overwriting a pipeline someone has been editing is not recoverable from the
// CLI, so it takes an explicit flag.
func TestCIInitRefusesToOverwrite(t *testing.T) {
	withComposeDir(t)
	if _, err := writeCIFiles("github", &utils.CorgiCompose{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := writeCIFiles("github", &utils.CorgiCompose{}, false); err == nil {
		t.Fatal("expected a refusal on the second write")
	}
	if _, err := writeCIFiles("github", &utils.CorgiCompose{}, true); err != nil {
		t.Errorf("--force must overwrite, got %v", err)
	}
}

// A compose with no e2e: block would fail on the `corgi test --e2e` step, and
// the generated pipeline should say so rather than let it be discovered in CI.
func TestCINextStepsCallsOutAMissingE2EBlock(t *testing.T) {
	if !contains(ciNextSteps("github", &utils.CorgiCompose{}), "e2e:") {
		t.Error("expected the missing e2e block to be mentioned")
	}
}

func assertParsesAsYAML(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s does not parse: %v", path, err)
	}
}

// Refusing halfway would leave .gitlab-ci.yml behind without the cache plan it
// includes, which is a broken pipeline rather than a refused one.
func TestCIInitWritesNothingWhenOneFileExists(t *testing.T) {
	dir := withComposeDir(t)
	if err := writeGeneratedFile(filepath.Join(dir, ".gitlab", "corgi-cache.yml"), "old\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := writeCIFiles("gitlab", &utils.CorgiCompose{}, false); err == nil {
		t.Fatal("expected a refusal")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitlab-ci.yml")); err == nil {
		t.Error(".gitlab-ci.yml must not be written when the run is refused")
	}
	existing, err := os.ReadFile(filepath.Join(dir, ".gitlab", "corgi-cache.yml"))
	if err != nil || string(existing) != "old\n" {
		t.Errorf("the existing file must be untouched, got %q / %v", existing, err)
	}
}
