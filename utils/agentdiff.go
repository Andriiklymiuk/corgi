package utils

import (
	"bytes"
	"fmt"
	"io"
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
	// RenamedFrom is the previous path when git detected a rename.
	RenamedFrom string `json:"renamedFrom,omitempty"`
	Patch       string `json:"patch,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
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
	// AlsoServing names other services backed by this same repository, so a
	// shared repo is reported once rather than counted twice.
	AlsoServing []string `json:"alsoServing,omitempty"`
	Error       string   `json:"error,omitempty"`
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

	// Two services can share a repository — including from different
	// subdirectories of it. Keying on the raw service directory missed that and
	// diffed the repo twice, listing every change twice and doubling the stack
	// totals, so the key is the resolved git root.
	byRoot := map[string]int{}
	for _, svc := range services {
		key := dirs[svc]
		if root, ok := repoRoot(key); ok {
			key = root
		}
		if i, seen := byRoot[key]; seen {
			out.Repos[i].AlsoServing = append(out.Repos[i].AlsoServing, svc)
			continue
		}
		rd := diffRepo(svc, dirs[svc], base, includePatch)
		byRoot[key] = len(out.Repos)
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
	// Every git call runs from the repository root. A service can live in a
	// subdirectory, and `git diff --numstat` reports paths relative to the
	// root — so running from the service directory made every pathspec miss
	// and returned an empty patch for every file. It also silently limited the
	// diff to that subtree.
	if root, ok := repoRoot(dir); ok {
		rd.Repo = root
		dir = root
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

	// -z, because without it a rename is reported as the single path
	// "old => new", which is then neither a usable display name nor a pathspec
	// that matches anything — every renamed file came back with an empty patch.
	stats, err := gitOut(dir, "diff", "--numstat", "-z", ref)
	if err != nil {
		rd.Error = err.Error()
		return rd
	}
	for _, f := range parseNumstatZ(stats) {
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
	for _, f := range untrackedFiles(rd.Repo, includePatch) {
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
		content, lines, truncated, rerr := readForDiff(filepath.Join(dir, path))
		if rerr != nil {
			continue
		}
		if isProbablyBinary(content) {
			f.Binary = true
			files = append(files, f)
			continue
		}
		f.Additions = lines
		if includePatch {
			f.Patch, f.Truncated = newFilePatch(path, content, truncated)
		}
		files = append(files, f)
	}
	return files
}

// readForDiff streams a file, keeping only the first maxPatchBytes for the
// patch while counting every line. A stray multi-gigabyte file is then bounded
// in memory, and the reported line count is still the real one — a truncated
// patch that also under-reports its size would be misleading twice over.
func readForDiff(path string) (head []byte, lines int, truncated bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()

	buf := make([]byte, 32<<10)
	var total int
	sawTrailingNewline := false
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			total += n
			lines += bytes.Count(chunk, []byte{'\n'})
			sawTrailingNewline = chunk[n-1] == '\n'
			if len(head) < maxPatchBytes {
				head = append(head, chunk[:min(n, maxPatchBytes-len(head))]...)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, false, readErr
		}
	}
	if total > 0 && !sawTrailingNewline {
		lines++ // a final line without a newline still counts
	}
	return head, lines, total > maxPatchBytes, nil
}

// newFilePatch renders an untracked file as a unified diff against nothing, so
// a client renders it the same way as every other entry.
func newFilePatch(path string, content []byte, truncated bool) (string, bool) {
	body := string(content)
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

// parseNumstatZ reads `git diff --numstat -z` output.
//
// Records are NUL-separated. An ordinary change is "adds\tdels\tpath\0"; a
// rename drops the path from that field and follows with two more records, the
// old path then the new one. Binary files report "-" for both counts.
func parseNumstatZ(out string) []FileDiff {
	records := strings.Split(out, "\x00")
	var files []FileDiff

	for i := 0; i < len(records); i++ {
		rec := records[i]
		if strings.TrimSpace(rec) == "" {
			continue
		}
		fields := strings.SplitN(rec, "\t", 3)
		if len(fields) < 3 {
			continue
		}
		f := FileDiff{Path: fields[2]}
		if f.Path == "" {
			// A rename: the next two records are the old and new paths. The new
			// one is what the change is about and what a pathspec matches.
			if i+2 < len(records) {
				f.Path = records[i+2]
				f.RenamedFrom = records[i+1]
				i += 2
			} else {
				continue
			}
		}
		if fields[0] == "-" || fields[1] == "-" {
			f.Binary = true
			files = append(files, f)
			continue
		}
		f.Additions, _ = strconv.Atoi(fields[0])
		f.Deletions, _ = strconv.Atoi(fields[1])
		files = append(files, f)
	}
	return files
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

// WorktreeDirs maps only the services a branch was actually materialized for.
// Used when diffing a branch, so a partial materialize does not drag every
// other service's main checkout into the result.
func WorktreeDirs(set *WorktreeSet) map[string]string {
	dirs := map[string]string{}
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
