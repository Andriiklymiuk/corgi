package utils

import (
	"fmt"
	"path/filepath"
)

// DockerSource says where a docker-mode service's definition comes from.
type DockerSource int

const (
	SourceNone DockerSource = iota
	// SourceRepoCompose delegates to a compose file the service repo ships.
	SourceRepoCompose
	// SourceDockerfile generates a compose wrapper around the Dockerfile.
	SourceDockerfile
)

var repoComposeNames = []string{
	"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml",
}

// RepoComposeFile returns the absolute path of the compose file the service
// repo ships — the declared runner.composeFile, else the first conventional
// name found. Empty when none exists.
func RepoComposeFile(s Service) string {
	if s.Runner.ComposeFile != "" {
		p := filepath.Join(s.AbsolutePath, s.Runner.ComposeFile)
		if exists, _ := CheckIfFileExistsInDirectory(filepath.Dir(p), filepath.Base(p)); exists {
			return p
		}
		return ""
	}
	for _, name := range repoComposeNames {
		if exists, _ := CheckIfFileExistsInDirectory(s.AbsolutePath, name); exists {
			return filepath.Join(s.AbsolutePath, name)
		}
	}
	return ""
}

// DetectDockerSource inspects the service dir. AbsolutePath must be final
// (post clone / worktree materialization).
func DetectDockerSource(s Service) DockerSource {
	if RepoComposeFile(s) != "" {
		return SourceRepoCompose
	}
	if exists, _ := CheckIfFileExistsInDirectory(s.AbsolutePath, s.DockerfileName()); exists {
		return SourceDockerfile
	}
	return SourceNone
}

func hasBuildFields(r Runner) bool {
	return r.Dockerfile != "" || r.Target != "" || len(r.Args) > 0 ||
		r.Command != "" || r.Context != ""
}

// ResolveRunnerModes decides, per service, whether it runs natively or in
// docker, and stamps Runner.Name so every existing docker branch applies.
// dockerFlag is `corgi run --docker`.
func ResolveRunnerModes(services []Service, dockerFlag bool) ([]Service, error) {
	out := make([]Service, len(services))
	for i, s := range services {
		if s.Runner.ComposeFile != "" && hasBuildFields(s.Runner) {
			return nil, fmt.Errorf(
				"service %s: runner.composeFile and build fields (dockerfile/context/target/args/command) are mutually exclusive",
				s.ServiceName)
		}
		src := DetectDockerSource(s)
		switch {
		case s.Runner.IsDocker():
			if src == SourceNone {
				return nil, fmt.Errorf(
					"service %s: runner is docker but no dockerfile %q or compose file found in %s",
					s.ServiceName, s.DockerfileName(), s.AbsolutePath)
			}
			s.ResolvedDockerSource = src
		case s.ManualRun:
		case dockerFlag && src != SourceNone:
			s.Runner.Name = "docker"
			s.ResolvedDockerSource = src
			Info(fmt.Sprintf("✨ %s: --docker, running from %s", s.ServiceName, sourceLabel(src)))
		case len(s.Start) == 0 && src != SourceNone:
			s.Runner.Name = "docker"
			s.ResolvedDockerSource = src
			Info(fmt.Sprintf("✨ %s: no start scripts — running from %s", s.ServiceName, sourceLabel(src)))
		}
		out[i] = s
	}
	return out, nil
}

func sourceLabel(src DockerSource) string {
	if src == SourceRepoCompose {
		return "the repo's compose file"
	}
	return "Dockerfile"
}
