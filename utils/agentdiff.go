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

const maxPatchBytes = 32 << 10

const maxStackPatchBytes = 1 << 20

type FileDiff struct {
	Path        string `json:"path"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	Binary      bool   `json:"binary,omitempty"`
	New         bool   `json:"new,omitempty"`
	RenamedFrom string `json:"renamedFrom,omitempty"`
	Patch       string `json:"patch,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

type RepoDiff struct {
	Service     string     `json:"service"`
	Repo        string     `json:"repo"`
	Branch      string     `json:"branch"`
	Base        string     `json:"base"`
	Additions   int        `json:"additions"`
	Deletions   int        `json:"deletions"`
	Files       []FileDiff `json:"files"`
	AlsoServing []string   `json:"alsoServing,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type StackDiff struct {
	Base             string     `json:"base"`
	Additions        int        `json:"additions"`
	Deletions        int        `json:"deletions"`
	Repos            []RepoDiff `json:"repos"`
	PatchesTruncated bool       `json:"patchesTruncated,omitempty"`
}

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

	byRoot := map[string]int{}
	budget := maxStackPatchBytes
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
		budget = applyPatchBudget(&rd, budget)
		byRoot[key] = len(out.Repos)
		out.Repos = append(out.Repos, rd)
		out.Additions += rd.Additions
		out.Deletions += rd.Deletions
	}
	if budget <= 0 {
		out.PatchesTruncated = true
	}
	return out
}

func applyPatchBudget(rd *RepoDiff, budget int) int {
	for i := range rd.Files {
		if budget <= 0 {
			if rd.Files[i].Patch != "" {
				rd.Files[i].Patch = ""
				rd.Files[i].Truncated = true
			}
			continue
		}
		budget -= len(rd.Files[i].Patch)
	}
	return budget
}

func diffRepo(service, dir, base string, includePatch bool) RepoDiff {
	rd := RepoDiff{Service: service, Repo: dir, Base: base}
	if dir == "" || !isGitRepo(dir) {
		rd.Error = "not a git repository"
		return rd
	}
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

	rd.Files = []FileDiff{}

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

	for _, f := range untrackedFiles(rd.Repo, includePatch) {
		rd.Files = append(rd.Files, f)
		rd.Additions += f.Additions
	}

	sort.Slice(rd.Files, func(i, j int) bool { return rd.Files[i].Path < rd.Files[j].Path })
	return rd
}

func untrackedFiles(dir string, includePatch bool) []FileDiff {
	out, err := gitOut(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var files []FileDiff
	for _, path := range strings.Split(out, "\x00") {
		if strings.TrimSpace(path) == "" {
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
		lines++
	}
	return head, lines, total > maxPatchBytes, nil
}

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

func isProbablyBinary(content []byte) bool {
	limit := min(len(content), 8000)
	for i := range limit {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

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

func filePatch(dir, ref, path string) (patch string, truncated bool) {
	out, err := gitOut(dir, "diff", ref, "--", path)
	if err != nil {
		return "", false
	}
	if len(out) <= maxPatchBytes {
		return out, false
	}
	return out[:maxPatchBytes] + "\n… truncated, open the file to see the rest", true
}

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
