package utils

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// AssetsBranchPrefix is where a repo keeps the images its pull requests show.
// The branch is never merged, so the links outlive the PR branch.
const AssetsBranchPrefix = "pr-assets/"

// AssetFile is one image after the push: where it came from, where it sits on
// the assets branch, and the link a PR body can carry.
type AssetFile struct {
	Source   string `json:"source"`
	Path     string `json:"path"`
	URL      string `json:"url,omitempty"`
	Markdown string `json:"markdown"`
}

// AssetPush is the result of PushAssets: the branch, its pushed head, and one
// entry per file with the markdown to paste.
type AssetPush struct {
	Branch   string      `json:"branch"`
	Head     string      `json:"head"`
	Files    []AssetFile `json:"files"`
	Markdown string      `json:"markdown"`
}

// PushAssets commits the files to docs/pr-assets/<key>/ on the repo's
// pr-assets/<key> branch (created from origin/<base> the first time, appended
// to after), pushes it, confirms origin has the same head, and returns the
// link form the forge renders on a private repo. The user's checkout is not
// touched: the work happens in a throwaway worktree that is removed at the end.
func PushAssets(dir, key, base string, files []string) (AssetPush, error) {
	var out AssetPush
	key = strings.TrimSpace(key)
	if key == "" {
		return out, fmt.Errorf("--key is required: the story key that names the assets branch")
	}
	if strings.ContainsAny(key, " /\\") {
		return out, fmt.Errorf("--key %q cannot contain spaces or slashes", key)
	}
	if len(files) == 0 {
		return out, fmt.Errorf("nothing to push: pass at least one image file")
	}
	if !isGitRepo(dir) {
		return out, fmt.Errorf("%s is not a git repository", dir)
	}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			return out, fmt.Errorf("%s: %v", f, err)
		}
		if st.IsDir() {
			return out, fmt.Errorf("%s is a directory; name the image files", f)
		}
	}
	remote, err := gitOut(dir, "remote", "get-url", "origin")
	if err != nil {
		return out, fmt.Errorf("no origin remote in %s", dir)
	}

	branch := AssetsBranchPrefix + key
	_ = gitRunNoPrompt(dir, "fetch", "--quiet", "origin", branch)
	start := "origin/" + branch
	if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", start); err != nil {
		start, err = assetsStartRef(dir, base)
		if err != nil {
			return out, err
		}
	}

	wt := filepath.Join(os.TempDir(), "corgi-assets", key)
	_, _ = gitOut(dir, "worktree", "remove", "--force", wt)
	_ = os.RemoveAll(wt)
	_ = gitRun(dir, "worktree", "prune")
	if err := gitRun(dir, "worktree", "add", "-B", branch, wt, start); err != nil {
		return out, fmt.Errorf("git worktree add %s: %v", wt, err)
	}
	defer func() {
		_, _ = gitOut(dir, "worktree", "remove", "--force", wt)
		_ = gitRun(dir, "worktree", "prune")
	}()

	rel := path.Join("docs", "pr-assets", key)
	dest := filepath.Join(wt, filepath.FromSlash(rel))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return out, err
	}
	for _, f := range files {
		name := filepath.Base(f)
		body, err := os.ReadFile(f)
		if err != nil {
			return out, err
		}
		if err := os.WriteFile(filepath.Join(dest, name), body, 0o644); err != nil {
			return out, err
		}
		p := path.Join(rel, name)
		u := assetURL(remote, branch, p)
		md := "![" + strings.TrimSuffix(name, path.Ext(name)) + "](" + u + ")"
		if u == "" {
			md = "![" + strings.TrimSuffix(name, path.Ext(name)) + "](" + p + ")"
		}
		out.Files = append(out.Files, AssetFile{Source: f, Path: p, URL: u, Markdown: md})
	}

	if err := gitRun(wt, "add", "-f", "--", rel); err != nil {
		return out, fmt.Errorf("git add: %v", err)
	}
	if _, err := gitOut(wt, "diff", "--cached", "--quiet"); err != nil {
		msg := "PR assets for " + key + " (screenshots only, never merge)"
		if err := gitRun(wt, "commit", "--quiet", "-m", msg); err != nil {
			return out, fmt.Errorf("git commit: %v", err)
		}
	}
	if err := gitRunNoPrompt(wt, "push", "--quiet", "-u", "origin", branch); err != nil {
		return out, fmt.Errorf("git push origin %s: %v", branch, err)
	}
	head, _ := gitOut(wt, gitRevParse, "HEAD")
	pushed, err := gitOutNoPrompt(dir, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil || !strings.HasPrefix(pushed, head) {
		return out, fmt.Errorf("origin does not have %s at %s after the push", branch, head)
	}

	out.Branch, out.Head = branch, head
	out.Markdown = assetsMarkdown(out.Files)
	return out, nil
}

func assetsStartRef(dir, base string) (string, error) {
	if base == "" {
		base = DefaultBranchOf(dir)
	}
	if base == "" {
		return "", fmt.Errorf("could not resolve the base branch; pass --base")
	}
	for _, ref := range []string{"origin/" + base, base} {
		if _, err := gitOut(dir, gitRevParse, "--verify", "--quiet", ref); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("base branch %s is not in this repo", base)
}

// assetURL is the one link form the forge renders inside a private repo's PR
// body: GitHub through the blob viewer with ?raw=true (raw.githubusercontent.com
// answers 404 to a browser there), GitLab through /-/raw/. A remote that is not
// a forge URL (a local path in tests) yields "".
func assetURL(remote, branch, p string) string {
	host, repoPath := remoteWebParts(remote)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "gitlab") {
		return "https://" + host + "/" + repoPath + "/-/raw/" + branch + "/" + p
	}
	return "https://" + host + "/" + repoPath + "/blob/" + branch + "/" + p + "?raw=true"
}

// remoteWebParts turns git@host:group/repo.git or https://host/group/repo.git
// into (host, group/repo).
func remoteWebParts(remote string) (host, repoPath string) {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "git@") || (strings.Contains(remote, ":") && !strings.Contains(remote, "://")) {
		at := strings.Index(remote, "@")
		colon := strings.Index(remote, ":")
		if colon < 0 {
			return "", ""
		}
		host = remote[at+1 : colon]
		repoPath = remote[colon+1:]
	} else {
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return "", ""
		}
		host, repoPath = u.Host, strings.TrimPrefix(u.Path, "/")
	}
	if i := strings.Index(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")
	if host == "" || repoPath == "" || !strings.Contains(host, ".") {
		return "", ""
	}
	return host, repoPath
}

func assetsMarkdown(files []AssetFile) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("|")
	for _, f := range files {
		b.WriteString(" " + strings.TrimSuffix(path.Base(f.Path), path.Ext(f.Path)) + " |")
	}
	b.WriteString("\n|")
	for range files {
		b.WriteString(" --- |")
	}
	b.WriteString("\n|")
	for _, f := range files {
		b.WriteString(" " + f.Markdown + " |")
	}
	b.WriteString("\n")
	return b.String()
}
