package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func newIdeaHomeRig(t *testing.T) (dir, stack string) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	dir = filepath.Join(data, "agent")
	stack = t.TempDir()
	registry := &workspace.Registry{}
	registry.Upsert(workspace.Workspace{ID: "api", AbsPath: stack})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}
	ideaAdopted.Lock()
	ideaAdopted.at = nil
	ideaAdopted.Unlock()
	return dir, stack
}

func TestAPageGoesIntoAWorkspaceWhenAskedOrWhenTheWorkspaceAlreadyKeepsPages(t *testing.T) {
	_, stack := newIdeaHomeRig(t)
	if _, ok := ideaGoesIntoWorkspace("api", false); ok {
		t.Fatal("a workspace nobody named got a new folder in its repository")
	}
	if root, ok := ideaGoesIntoWorkspace("api", true); !ok || root != stack {
		t.Fatalf("asked for: %q %v", root, ok)
	}
	if _, ok := ideaGoesIntoWorkspace("no-such-workspace", true); ok {
		t.Fatal("an unknown workspace was accepted")
	}
	if _, ok := ideaGoesIntoWorkspace("", true); ok {
		t.Fatal("no workspace was accepted")
	}
	if err := os.MkdirAll(idea.WorkspaceRoot(stack), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := ideaGoesIntoWorkspace("api", false); !ok {
		t.Fatal("a workspace that already keeps pages did not get the new one")
	}
}

func TestGivingAPageAWorkspaceMovesItThereAndItStillBuildsThere(t *testing.T) {
	dir, stack := newIdeaHomeRig(t)
	made, err := idea.Create(dir, "", "Usage limits", "# Usage limits\n", idea.Meta{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := setIdea(dir, made.Meta.Slug, ideaChanges{Workspace: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Workspace != "api" || got.Meta.Home == "" || got.Meta.Key != made.Meta.Key {
		t.Fatalf("after set: %+v", got.Meta)
	}
	if _, err := os.Stat(filepath.Join(stack, ".corgi", "ideas", "usage-limits", idea.DocName)); err != nil {
		t.Fatalf("the page is not in the workspace: %v", err)
	}
	if kept := whereIdeaIsKept(got.Meta); kept != "api · "+filepath.Join(".corgi", "ideas", "usage-limits") {
		t.Fatalf("kept line: %q", kept)
	}
	if kept := whereIdeaIsKept(idea.Meta{}); kept != "on this machine" {
		t.Fatalf("kept line: %q", kept)
	}
	if _, err := setIdea(dir, made.Meta.Slug, ideaChanges{Workspace: "no-such-workspace"}); err == nil {
		t.Fatal("moved to a workspace that does not exist")
	}
}

func TestTheListFindsPagesATeammateCommittedToARegisteredWorkspace(t *testing.T) {
	dir, stack := newIdeaHomeRig(t)
	theirs := filepath.Join(idea.WorkspaceRoot(stack), "referrals")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(theirs, idea.MetaName), []byte(`{"slug":"referrals","title":"Referral codes","status":"draft","workspace":"api"}`), 0o644)
	_ = os.WriteFile(filepath.Join(theirs, idea.DocName), []byte("# Referral codes\n"), 0o644)
	list := listIdeas(dir)
	if len(list) != 1 || list[0].Slug != "referrals" || list[0].Home == "" || list[0].Workspace != "api" {
		t.Fatalf("list: %+v", list)
	}
	if id, err := idea.Load(dir, "referrals"); err != nil || id.Doc != "# Referral codes\n" {
		t.Fatalf("load: %v", err)
	}
}
