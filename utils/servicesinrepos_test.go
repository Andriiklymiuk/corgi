package utils

import (
	"path/filepath"
	"testing"
)

func TestPullRequestProjectReadsGitHubAndGitLabRefs(t *testing.T) {
	for ref, want := range map[string]string{
		"acme/api#583":             "acme/api",
		"group/sub/core/core!9384": "group/sub/core/core",
		"Acme/Web#12":              "acme/web",
		"ABC-123":                  "",
		"api#12":                   "",
		"acme/api#":                "",
		"acme/api#12a":             "",
		"routine/digest":           "",
		"":                         "",
	} {
		if got := PullRequestProject(ref); got != want {
			t.Errorf("%q: %q, want %q", ref, got, want)
		}
	}
}

func TestRemoteProjectStripsHostAndSuffix(t *testing.T) {
	for url, want := range map[string]string{
		"git@gitlab.com:group/sub/core/core.git": "group/sub/core/core",
		"https://github.com/acme/api.git":        "acme/api",
		"https://github.com/Acme/API":            "acme/api",
		"ssh://git@gitlab.example.com/a/b.git":   "a/b",
		"":                                       "",
	} {
		if got := RemoteProject(url); got != want {
			t.Errorf("%q: %q, want %q", url, got, want)
		}
	}
}

func TestServicesInReposPicksOnlyTheRepoThePullRequestIsIn(t *testing.T) {
	dir := t.TempDir()
	api := newRepo(t, filepath.Join(dir, "api"))
	web := newRepo(t, filepath.Join(dir, "web"))
	if _, err := gitOut(api, "remote", "add", "origin", "git@github.com:acme/api.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(web, "remote", "add", "origin", "https://github.com/acme/web.git"); err != nil {
		t.Fatal(err)
	}
	corgi := &CorgiCompose{Services: []Service{
		{ServiceName: "api", AbsolutePath: api},
		{ServiceName: "worker", AbsolutePath: api},
		{ServiceName: "web", AbsolutePath: web},
		{ServiceName: "gone", AbsolutePath: filepath.Join(dir, "gone")},
	}}
	got := ServicesInRepos(corgi, []string{"acme/api"})
	if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
		t.Fatalf("got %v, want api and worker", got)
	}
	if got := ServicesInRepos(corgi, []string{"acme/other"}); got != nil {
		t.Fatalf("no match: %v", got)
	}
	if got := ServicesInRepos(corgi, nil); got != nil {
		t.Fatalf("no projects: %v", got)
	}
}
