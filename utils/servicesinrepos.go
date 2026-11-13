package utils

import (
	"strings"
)

func PullRequestProject(ref string) string {
	ref = strings.TrimSpace(ref)
	cut := strings.LastIndexAny(ref, "#!")
	if cut <= 0 || cut == len(ref)-1 {
		return ""
	}
	for _, r := range ref[cut+1:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	project := ref[:cut]
	if !strings.Contains(project, "/") {
		return ""
	}
	return strings.ToLower(project)
}

func RemoteProject(remoteURL string) string {
	u := strings.TrimSpace(remoteURL)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if slash := strings.Index(u, "/"); slash >= 0 {
			u = u[slash+1:]
		} else {
			return ""
		}
	} else if colon := strings.Index(u, ":"); colon >= 0 {
		u = u[colon+1:]
	}
	return strings.ToLower(strings.Trim(u, "/"))
}

func ServicesInRepos(corgi *CorgiCompose, projects []string) []string {
	if corgi == nil || len(projects) == 0 {
		return nil
	}
	wanted := map[string]bool{}
	for _, p := range projects {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			wanted[p] = true
		}
	}
	projectOfRoot := map[string]string{}
	var names []string
	for i := range corgi.Services {
		svc := &corgi.Services[i]
		root, ok := repoRoot(svc.AbsolutePath)
		if !ok {
			continue
		}
		project, seen := projectOfRoot[root]
		if !seen {
			url, _ := gitOut(root, "remote", "get-url", "origin")
			project = RemoteProject(url)
			projectOfRoot[root] = project
		}
		if project != "" && wanted[project] {
			names = append(names, svc.ServiceName)
		}
	}
	return names
}
