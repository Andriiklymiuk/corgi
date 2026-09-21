package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, name, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexSkillsInstallCopiesEverySkillAndSharedAndOwnsOnlyItsOwn(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writeSkill(t, src, "stories", map[string]string{"SKILL.md": "# stories\nread ../_shared/conventions.md", "references/a.md": "a"})
	writeSkill(t, src, "review", map[string]string{"SKILL.md": "# review"})
	writeSkill(t, src, "_shared", map[string]string{"conventions.md": "never ask"})
	writeSkill(t, dst, "imagegen", map[string]string{"SKILL.md": "someone else's"})

	rep, err := syncCodexSkills(src, dst, "2.30.9")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Added) != 3 || len(rep.Updated) != 0 || len(rep.Removed) != 0 {
		t.Fatalf("first install adds all three: %+v", rep)
	}
	for _, p := range []string{"stories/SKILL.md", "stories/references/a.md", "review/SKILL.md", "_shared/conventions.md", "imagegen/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Fatalf("%s missing after install", p)
		}
	}
	m, ok := readCodexSkillsManifest(dst)
	if !ok || m.Version != "2.30.9" || len(m.Skills) != 3 || strings.Contains(strings.Join(m.Skills, ","), "imagegen") {
		t.Fatalf("the manifest lists what corgi owns, nothing else: %+v", m)
	}

	os.WriteFile(filepath.Join(src, "stories", "SKILL.md"), []byte("# stories v2"), 0o644)
	os.RemoveAll(filepath.Join(src, "review"))
	os.WriteFile(filepath.Join(dst, "stories", "stale.md"), []byte("left over"), 0o644)
	rep, err = syncCodexSkills(src, dst, "2.30.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Updated) != 1 || rep.Updated[0] != "stories" || len(rep.Removed) != 1 || rep.Removed[0] != "review" || len(rep.Added) != 0 {
		t.Fatalf("a changed skill updates, a gone one goes: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dst, "stories", "stale.md")); err == nil {
		t.Fatal("an update replaces the skill directory whole, no leftovers")
	}
	if _, err := os.Stat(filepath.Join(dst, "imagegen", "SKILL.md")); err != nil {
		t.Fatal("a skill corgi does not own is never touched")
	}
	rep, _ = syncCodexSkills(src, dst, "2.30.10")
	if len(rep.Added)+len(rep.Updated)+len(rep.Removed) != 0 {
		t.Fatalf("a second run changes nothing: %+v", rep)
	}
	if stale := codexSkillsStale(src, dst); len(stale) != 0 {
		t.Fatalf("in sync means nothing stale: %v", stale)
	}
	os.WriteFile(filepath.Join(src, "_shared", "conventions.md"), []byte("ask once"), 0o644)
	if stale := codexSkillsStale(src, dst); len(stale) != 1 || stale[0] != "_shared" {
		t.Fatalf("a source edit shows as stale: %v", stale)
	}
}
