package utils

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
)

const PostgresDataDir = "/var/lib/postgresql/data"

// cockroach/yugabyte are pg-wire but a different storage engine; supabase has a
// different lifecycle — none share the physical data-dir format, so all excluded.
var postgresFamily = map[string]bool{
	"postgres":    true,
	"postgis":     true,
	"pgvector":    true,
	"timescaledb": true,
}

func IsPostgresFamilyDriver(driver string) bool { return postgresFamily[driver] }

func ContainerName(driver, serviceName string) string { return driver + "-" + serviceName }

func SnapshotsDir(serviceName string) (string, error) {
	base, err := GetPathToDbService(serviceName)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "snapshots"), nil
}

func SnapshotPaths(serviceName, name string) (archive, meta string, err error) {
	dir, err := SnapshotsDir(serviceName)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, name+".tar.zst"), filepath.Join(dir, name+".meta.json"), nil
}

func DefaultSnapshotName(at time.Time) string { return at.UTC().Format("2006-01-02-1504") }

func SanitizeSnapshotName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("snapshot name is empty")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid snapshot name %q (no path separators or '..')", name)
	}
	return name, nil
}

type SnapshotMeta struct {
	Stack          string `json:"stack"`
	Service        string `json:"service"`
	PgVersionMajor string `json:"pgVersionMajor"`
	Image          string `json:"image"`
	Arch           string `json:"arch"`
	DataPath       string `json:"dataPath"`
	CreatedAt      string `json:"createdAt"`
	SizeBytes      int64  `json:"sizeBytes"`
	SHA256         string `json:"sha256"`
}

func WriteSnapshotMeta(path string, m SnapshotMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func ReadSnapshotMeta(path string) (SnapshotMeta, error) {
	var m SnapshotMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("snapshot metadata %s is unreadable: %w", filepath.Base(path), err)
	}
	return m, nil
}

type SnapshotListItem struct {
	Name string `json:"name"`
	SnapshotMeta
	ArchivePath string `json:"archivePath"`
}

func ListSnapshots(serviceName string) ([]SnapshotListItem, error) {
	dir, err := SnapshotsDir(serviceName)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SnapshotListItem
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".tar.zst")
		if name == e.Name() {
			continue
		}
		arc, metaPath, err := SnapshotPaths(serviceName, name)
		if err != nil {
			return nil, err
		}
		meta, err := ReadSnapshotMeta(metaPath)
		if err != nil {
			continue
		}
		out = append(out, SnapshotListItem{Name: name, SnapshotMeta: meta, ArchivePath: arc})
	}
	return out, nil
}

func CheckRestoreCompatibility(m SnapshotMeta, targetImage, targetArch, targetPgMajor string) error {
	if m.Image != targetImage {
		return fmt.Errorf("image mismatch: snapshot is %q but target is %q — physical snapshots are image-specific (extensions + uid). Use --force to override", m.Image, targetImage)
	}
	if m.Arch != targetArch {
		return fmt.Errorf("arch mismatch: snapshot is %q but target is %q — physical format is architecture-specific. Use --force to override", m.Arch, targetArch)
	}
	// Only compare when both sides recorded a version; older snapshots may lack it.
	if m.PgVersionMajor != "" && targetPgMajor != "" && m.PgVersionMajor != targetPgMajor {
		return fmt.Errorf("pg version mismatch: snapshot is major %q but target is major %q — a physical data dir is not portable across major versions. Use --force to override", m.PgVersionMajor, targetPgMajor)
	}
	return nil
}

// IsStackSupervised reports whether a detached `corgi run` is managing this stack:
// snapshot/restore stop+restart the container and would race a live supervisor.
func IsStackSupervised(composeDir string) bool {
	path := RunStatePath(composeDir)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	st, err := ReadRunState(path)
	if err != nil {
		return false
	}
	st = ReconcileRunState(st, PidAlive, ContainerRunning)
	for _, e := range st.Services {
		if e.Status == "running" {
			return true
		}
	}
	for _, e := range st.DBServices {
		if e.Status == "running" {
			return true
		}
	}
	return false
}

func parsePgVersionMajor(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	return s
}

func containerPgVersionMajor(container string) (string, error) {
	out, err := exec.Command("docker", "exec", container, "cat", PostgresDataDir+"/PG_VERSION").Output()
	if err != nil {
		return "", fmt.Errorf("reading PG_VERSION from %s: %w", container, err)
	}
	v := parsePgVersionMajor(string(out))
	if v == "" {
		return "", fmt.Errorf("empty PG_VERSION in %s", container)
	}
	return v, nil
}

func dockerServerArch() (string, error) {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Arch}}").Output()
	if err != nil {
		return "", fmt.Errorf("reading docker server arch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func composeImage(serviceDir string) (string, error) {
	c := exec.Command("docker", "compose", "config", "--images")
	c.Dir = serviceDir
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("resolving compose image in %s: %w", serviceDir, err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("no image found in compose for %s", serviceDir)
}

func composeInDir(dir string, args ...string) error {
	c := exec.Command("docker", append([]string{"compose"}, args...)...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose %s in %s: %s: %w",
			strings.Join(args, " "), dir, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Generous timeout: postgres STOPSIGNAL is SIGINT (clean), but a big checkpoint can
// exceed the default 10s and get SIGKILLed, leaving a recovery-needing data dir.
func stopContainerClean(container string, timeoutSeconds int) error {
	out, err := exec.Command("docker", "stop", "-t", fmt.Sprint(timeoutSeconds), container).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop %s: %s: %w", container, strings.TrimSpace(string(out)), err)
	}
	code, err := exec.Command("docker", "inspect", "-f", "{{.State.ExitCode}}", container).Output()
	if err != nil {
		return fmt.Errorf("inspecting %s exit code: %w", container, err)
	}
	if c := strings.TrimSpace(string(code)); c != "0" {
		return fmt.Errorf("container %s did not exit cleanly (exit %s) — aborting to avoid a recovery-needing snapshot", container, c)
	}
	return nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// Streams docker cp through zstd; the returned size/sha256 are over the COMPRESSED archive.
func writeSnapshotArchive(container, archivePath string) (int64, string, error) {
	cp := exec.Command("docker", "cp", container+":"+PostgresDataDir, "-")
	stdout, err := cp.StdoutPipe()
	if err != nil {
		return 0, "", err
	}
	var cpErr bytes.Buffer
	cp.Stderr = &cpErr
	if err := cp.Start(); err != nil {
		return 0, "", err
	}

	f, err := os.Create(archivePath)
	if err != nil {
		_ = cp.Process.Kill()
		_ = cp.Wait()
		return 0, "", err
	}
	hasher := sha256.New()
	counter := &countingWriter{}
	enc, err := zstd.NewWriter(io.MultiWriter(f, hasher, counter))
	if err != nil {
		f.Close()
		_ = cp.Process.Kill()
		_ = cp.Wait()
		return 0, "", err
	}

	_, copyErr := io.Copy(enc, stdout)
	encErr := enc.Close()
	closeErr := f.Close()
	waitErr := cp.Wait()

	if copyErr != nil {
		return 0, "", fmt.Errorf("streaming snapshot: %w", copyErr)
	}
	if encErr != nil {
		return 0, "", fmt.Errorf("finalizing zstd: %w", encErr)
	}
	if closeErr != nil {
		return 0, "", closeErr
	}
	if waitErr != nil {
		return 0, "", fmt.Errorf("docker cp failed: %s: %w", strings.TrimSpace(cpErr.String()), waitErr)
	}
	return counter.n, hex.EncodeToString(hasher.Sum(nil)), nil
}

// trapInterrupt runs onInterrupt once if SIGINT/SIGTERM arrives, then exits
// non-zero. Go's default disposition kills the process without running defers,
// so a long docker cp / inject would otherwise leave the db stopped or wiped.
// The returned func deregisters the handler on the normal (no-signal) path.
func trapInterrupt(onInterrupt func(os.Signal)) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	var once sync.Once
	go func() {
		s, ok := <-ch
		if !ok {
			return
		}
		onInterrupt(s)
		os.Exit(1)
	}()
	return func() {
		once.Do(func() {
			signal.Stop(ch)
			close(ch)
		})
	}
}

type SnapshotRequest struct {
	Service    string
	Driver     string
	Stack      string
	Name       string
	Force      bool
	WasRunning bool // restart afterwards only if it was running
}

func RunSnapshot(req SnapshotRequest, now time.Time) (SnapshotMeta, error) {
	var meta SnapshotMeta
	container := ContainerName(req.Driver, req.Service)
	serviceDir, err := GetPathToDbService(req.Service)
	if err != nil {
		return meta, err
	}

	archive, metaPath, err := SnapshotPaths(req.Service, req.Name)
	if err != nil {
		return meta, err
	}
	if !req.Force {
		if _, err := os.Stat(archive); err == nil {
			return meta, fmt.Errorf("snapshot %q already exists — use --force to overwrite", req.Name)
		}
	}
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		return meta, err
	}

	pgMajor, err := containerPgVersionMajor(container)
	if err != nil {
		return meta, fmt.Errorf("%s: nothing built to snapshot? %w", req.Service, err)
	}
	arch, err := dockerServerArch()
	if err != nil {
		return meta, err
	}
	image, err := composeImage(serviceDir)
	if err != nil {
		return meta, err
	}

	if err := stopContainerClean(container, 120); err != nil {
		return meta, err
	}
	if req.WasRunning {
		defer func() { _ = composeInDir(serviceDir, "start") }()
	}

	// stream to .tmp, write meta, then atomic rename so a partial never looks valid
	tmp := archive + ".tmp"
	stop := trapInterrupt(func(os.Signal) {
		_ = os.Remove(tmp)
		_ = os.Remove(metaPath)
		if req.WasRunning {
			_ = composeInDir(serviceDir, "start")
		}
		Infof("\n⚠️  snapshot %q interrupted — partial files removed, container restarted\n", req.Name)
	})
	defer stop()

	size, sum, err := writeSnapshotArchive(container, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return meta, err
	}
	meta = SnapshotMeta{
		Stack: req.Stack, Service: req.Service, PgVersionMajor: pgMajor,
		Image: image, Arch: arch, DataPath: PostgresDataDir,
		CreatedAt: now.UTC().Format(time.RFC3339), SizeBytes: size, SHA256: sum,
	}
	if err := WriteSnapshotMeta(metaPath, meta); err != nil {
		_ = os.Remove(tmp)
		_ = os.Remove(metaPath)
		return meta, err
	}
	if err := os.Rename(tmp, archive); err != nil {
		_ = os.Remove(tmp)
		_ = os.Remove(metaPath)
		return meta, err
	}
	return meta, nil
}

// probeArchive cheaply confirms the payload decompresses+tars, reading only a
// bounded prefix — run before any destructive wipe.
func probeArchive(archivePath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return fmt.Errorf("snapshot is not valid zstd: %w", err)
	}
	defer dec.Close()
	tr := tar.NewReader(dec)
	for range 5 {
		_, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("snapshot payload is corrupt/truncated: %w", err)
		}
	}
	return nil
}

func verifyArchiveSHA(archivePath, want string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch — snapshot may be corrupt (want %s, got %s)", want, got)
	}
	return nil
}

// Copies into the data dir's PARENT: the tar is rooted at data/, so it lands as
// .../postgresql/data — symmetric with how the snapshot copy was taken.
func injectArchive(container, archivePath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		return err
	}
	defer dec.Close()

	parent := filepath.Dir(PostgresDataDir)
	cp := exec.Command("docker", "cp", "-", container+":"+parent)
	stdin, err := cp.StdinPipe()
	if err != nil {
		return err
	}
	var cpErr bytes.Buffer
	cp.Stderr = &cpErr
	if err := cp.Start(); err != nil {
		return err
	}
	_, copyErr := io.Copy(stdin, dec)
	closeErr := stdin.Close()
	waitErr := cp.Wait()
	if copyErr != nil {
		return fmt.Errorf("streaming restore: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if waitErr != nil {
		return fmt.Errorf("docker cp restore failed: %s: %w", strings.TrimSpace(cpErr.String()), waitErr)
	}
	return nil
}

type RestoreRequest struct {
	Service     string
	Driver      string
	ArchivePath string
	MetaPath    string
	FromPath    bool // user passed an explicit (untrusted) path → full hash check
	Force       bool
}

// RunRestore validates everything BEFORE wiping, then down → up --no-start →
// inject → start (data must land before start so the image skips initdb).
func RunRestore(req RestoreRequest) error {
	serviceDir, err := GetPathToDbService(req.Service)
	if err != nil {
		return err
	}
	if _, err := os.Stat(req.ArchivePath); err != nil {
		return fmt.Errorf("snapshot archive not found: %s", req.ArchivePath)
	}
	meta, err := ReadSnapshotMeta(req.MetaPath)
	if err != nil {
		return fmt.Errorf("snapshot metadata missing/unreadable (%s) — both .tar.zst and .meta.json are required: %w", filepath.Base(req.MetaPath), err)
	}

	arch, err := dockerServerArch()
	if err != nil {
		return err
	}
	image, err := composeImage(serviceDir)
	if err != nil {
		return err
	}
	container := ContainerName(req.Driver, req.Service)
	targetPgMajor, _ := containerPgVersionMajor(container) // best-effort; "" skips the gate
	if cerr := CheckRestoreCompatibility(meta, image, arch, targetPgMajor); cerr != nil {
		if !req.Force {
			return cerr
		}
		Infof("⚠️  --force: ignoring %v\n", cerr)
	}

	if err := probeArchive(req.ArchivePath); err != nil {
		return err
	}
	// Integrity check runs for every restore — a corrupt named snapshot must be
	// caught BEFORE `compose down --volumes` wipes the live db.
	if err := verifyArchiveSHA(req.ArchivePath, meta.SHA256); err != nil {
		return err
	}

	// destructive from here: the db is wiped and unusable until inject+start succeed.
	// An interrupt after the wipe must leave a clear "re-restore needed" message
	// rather than dying silently with a half-wiped data dir.
	wiped := false
	stop := trapInterrupt(func(os.Signal) {
		if wiped {
			Infof("\n⚠️  restore of %q interrupted AFTER wipe — the db is empty; re-run `corgi db restore` to recover\n", req.Service)
		} else {
			Infof("\n⚠️  restore of %q interrupted before any change\n", req.Service)
		}
	})
	defer stop()

	if err := composeInDir(serviceDir, "down", "--volumes"); err != nil {
		return err
	}
	wiped = true
	if err := composeInDir(serviceDir, "up", "--no-start"); err != nil {
		return err
	}
	if err := injectArchive(container, req.ArchivePath); err != nil {
		return fmt.Errorf("restore failed after wipe — db needs another restore: %w", err)
	}
	if err := composeInDir(serviceDir, "start"); err != nil {
		return fmt.Errorf("snapshot injected but container failed to start: %w", err)
	}
	return nil
}
