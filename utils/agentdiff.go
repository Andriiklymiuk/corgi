package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A stack's change spans several repositories, so the diff worth reading on a
// phone is one view across all of them. It needs no tunnel and no running
// stack, which is why it is the artifact that works on a train.

// maxPatchBytes caps one file's patch in the response. A generated lockfile can
// be megabytes, and one of those would break a phone client or blow the MCP
// response size for everything else in the same call.
const maxPatchBytes = 32 << 10

// FileDiff is one changed file.
type FileDiff struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
	New       bool   `json:"new,omitempty"`
	Patch     string `json:"patch,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// RepoDiff is one repository's changes against its merge base.
type RepoDiff struct {
	Service   string     `json:"service"`
	Repo      string     `json:"repo"`
	Branch    string     `json:"branch"`
	Base      string     `json:"base"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Files     []FileDiff `json:"files"`
	Error     string     `json:"error,omitempty"`
}

// StackDiff is the whole change, across every repository in the stack.
type StackDiff struct {
	Base      string     `json:"base"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Repos     []RepoDiff `json:"repos"`
}

// DiffStack collects the diff of every service's checkout against base.
//
// dirs maps a service name to the directory to diff, so it works against either
// the main checkouts or a materialized worktree set. base is a branch name such
// as "main"; each repo is compared against its own merge base with it.
func DiffStack(dirs map[string]string, base string, includePatch bool) *StackDiff {
	if strings.TrimSpace(base) == "" {
		base = "main"
	}
	out := &StackDiff{Base: base}

	services := make([]string, 0, len(dirs))
	for svc := range dirs {
		services = append(services, svc)
	}
	sort.Strings(services)

	for _, svc := range services {
		rd := diffRepo(svc, dirs[svc], base, includePatch)
		out.Repos = append(out.Repos, rd)
		out.Additions += rd.Additions
		out.Deletions += rd.Deletions
	}
	return out
}

func diffRepo(service, dir, base string, includePatch bool) RepoDiff {
	rd := RepoDiff{Service: service, Repo: dir, Base: base}
	if dir == "" || !isGitRepo(dir) {
		rd.Error = "not a git repository"
		return rd
	}
	if root, ok := repoRoot(dir); ok {
		rd.Repo = root
	}
	if branch, err := gitOut(dir, gitRevParse, gitAbbrevRef, "HEAD"); err == nil {
		rd.Branch = branch
	}

	ref := mergeBaseRef(dir, base)
	if ref == "" {
		rd.Error = fmt.Sprintf("no common history with %s", base)
		return rd
	}
	rd.Base = ref

	// Non-nil so a client can iterate the result without a null check.
	rd.Files = []FileDiff{}

	stats, err := gitOut(dir, "diff", "--numstat", ref)
	if err != nil {
		rd.Error = err.Error()
		return rd
	}
	for _, line := range strings.Split(stats, "\n") {
		f, ok := parseNumstatLine(line)
		if !ok {
			continue
		}
		if includePatch {
			f.Patch, f.Truncated = filePatch(dir, ref, f.Path)
		}
		rd.Files = append(rd.Files, f)
		rd.Additions += f.Additions
		rd.Deletions += f.Deletions
	}

	// An agent's first act is usually to create files, and `git diff` shows
	// nothing for an untracked one. Without this the most common change of all
	// would read as an empty diff.
	for _, f := range untrackedFiles(dir, includePatch) {
		rd.Files = append(rd.Files, f)
		rd.Additions += f.Additions
	}

	sort.Slice(rd.Files, func(i, j int) bool { return rd.Files[i].Path < rd.Files[j].Path })
	return rd
}

// untrackedFiles reports files git does not track yet, respecting .gitignore.
func untrackedFiles(dir string, includePatch bool) []FileDiff {
	out, err := gitOut(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var files []FileDiff
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		f := FileDiff{Path: path, New: true}
		content, rerr := os.ReadFile(filepath.Join(dir, path))
		if rerr != nil {
			continue
		}
		if isProbablyBinary(content) {
			f.Binary = true
			files = append(files, f)
			continue
		}
		f.Additions = countLines(content)
		if includePatch {
			f.Patch, f.Truncated = newFilePatch(path, content)
		}
		files = append(files, f)
	}
	return files
}

// newFilePatch renders an untracked file as a unified diff against nothing, so
// a client renders it the same way as every other entry.
func newFilePatch(path string, content []byte) (string, bool) {
	body := string(content)
	truncated := false
	if len(body) > maxPatchBytes {
		body = body[:maxPatchBytes]
		truncated = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n", path)
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		b.WriteString("+" + line + "\n")
	}
	if truncated {
		b.WriteString("… truncated, open the file to see the rest\n")
	}
	return b.String(), truncated
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := strings.Count(string(content), "\n")
	if !strings.HasSuffix(string(content), "\n") {
		n++
	}
	return n
}

// isProbablyBinary uses git's own heuristic: a NUL byte near the start.
func isProbablyBinary(content []byte) bool {
	limit := min(len(content), 8000)
	for i := range limit {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// mergeBaseRef resolves the commit to diff against: the merge base with base,
// falling back to origin/<base> when only the remote-tracking ref exists.
func mergeBaseRef(dir, base string) string {
	for _, candidate := range []string{base, "origin/" + base} {
		if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", candidate); err != nil {
			continue
		}
		if ref, err := gitOut(dir, "merge-base", "HEAD", candidate); err == nil && ref != "" {
			return ref
		}
	}
	return ""
}

// parseNumstatLine reads one `git diff --numstat` row. Binary files report "-"
// for both counts.
func parseNumstatLine(line string) (FileDiff, bool) {
	fields := strings.SplitN(strings.TrimSpace(line), "\t", 3)
	if len(fields) != 3 {
		return FileDiff{}, false
	}
	f := FileDiff{Path: fields[2]}
	if fields[0] == "-" || fields[1] == "-" {
		f.Binary = true
		return f, true
	}
	f.Additions, _ = strconv.Atoi(fields[0])
	f.Deletions, _ = strconv.Atoi(fields[1])
	return f, true
}

// filePatch returns one file's unified diff, truncated at maxPatchBytes.
func filePatch(dir, ref, path string) (patch string, truncated bool) {
	// `--` stops a path that looks like a flag from being read as one.
	out, err := gitOut(dir, "diff", ref, "--", path)
	if err != nil {
		return "", false
	}
	if len(out) <= maxPatchBytes {
		return out, false
	}
	return out[:maxPatchBytes] + "\n… truncated, open the file to see the rest", true
}

// ServiceDirs maps each service to the directory its code lives in, preferring
// a materialized worktree when one exists for the branch.
func ServiceDirs(corgi *CorgiCompose, set *WorktreeSet) map[string]string {
	dirs := map[string]string{}
	for i := range corgi.Services {
		svc := &corgi.Services[i]
		if svc.AbsolutePath != "" {
			dirs[svc.ServiceName] = svc.AbsolutePath
		}
	}
	if set == nil {
		return dirs
	}
	for _, w := range set.Worktrees {
		if w.Dir != "" {
			dirs[w.Service] = w.Dir
		}
	}
	return dirs
}

// ShortRepoName is the last path segment, for compact output.
func ShortRepoName(repo string) string { return filepath.Base(repo) }
