package gitbase

import (
	"os/exec"
	"strings"
)

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

func MergeBase(dir string) (base, ref string) {
	for _, r := range Refs(dir) {
		out, err := exec.Command("git", "-C", dir, "merge-base", "HEAD", r).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out)), r
		}
	}
	return "", ""
}
