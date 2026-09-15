// Package gitbase names the branch a checkout's work is measured against.
package gitbase

import (
	"os/exec"
	"strings"
)

// Refs is the candidates for a checkout's base branch, best first: what
// origin calls its default (origin/HEAD), then the usual names on origin,
// then the same names locally. A repo whose default is trunk has a stale
// master lying around more often than not; asking origin first keeps a
// diff from being measured against the wrong branch.
func Refs(dir string) []string {
	var refs []string
	if out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			refs = append(refs, ref)
		}
	}
	for _, name := range []string{"main", "master", "trunk", "develop"} {
		refs = append(refs, "origin/"+name)
	}
	for _, name := range []string{"main", "master", "trunk", "develop"} {
		refs = append(refs, name)
	}
	return refs
}

// MergeBase is where HEAD left the base branch: the first candidate that
// shares history with it, and the commit they share. "" when none does.
func MergeBase(dir string) (base, ref string) {
	for _, r := range Refs(dir) {
		out, err := exec.Command("git", "-C", dir, "merge-base", "HEAD", r).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out)), r
		}
	}
	return "", ""
}
