package idea

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newHomeRig(t *testing.T) (agentDir, workspace string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "agent"), t.TempDir()
}

func TestAPageKeptInAWorkspaceLivesInItsCorgiFolderAndStillAnswersBySlug(t *testing.T) {
	agent, ws := newHomeRig(t)
	now := time.Now()
	made, err := Create(agent, "", "Usage limits", "# Usage limits\n", Meta{Workspace: "api", Source: "/Users/someone/notes.md"}, now)
	if err != nil {
		t.Fatal(err)
	}
	key := made.Meta.Key
	target, err := MoveToWorkspace(agent, made.Meta.Slug, ws)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(ws, ".corgi", "ideas", "usage-limits"); target != want {
		t.Fatalf("kept in %s, want %s", target, want)
	}
	raw, err := os.ReadFile(filepath.Join(target, MetaName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) || strings.Contains(string(raw), "someone") {
		t.Fatalf("the share key or a local path went into the repository: %s", raw)
	}
	got, err := Load(agent, "usage-limits")
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Key != key || got.Meta.Workspace != "api" || got.Meta.Home != filepath.Join(".corgi", "ideas", "usage-limits") || got.Doc != "# Usage limits\n" {
		t.Fatalf("loaded %+v", got.Meta)
	}
	if _, err := got.AddComment(KindComment, "ana", "use postgres", nil, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := got.Save(agent); err != nil {
		t.Fatal(err)
	}
	comments, _ := os.ReadFile(filepath.Join(target, CommentsName))
	meta, _ := os.ReadFile(filepath.Join(target, MetaName))
	if !strings.Contains(string(comments), "use postgres") || strings.Contains(string(meta), key) || strings.Contains(string(meta), "home") || strings.Contains(string(meta), `"workspace"`) {
		t.Fatalf("after a save: comments %s meta %s", comments, meta)
	}
	if list := List(agent); len(list) != 1 || list[0].Slug != "usage-limits" || list[0].Key != key {
		t.Fatalf("list %+v", list)
	}
	if again, err := MoveToWorkspace(agent, "usage-limits", ws); err != nil || again != target {
		t.Fatalf("a second move must change nothing: %s %v", again, err)
	}
}

func TestAPageMovesBetweenWorkspacesAndBackHomeWithItsKey(t *testing.T) {
	agent, first := newHomeRig(t)
	second := t.TempDir()
	made, _ := Create(agent, "", "Banner", "text\n", Meta{}, time.Now())
	key := made.Meta.Key
	if _, err := MoveToWorkspace(agent, "banner", first); err != nil {
		t.Fatal(err)
	}
	moved, err := MoveToWorkspace(agent, "banner", second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, ".corgi", "ideas", "banner")); !os.IsNotExist(err) {
		t.Fatal("the first workspace still holds the page")
	}
	if got, err := Load(agent, "banner"); err != nil || got.Meta.Key != key || got.Doc != "text\n" {
		t.Fatalf("after the second move: %v", err)
	}
	if err := MoveHome(agent, "banner"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatal("the workspace still holds the page after it went home")
	}
	if _, linked := Home(agent, "banner"); linked {
		t.Fatal("still linked")
	}
	got, err := Load(agent, "banner")
	if err != nil || got.Meta.Key != key || got.Meta.Home != "" {
		t.Fatalf("home again: %+v %v", got, err)
	}
	var onDisk Meta
	raw, _ := os.ReadFile(filepath.Join(Dir(agent, "banner"), MetaName))
	if json.Unmarshal(raw, &onDisk) != nil || onDisk.Key != key {
		t.Fatal("the key did not come back into the page's own file")
	}
}

func TestPagesATeammateCommittedAreFoundOnceAndANameTakenGetsANumber(t *testing.T) {
	agent, ws := newHomeRig(t)
	if _, err := Create(agent, "", "Checkout", "mine\n", Meta{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"checkout", "referrals"} {
		dir := filepath.Join(WorkspaceRoot(ws), name)
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, MetaName), []byte(`{"slug":"`+name+`","title":"`+name+`","status":"draft","workspace":"someone-elses-secrets","auto":true,"buildWhenApproved":true,"tasks":[7],"plan":3,"key":"planted-key"}`), 0o644)
		_ = os.WriteFile(filepath.Join(dir, DocName), []byte("theirs\n"), 0o644)
	}
	_ = os.MkdirAll(filepath.Join(WorkspaceRoot(ws), "not a page"), 0o755)
	adopted := Adopt(agent, ws, "api")
	if len(adopted) != 2 || adopted[0] != "checkout-2" || adopted[1] != "referrals" {
		t.Fatalf("adopted %v", adopted)
	}
	if again := Adopt(agent, ws, "api"); len(again) != 0 {
		t.Fatalf("found twice: %v", again)
	}
	if mine, _ := Load(agent, "checkout"); mine == nil || mine.Doc != "mine\n" {
		t.Fatal("the page already here was touched")
	}
	theirs, err := Load(agent, "checkout-2")
	if err != nil || theirs.Doc != "theirs\n" {
		t.Fatalf("the teammate's page: %v", err)
	}
	m := theirs.Meta
	if m.Key != "" || m.Auto || m.BuildWhenApproved || len(m.Tasks) != 0 || m.Plan != 0 || m.Workspace != "api" {
		t.Fatalf("a page from the repository switched something on here: %+v", m)
	}
	for _, row := range List(agent) {
		if row.Slug != "checkout" && (row.Auto || row.BuildWhenApproved || row.Key != "" || row.Workspace != "api") {
			t.Fatalf("the list trusts the repository: %+v", row)
		}
	}
	if len(List(agent)) != 3 {
		t.Fatalf("list has %d", len(List(agent)))
	}
}

func TestRemovingAPageKeptInAWorkspaceRemovesItThereToo(t *testing.T) {
	agent, ws := newHomeRig(t)
	_, _ = Create(agent, "", "Gone", "x\n", Meta{}, time.Now())
	target, err := MoveToWorkspace(agent, "gone", ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := Remove(agent, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the folder in the workspace is still there")
	}
	if _, err := os.Lstat(Dir(agent, "gone")); !os.IsNotExist(err) {
		t.Fatal("the link is still there")
	}
	if _, err := os.Stat(localPath(agent, "gone")); !os.IsNotExist(err) {
		t.Fatal("the key file is still there")
	}
	if _, err := MoveToWorkspace(agent, "gone", ws); err == nil {
		t.Fatal("a page that is gone was moved")
	}
}

func TestAWorkspaceThatAlreadyHasThatFolderRefusesTheMove(t *testing.T) {
	agent, ws := newHomeRig(t)
	_, _ = Create(agent, "", "Taken", "x\n", Meta{}, time.Now())
	_ = os.MkdirAll(filepath.Join(WorkspaceRoot(ws), "taken"), 0o755)
	if _, err := MoveToWorkspace(agent, "taken", ws); err == nil {
		t.Fatal("moved onto a folder that was already there")
	}
	if got, err := Load(agent, "taken"); err != nil || got.Doc != "x\n" {
		t.Fatal("the page was lost by a refused move")
	}
}
