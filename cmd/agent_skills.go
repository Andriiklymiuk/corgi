package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

const codexSkillsManifestName = ".corgi-skills.json"

type codexSkillsManifest struct {
	Version string            `json:"version"`
	Source  string            `json:"source"`
	Skills  []string          `json:"skills"`
	Hashes  map[string]string `json:"hashes"`
}

type codexSkillsReport struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
	Skills  int      `json:"skills"`
	Dir     string   `json:"dir"`
	Source  string   `json:"source"`
}

var agentSkillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "The corgi skills for agents other than Claude: install and check them for Codex",
	Long: `Claude reads the corgi skills from the plugin (/plugin install corgi@corgi).
Codex reads skills from ~/.codex/skills/<name>/SKILL.md and has no plugin, so
corgi copies them there: every skill, the _shared files they read, one
manifest naming what corgi owns. Skills you installed yourself are never
touched; a skill corgi dropped is removed. Run it again after an upgrade -
corgi agent doctor says when the copy is behind.

  corgi agent skills install               # into ~/.codex/skills ($CODEX_HOME/skills when set)
  corgi agent skills install --from <dir>  # from a checkout instead of the installed plugin
  corgi agent skills status`,
}

var agentSkillsInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Copy the corgi skills into Codex's skills directory",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		from, _ := cmd.Flags().GetString("from")
		src, err := corgiSkillsSource(from)
		if err != nil {
			exitWithError("agent_skills", err, 2)
		}
		dst := codexSkillsDir()
		if dst == "" {
			exitWithError("agent_skills", fmt.Errorf("no home directory"), 1)
		}
		rep, err := syncCodexSkills(src, dst, APP_VERSION)
		if err != nil {
			exitWithError("agent_skills", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(rep)
			return
		}
		fmt.Printf("%d corgi skills in %s (from %s)\n", rep.Skills, rep.Dir, rep.Source)
		for _, n := range rep.Added {
			fmt.Printf("  + %s\n", n)
		}
		for _, n := range rep.Updated {
			fmt.Printf("  ~ %s\n", n)
		}
		for _, n := range rep.Removed {
			fmt.Printf("  - %s\n", n)
		}
		if len(rep.Added)+len(rep.Updated)+len(rep.Removed) == 0 {
			fmt.Println("  already current")
		}
		if !codexInstalled() {
			fmt.Println("codex is not on PATH; the skills wait there until it is")
		}
	},
}

var agentSkillsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "What Codex has, and whether it is behind the plugin",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dst := codexSkillsDir()
		m, ok := readCodexSkillsManifest(dst)
		src, srcErr := corgiSkillsSource("")
		var stale []string
		if ok && srcErr == nil {
			stale = codexSkillsStale(src, dst)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"dir": dst, "installed": ok, "version": m.Version, "skills": m.Skills, "source": src, "stale": stale})
			return
		}
		if !ok {
			fmt.Printf("no corgi skills in %s - corgi agent skills install\n", dst)
			return
		}
		fmt.Printf("%d corgi skills in %s, installed by corgi %s\n", len(m.Skills), dst, m.Version)
		switch {
		case srcErr != nil:
			fmt.Printf("cannot compare: %v\n", srcErr)
		case len(stale) > 0:
			fmt.Printf("behind the plugin: %s - corgi agent skills install\n", strings.Join(stale, ", "))
		default:
			fmt.Println("current")
		}
	},
}

func codexSkillsDir() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "skills")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "skills")
}

// corgiSkillsSource is where the plugin's skills live on this machine: an
// explicit directory, else the Claude marketplace checkout, else its newest cache.
func corgiSkillsSource(from string) (string, error) {
	if from = strings.TrimSpace(from); from != "" {
		if _, err := os.Stat(filepath.Join(from, "_shared")); err != nil {
			if _, err := os.Stat(filepath.Join(from, "plugins", "corgi", "skills")); err == nil {
				from = filepath.Join(from, "plugins", "corgi", "skills")
			}
		}
		if _, err := os.Stat(from); err != nil {
			return "", fmt.Errorf("--from %s: %w", from, err)
		}
		if abs, err := filepath.Abs(from); err == nil {
			from = abs
		}
		return from, nil
	}
	if env := strings.TrimSpace(os.Getenv("CORGI_SKILLS_DIR")); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	market := filepath.Join(home, ".claude", "plugins", "marketplaces", "corgi", "plugins", "corgi", "skills")
	if _, err := os.Stat(market); err == nil {
		return market, nil
	}
	cache := filepath.Join(home, ".claude", "plugins", "cache", "corgi", "corgi")
	entries, _ := os.ReadDir(cache)
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(cache, e.Name(), "skills")); err == nil {
				versions = append(versions, e.Name())
			}
		}
	}
	if len(versions) > 0 {
		sort.Slice(versions, func(i, j int) bool { return versionLess(versions[j], versions[i]) })
		return filepath.Join(cache, versions[0], "skills"), nil
	}
	return "", fmt.Errorf("the corgi plugin is not installed for Claude, so there is nothing to copy: in `claude`, /plugin marketplace add Andriiklymiuk/corgi, then /plugin install corgi@corgi; or --from <checkout of the corgi repo>")
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var x, y int
		fmt.Sscanf(pa[i], "%d", &x)
		fmt.Sscanf(pb[i], "%d", &y)
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

func skillDirs(src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == "_shared" {
			names = append(names, e.Name())
			continue
		}
		if _, err := os.Stat(filepath.Join(src, e.Name(), "SKILL.md")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func hashDir(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func readCodexSkillsManifest(dst string) (codexSkillsManifest, bool) {
	var m codexSkillsManifest
	data, err := os.ReadFile(filepath.Join(dst, codexSkillsManifestName))
	if err != nil || json.Unmarshal(data, &m) != nil {
		return codexSkillsManifest{}, false
	}
	return m, true
}

func syncCodexSkills(src, dst, version string) (codexSkillsReport, error) {
	rep := codexSkillsReport{Dir: dst, Source: src}
	names, err := skillDirs(src)
	if err != nil {
		return rep, err
	}
	if len(names) == 0 {
		return rep, fmt.Errorf("no skills in %s", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return rep, err
	}
	old, _ := readCodexSkillsManifest(dst)
	next := codexSkillsManifest{Version: version, Source: src, Hashes: map[string]string{}}
	for _, name := range names {
		sum, err := hashDir(filepath.Join(src, name))
		if err != nil {
			return rep, err
		}
		next.Hashes[name] = sum
		next.Skills = append(next.Skills, name)
		target := filepath.Join(dst, name)
		_, present := os.Stat(target)
		if old.Hashes[name] == sum && present == nil {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return rep, err
		}
		if err := copyDir(filepath.Join(src, name), target); err != nil {
			return rep, err
		}
		if old.Hashes[name] == "" {
			rep.Added = append(rep.Added, name)
		} else {
			rep.Updated = append(rep.Updated, name)
		}
	}
	for _, name := range old.Skills {
		if _, keep := next.Hashes[name]; keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dst, name)); err != nil {
			return rep, err
		}
		rep.Removed = append(rep.Removed, name)
	}
	data, _ := json.MarshalIndent(next, "", "  ")
	if err := os.WriteFile(filepath.Join(dst, codexSkillsManifestName), data, 0o644); err != nil {
		return rep, err
	}
	rep.Skills = len(names)
	return rep, nil
}

func codexSkillsStale(src, dst string) []string {
	m, ok := readCodexSkillsManifest(dst)
	if !ok {
		return []string{"all"}
	}
	names, err := skillDirs(src)
	if err != nil {
		return nil
	}
	var stale []string
	for _, name := range names {
		sum, err := hashDir(filepath.Join(src, name))
		if err != nil || m.Hashes[name] != sum {
			stale = append(stale, name)
		}
	}
	for _, name := range m.Skills {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			stale = append(stale, name)
		}
	}
	return stale
}

func init() {
	agentSkillsInstallCmd.Flags().String("from", "", "Copy from this directory (a corgi checkout, or its plugins/corgi/skills) instead of the installed plugin")
	agentSkillsCmd.AddCommand(agentSkillsInstallCmd, agentSkillsStatusCmd)
	agentCmd.AddCommand(agentSkillsCmd)
}
