package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"andriiklymiuk/corgi/utils"
	"github.com/spf13/cobra"
)

var docsCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Docs that mention what the diff changed, and pointers that no longer land",
	Long: `Reads the stack's changed surface (exported symbols, routes, contracts,
migrations, config) against a base branch, then finds every Markdown file that
names one of them - those are the docs to re-read. Also checks the file:line
pointers in CLAUDE.md, AGENTS.md and .claude/rules against the tree.

  corgi docs check
  corgi docs check --base develop --json
  corgi docs check --branch feature/ABC-123/limits

Exit 1 when something needs a look, so it can gate a merge.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		base, _ := cmd.Flags().GetString("base")
		branch, _ := cmd.Flags().GetString("branch")
		composePath, _ := cmd.Flags().GetString("filename")
		corgi, dir, err := loadComposeForAgent(composePath)
		if err != nil {
			exitWithError("docs_check", err, 1)
		}
		out, err := mcpSurface(composePath, base, branch)
		if err != nil {
			exitWithError("docs_check", err, 1)
		}
		repos := out.(map[string]any)["repos"].([]utils.RepoSurface)
		roots := []string{dir}
		for _, d := range utils.ServiceDirs(corgi, nil) {
			roots = append(roots, d)
		}
		report := checkDocs(roots, repos)
		if utils.JSONOutput {
			utils.PrintJSON(report)
		} else {
			printDocsReport(report)
		}
		if len(report.Mentions) > 0 || len(report.StalePointers) > 0 {
			exitProcess(1)
		}
	},
}

type docMention struct {
	Doc  string `json:"doc"`
	Line int    `json:"line"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Op   string `json:"op"`
	Path string `json:"path"`
}

type stalePointer struct {
	Doc     string `json:"doc"`
	Line    int    `json:"line"`
	Pointer string `json:"pointer"`
	Why     string `json:"why"`
}

type docsReport struct {
	Mentions      []docMention   `json:"mentions"`
	StalePointers []stalePointer `json:"stalePointers"`
	DocsScanned   int            `json:"docsScanned"`
}

var pointerRe = regexp.MustCompile(`\b([A-Za-z0-9_./-]+\.(?:go|ts|tsx|js|py|rb|rs|java|kt|swift|sql|ya?ml|md)):(\d+)\b`)

func surfaceName(c utils.SurfaceChange) string {
	switch c.Kind {
	case "config", "migration":
		return ""
	case "route":
		if i := strings.Index(c.Name, " "); i > 0 {
			return c.Name[i+1:]
		}
	}
	return c.Name
}

func surfaceNames(repos []utils.RepoSurface) map[string]utils.SurfaceChange {
	names := map[string]utils.SurfaceChange{}
	for _, r := range repos {
		for _, c := range r.Changes {
			if name := surfaceName(c); len(name) >= 4 {
				names[name] = c
			}
		}
	}
	return names
}

var skippedDocDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".next": true, "corgi_services": true, ".corgi": true}

func checkDocs(roots []string, repos []utils.RepoSurface) docsReport {
	var report docsReport
	names := surfaceNames(repos)
	seen := map[string]bool{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			switch {
			case err != nil:
				return nil
			case d.IsDir() && skippedDocDirs[d.Name()]:
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(d.Name(), ".md") || seen[path]:
				return nil
			}
			seen[path] = true
			report.DocsScanned++
			scanDoc(path, filepath.Dir(path), names, &report)
			return nil
		})
	}
	sort.Slice(report.Mentions, func(i, j int) bool {
		if report.Mentions[i].Doc != report.Mentions[j].Doc {
			return report.Mentions[i].Doc < report.Mentions[j].Doc
		}
		return report.Mentions[i].Line < report.Mentions[j].Line
	})
	return report
}

func scanDoc(path, dir string, names map[string]utils.SurfaceChange, report *docsReport) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	base := filepath.Base(path)
	pointerDoc := base == claudeMemoryFile || base == "AGENTS.md" || strings.Contains(filepath.ToSlash(path), "/.claude/rules/")
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		for name, c := range names {
			if strings.Contains(line, name) && wordBounded(line, name) {
				report.Mentions = append(report.Mentions, docMention{Doc: path, Line: n, Name: c.Name, Kind: c.Kind, Op: c.Op, Path: c.Path})
			}
		}
		if pointerDoc {
			report.StalePointers = append(report.StalePointers, stalePointersIn(line, path, dir, n)...)
		}
	}
}

func stalePointersIn(line, path, dir string, n int) []stalePointer {
	var out []stalePointer
	for _, m := range pointerRe.FindAllStringSubmatch(line, -1) {
		want, _ := strconv.Atoi(m[2])
		abs := m[1]
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, m[1])
		}
		total, err := countFileLines(abs)
		switch {
		case err != nil:
			out = append(out, stalePointer{Doc: path, Line: n, Pointer: m[0], Why: "no such file"})
		case want > total:
			out = append(out, stalePointer{Doc: path, Line: n, Pointer: m[0], Why: fmt.Sprintf("the file has %d lines", total)})
		}
	}
	return out
}

func wordBounded(line, name string) bool {
	i := strings.Index(line, name)
	for i >= 0 {
		before := i == 0 || !isWordByte(line[i-1])
		after := i+len(name) >= len(line) || !isWordByte(line[i+len(name)])
		if before && after {
			return true
		}
		j := strings.Index(line[i+1:], name)
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		n++
	}
	return n, nil
}

func printDocsReport(r docsReport) {
	if len(r.Mentions) == 0 && len(r.StalePointers) == 0 {
		fmt.Printf("✓ %d docs scanned: none name what changed, every pointer lands\n", r.DocsScanned)
		return
	}
	if len(r.Mentions) > 0 {
		fmt.Printf("Docs that name what the diff changed (%d) - re-read these:\n", len(r.Mentions))
		for _, m := range r.Mentions {
			fmt.Printf("  %s:%d  `%s` (%s %s in %s)\n", m.Doc, m.Line, m.Name, m.Op, m.Kind, m.Path)
		}
	}
	if len(r.StalePointers) > 0 {
		fmt.Printf("Pointers that no longer land (%d):\n", len(r.StalePointers))
		for _, p := range r.StalePointers {
			fmt.Printf("  %s:%d  %s - %s\n", p.Doc, p.Line, p.Pointer, p.Why)
		}
	}
}

func init() {
	docsCheckCmd.Flags().String("base", "", "base branch (default: main)")
	docsCheckCmd.Flags().String("branch", "", "check the existing worktrees of this branch instead of the checkouts")
	docsCmd.AddCommand(docsCheckCmd)
}
