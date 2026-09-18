package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/gitbase"
)

const diffPatchMax = 200 << 10

const diffTimeout = 8 * time.Second

type DiffFile struct {
	Path      string `json:"path"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Binary    bool   `json:"binary,omitempty"`
	Generated bool   `json:"generated,omitempty"`
}

var diffBase = func(ctx context.Context, dir string) string {
	for _, b := range gitbase.Refs(dir) {
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "merge-base", "HEAD", b).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func diffFiles(ctx context.Context, dir, base string) ([]DiffFile, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--numstat", "--no-color", base).Output()
	if err != nil {
		return nil, err
	}
	var files []DiffFile
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		path := strings.Join(f[2:], " ")
		df := DiffFile{Path: path, Generated: daemon.GeneratedDiffPath(path)}
		if f[0] == "-" || f[1] == "-" {
			df.Binary = true
		} else {
			df.Added, _ = strconv.Atoi(f[0])
			df.Deleted, _ = strconv.Atoi(f[1])
		}
		files = append(files, df)
	}
	if files == nil {
		files = []DiffFile{}
	}
	return files, nil
}

const diffAllMax = 600 << 10

func diffPatch(ctx context.Context, dir, base, path string) (patch string, truncated bool, err error) {
	args := []string{"-C", dir, "diff", "--no-color", "--unified=3", base, "--"}
	limit := diffPatchMax
	if path == "" {
		limit = diffAllMax
		files, ferr := diffFiles(ctx, dir, base)
		if ferr != nil {
			return "", false, ferr
		}
		for _, f := range files {
			if !f.Generated && !f.Binary {
				args = append(args, f.Path)
			}
		}
		if len(args) == 7 {
			return "", false, nil
		}
	} else {
		args = append(args, path)
	}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", false, err
	}
	if len(out) > limit {
		cut := bytes.LastIndexByte(out[:limit], '\n')
		if cut < 0 {
			cut = limit
		}
		return string(out[:cut]), true, nil
	}
	return string(out), false, nil
}

func launchDiffHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET /launch/diff?session=<id>[&file=<path>]")
		return
	}
	session, code, msg := launchSessionFor(r.URL.Query().Get("session"))
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if !streamAllowedFor(session.Label) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reading %s's code from a phone is off on the laptop: corgi agent stream enable --workspace %s", session.Label, session.Label))
		return
	}
	dir := diffDirFor(session)
	if dir == "" {
		writeLaunchError(w, http.StatusNotFound, "this session has no checkout to diff")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diffTimeout)
	defer cancel()
	base := diffBase(ctx, dir)
	if base == "" {
		writeLaunchJSON(w, map[string]any{"session": session.ID, "files": []DiffFile{}, "base": "", "note": "no main branch to diff against"})
		return
	}
	if r.URL.Query().Get("all") == "1" {
		patch, truncated, err := diffPatch(ctx, dir, base, "")
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "git could not diff the branch")
			return
		}
		writeLaunchJSON(w, map[string]any{"session": session.ID, "file": "", "patch": patch, "truncated": truncated, "base": base[:min(12, len(base))]})
		return
	}
	files, err := diffFiles(ctx, dir, base)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "git could not diff the branch")
		return
	}
	if file := r.URL.Query().Get("file"); file != "" {
		listed := listedDiffPath(files, file)
		if listed == "" {
			writeLaunchError(w, http.StatusBadRequest, "a file from this branch's diff")
			return
		}
		patch, truncated, err := diffPatch(ctx, dir, base, listed)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "git could not diff that file")
			return
		}
		writeLaunchJSON(w, map[string]any{"session": session.ID, "file": listed, "patch": patch, "truncated": truncated, "base": base[:min(12, len(base))]})
		return
	}
	added, deleted := 0, 0
	for _, f := range files {
		if !f.Generated {
			added += f.Added
			deleted += f.Deleted
		}
	}
	writeLaunchJSON(w, map[string]any{"session": session.ID, "base": base[:min(12, len(base))], "branch": session.Branch, "files": files, "added": added, "deleted": deleted})
}

func listedDiffPath(files []DiffFile, file string) string {
	if !validDiffPath(file) {
		return ""
	}
	for _, f := range files {
		if f.Path == file {
			return f.Path
		}
	}
	return ""
}

func validDiffPath(file string) bool {
	if file == "" || len(file) > 4096 || strings.HasPrefix(file, "-") || strings.HasPrefix(file, "/") {
		return false
	}
	for _, part := range strings.Split(file, "/") {
		if part == ".." || part == "" {
			return false
		}
	}
	for _, r := range file {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

var diffDirFor = func(s sessions.Session) string { return s.Cwd }
