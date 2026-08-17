package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInContainerDetectsTheDockerMarker(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, ".dockerenv")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapContainerProbes(t, marker, filepath.Join(dir, "no-cgroup"))
	defer restore()

	if !InContainer() {
		t.Error("expected the docker marker to be detected")
	}
}

func TestInContainerReadsTheInitCgroup(t *testing.T) {
	dir := t.TempDir()
	cgroup := filepath.Join(dir, "cgroup")
	if err := os.WriteFile(cgroup, []byte("0::/kubepods/besteffort/pod123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapContainerProbes(t, filepath.Join(dir, "absent"), cgroup)
	defer restore()

	if !InContainer() {
		t.Error("expected a kubepods cgroup to count as a container")
	}
}

func TestInContainerIsFalseOnAPlainHost(t *testing.T) {
	dir := t.TempDir()
	cgroup := filepath.Join(dir, "cgroup")
	if err := os.WriteFile(cgroup, []byte("0::/init.scope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapContainerProbes(t, filepath.Join(dir, "absent"), cgroup)
	defer restore()

	if InContainer() {
		t.Error("a host cgroup must not be read as a container")
	}
}

func swapContainerProbes(t *testing.T, marker, cgroup string) func() {
	t.Helper()
	origMarker, origCgroup := dockerEnvMarkerPath, initCgroupPath
	dockerEnvMarkerPath, initCgroupPath = marker, cgroup
	return func() { dockerEnvMarkerPath, initCgroupPath = origMarker, origCgroup }
}

// The gitignored env file is the classic CI blocker: corgi falls back to a
// committed example whose placeholder values start the service and then fail
// at the first request, thousands of lines from the cause.
func TestMissingEnvSourcesReportsTheFallback(t *testing.T) {
	dir := t.TempDir()
	serviceDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serviceDir, ".env-example"), []byte("KEY=changeme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapComposeDir(t, dir)
	defer restore()

	missing := MissingEnvSources(&CorgiCompose{Services: []Service{{
		ServiceName:         "api",
		Path:                "./api",
		AbsolutePath:        serviceDir,
		CopyEnvFromFilePath: "env/source/api.env",
	}}})

	if len(missing) != 1 {
		t.Fatalf("expected one missing env source, got %+v", missing)
	}
	if missing[0].Service != "api" || missing[0].Declared != "env/source/api.env" {
		t.Errorf("unexpected report: %+v", missing[0])
	}
	if missing[0].Fallback == "" {
		t.Error("expected the silent fallback to be named")
	}
}

func TestMissingEnvSourcesIsQuietWhenTheFileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "env", "api.env"), []byte("KEY=real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := swapComposeDir(t, dir)
	defer restore()

	missing := MissingEnvSources(&CorgiCompose{Services: []Service{{
		ServiceName:         "api",
		Path:                "./api",
		CopyEnvFromFilePath: "env/api.env",
	}}})
	if len(missing) != 0 {
		t.Errorf("expected nothing to report, got %+v", missing)
	}
}

// A service with no copyEnvFromFilePath has nothing to be missing.
func TestMissingEnvSourcesIgnoresServicesWithoutADeclaration(t *testing.T) {
	restore := swapComposeDir(t, t.TempDir())
	defer restore()

	missing := MissingEnvSources(&CorgiCompose{Services: []Service{{ServiceName: "api", Path: "./api"}}})
	if len(missing) != 0 {
		t.Errorf("expected nothing to report, got %+v", missing)
	}
}

// ${tier} is substituted before the file is looked for, or --tier staging would
// always report a miss.
func TestMissingEnvSourcesSubstitutesTheTier(t *testing.T) {
	dir := t.TempDir()
	restore := swapComposeDir(t, dir)
	defer restore()
	origTier := ActiveTierName
	ActiveTierName = "staging"
	defer func() { ActiveTierName = origTier }()

	missing := MissingEnvSources(&CorgiCompose{Services: []Service{{
		ServiceName:         "api",
		Path:                "./api",
		CopyEnvFromFilePath: "creds/${tier}.env",
	}}})

	if len(missing) != 1 || missing[0].Declared != "creds/staging.env" {
		t.Fatalf("expected the tier substituted, got %+v", missing)
	}
}

func swapComposeDir(t *testing.T, dir string) func() {
	t.Helper()
	orig := CorgiComposePathDir
	CorgiComposePathDir = dir
	return func() { CorgiComposePathDir = orig }
}
