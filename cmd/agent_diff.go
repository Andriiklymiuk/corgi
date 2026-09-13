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
)

// The diff a session has built up, for a phone: the files with their
// counts, and one file's patch on request. git does the work; corgi only
// picks the base and bounds the answer — a phone on 5G wants the list in a
// blink and one file at a time, never the whole branch in one body.
//
// Same gate as the conversation: the workspace has to be readable from a
// phone (corgi agent stream enable), because a diff is the code.

// diffPatchMax bounds one file's patch; a phone cannot show more anyway.
const diffPatchMax = 200 << 10

// diffTimeout bounds git: a huge repo must not hold the request.
const diffTimeout = 8 * time.Second

// DiffFile is one changed path with its counts.
type DiffFile struct {
	Path      string `json:"path"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	Binary    bool   `json:"binary,omitempty"`
	Generated bool   `json:"generated,omitempty"`
}

// diffBase is the merge base with the main branch, or "" when there is
// none to speak of (no main, not a repo).
var diffBase = func(ctx context.Context, dir string) string {
	for _, b := range []string{"origin/main", "origin/master", "main", "master"} {
		out, err := exec.CommandContext(ctx, "git", "-C", dir, "merge-base", "HEAD", b).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

// diffFiles is --numstat against the base: every path, with its counts.
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

// diffPatch is one file's unified diff against the base, cut at diffPatchMax.
func diffPatch(ctx context.Context, dir, base, path string) (patch string, truncated bool, err error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--no-color", "--unified=3", base, "--", path).Output()
	if err != nil {
		return "", false, err
	}
	if len(out) > diffPatchMax {
		cut := bytes.LastIndexByte(out[:diffPatchMax], '\n')
		if cut < 0 {
			cut = diffPatchMax
		}
		return string(out[:cut]), true, nil
	}
	return string(out), false, nil
}

// launchDiffHandler: GET /launch/diff?session=<id> lists the files;
// &file=<path> answers with that file's patch.
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
	if file := r.URL.Query().Get("file"); file != "" {
		if strings.Contains(file, "..") || strings.HasPrefix(file, "/") {
			writeLaunchError(w, http.StatusBadRequest, "a path inside the checkout")
			return
		}
		patch, truncated, err := diffPatch(ctx, dir, base, file)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "git could not diff that file")
			return
		}
		writeLaunchJSON(w, map[string]any{"session": session.ID, "file": file, "patch": patch, "truncated": truncated, "base": base[:min(12, len(base))]})
		return
	}
	files, err := diffFiles(ctx, dir, base)
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "git could not diff the branch")
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

// diffDirFor is where a session's branch lives: its cwd, which for an
// isolated session is the worktree itself.
var diffDirFor = func(s sessions.Session) string { return s.Cwd }
