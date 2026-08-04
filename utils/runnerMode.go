package utils

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// DockerSource says where a docker-mode service's definition comes from.
type DockerSource int

const (
	SourceNone DockerSource = iota
	// SourceRepoCompose delegates to a compose file the service repo ships.
	SourceRepoCompose
	// SourceDockerfile generates a compose wrapper around the Dockerfile.
	SourceDockerfile
	// SourceImage runs a registry image directly (runner.image) — no build.
	SourceImage
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
		if fileExists(p) {
			return p
		}
		return ""
	}
	for _, name := range repoComposeNames {
		p := filepath.Join(s.AbsolutePath, name)
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func dockerfileExists(s Service) bool {
	return fileExists(filepath.Join(s.AbsolutePath, s.DockerfileName()))
}

// DetectDockerSource inspects the service dir. AbsolutePath must be final
// (post clone / worktree materialization).
//
// Precedence: declared build fields (dockerfile/target/args/…) or an explicit
// docker runner pin the Dockerfile — a repo's own compose file must never
// silently override what the user configured. Only for zero-config services
// does a repo compose file win over a plain Dockerfile.
func DetectDockerSource(s Service) DockerSource {
	if s.Runner.Image != "" {
		return SourceImage
	}
	dockerfilePinned := hasBuildFields(s.Runner) || s.Runner.IsDocker()
	if dockerfilePinned && s.Runner.ComposeFile == "" && dockerfileExists(s) {
		return SourceDockerfile
	}
	if RepoComposeFile(s) != "" {
		return SourceRepoCompose
	}
	if dockerfileExists(s) {
		return SourceDockerfile
	}
	return SourceNone
}

func hasBuildFields(r Runner) bool {
	return r.Dockerfile != "" || r.Target != "" || len(r.Args) > 0 ||
		r.Command != "" || r.Context != "" || len(r.Volumes) > 0 ||
		r.ContainerPort != 0
}

// ResolveRunnerModes decides, per service, whether it runs natively or in
// docker, and stamps Runner.Name so every existing docker branch applies.
// dockerFlag is `corgi run --docker`. announce prints the ✨ detection lines —
// pass false from stop/restart where "running from Dockerfile" would mislead.
func ResolveRunnerModes(services []Service, dockerFlag, announce bool) ([]Service, error) {
	out := make([]Service, len(services))
	for i, s := range services {
		if s.Runner.ComposeFile != "" && hasBuildFields(s.Runner) {
			return nil, fmt.Errorf(
				"service %s: runner.composeFile and build fields (dockerfile/context/target/args/volumes/containerPort/command) are mutually exclusive",
				s.ServiceName)
		}
		if s.Runner.Image != "" && (s.Runner.ComposeFile != "" || s.Runner.Dockerfile != "" || s.Runner.Context != "" || s.Runner.Target != "" || len(s.Runner.Args) > 0) {
			return nil, fmt.Errorf(
				"service %s: runner.image and build sources (dockerfile/context/target/args/composeFile) are mutually exclusive",
				s.ServiceName)
		}
		if s.Runner.Image != "" && s.Port == 0 {
			return nil, fmt.Errorf(
				"service %s: runner.image needs `port:` — there is no Dockerfile to read EXPOSE from",
				s.ServiceName)
		}
		// A declared dockerfile that doesn't exist is a config error (typo),
		// not a cue to silently fall back to a repo compose file — but only
		// when docker mode would actually engage for this service.
		if s.Runner.Dockerfile != "" && !dockerfileExists(s) &&
			(s.Runner.IsDocker() || dockerFlag || len(s.Start) == 0) {
			return nil, fmt.Errorf(
				"service %s: runner.dockerfile %q not found in %s",
				s.ServiceName, s.Runner.Dockerfile, s.AbsolutePath)
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
			// Declared intent — a missing port here is a hard config error.
			if src == SourceDockerfile {
				if err := resolveDockerPortDefaults(&s, announce); err != nil {
					return nil, err
				}
			}
		case s.ManualRun:
		case dockerFlag && src != SourceNone:
			if flipped, ok := tryDockerFlip(s, src, announce, "--docker,"); ok {
				s = flipped
			}
		case len(s.Start) == 0 && src != SourceNone:
			if flipped, ok := tryDockerFlip(s, src, announce, "no start scripts —"); ok {
				s = flipped
			}
		case len(s.Start) == 0 && s.Port != 0:
			// Post-clone parity with E_MISSING_START: the repo turned out to
			// ship neither scripts nor a dockerfile/compose file.
			return nil, fmt.Errorf(
				"service %s: sets port %d but has no start command, no Dockerfile and no compose file in %s",
				s.ServiceName, s.Port, s.AbsolutePath)
		}
		out[i] = s
	}
	return out, nil
}

// tryDockerFlip stamps docker mode on a service corgi chose (flag or
// auto-detect). A Dockerfile with no resolvable port isn't a hard error here —
// the user didn't declare docker intent — so the service stays native with a
// hint instead of failing the whole run.
func tryDockerFlip(s Service, src DockerSource, announce bool, why string) (Service, bool) {
	flipped := s
	flipped.Runner.Name = "docker"
	flipped.ResolvedDockerSource = src
	if src == SourceDockerfile {
		if err := resolveDockerPortDefaults(&flipped, announce); err != nil {
			if announce {
				Info(fmt.Sprintf("%s: found a Dockerfile but no port — add `port:` or an EXPOSE line to run it in docker", s.ServiceName))
			}
			return s, false
		}
	}
	if announce {
		Info(fmt.Sprintf("✨ %s: %s running from %s", s.ServiceName, why, sourceLabel(src)))
	}
	return flipped, true
}

// resolveDockerPortDefaults fills Port from the Dockerfile's EXPOSE for
// docker-mode services that declared none. Parse-time resolution only covers
// services with runner declared in yml; auto-detected ones land here.
func resolveDockerPortDefaults(s *Service, announce bool) error {
	if s.Port != 0 {
		return nil
	}
	exposed, err := GetExposedPortFromDockerfile(*s)
	if err == nil {
		if p, convErr := strconv.Atoi(exposed); convErr == nil && p > 0 {
			s.Port = p
			if announce {
				Info(fmt.Sprintf("✨ %s: port %d detected from EXPOSE", s.ServiceName, p))
			}
			return nil
		}
	}
	return fmt.Errorf(
		"service %s: docker mode needs a port — add `port:` in corgi-compose.yml or an EXPOSE line in %s",
		s.ServiceName, s.DockerfileName())
}

func sourceLabel(src DockerSource) string {
	switch src {
	case SourceRepoCompose:
		return "the repo's compose file"
	case SourceImage:
		return "its image"
	default:
		return "Dockerfile"
	}
}
