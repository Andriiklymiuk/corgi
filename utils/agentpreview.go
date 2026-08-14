package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// A live preview is a public URL onto a service the agent is editing, so the
// user can watch it change from a phone. corgi does not need a refresh
// mechanism — the dev server already hot reloads. corgi needs to keep one
// tunnel open and be honest about what state the build is in.
//
// The tunnel runs as a DETACHED process writing to a log file, the same shape
// corgi already uses for detached services. That way a preview outlives the
// session that started it, and a later corgi run can still find and reap it.

// PreviewState is what to show over the webview. A visible banner beats a
// white screen, so "broken" is a first-class state rather than an absence.
type PreviewState string

const (
	PreviewStarting PreviewState = "starting" // tunnel spawned, no URL yet
	PreviewReady    PreviewState = "ready"    // URL published and the port answers
	PreviewBroken   PreviewState = "broken"   // URL published but the service does not answer
	PreviewStopped  PreviewState = "stopped"  // torn down
)

// DefaultPreviewIdleMinutes is how long a preview survives without being
// looked at. A forgotten preview is a public URL onto seeded data.
const DefaultPreviewIdleMinutes = 20

// Preview is one live preview.
type Preview struct {
	ID            string       `json:"id"`
	Workspace     string       `json:"workspace"`
	Service       string       `json:"service"`
	Branch        string       `json:"branch,omitempty"`
	Port          int          `json:"port"`
	URL           string       `json:"url,omitempty"`
	State         PreviewState `json:"state"`
	Error         string       `json:"error,omitempty"`
	Frozen        bool         `json:"frozen,omitempty"`
	PID           int          `json:"pid,omitempty"`
	LogFile       string       `json:"logFile,omitempty"`
	StartedAt     time.Time    `json:"startedAt"`
	LastTouched   time.Time    `json:"lastTouched"`
	IdleMinutes   int          `json:"idleMinutes"`
	TunnelIsQuick bool         `json:"quickTunnel,omitempty"`
}

// Expired reports whether the preview has gone unlooked-at for too long.
// A frozen preview never expires: freezing means someone is reading it.
func (p Preview) Expired(now time.Time) bool {
	if p.Frozen || p.IdleMinutes <= 0 {
		return false
	}
	return now.Sub(p.LastTouched) > time.Duration(p.IdleMinutes)*time.Minute
}

// PreviewStore is the on-disk set of live previews, so a preview started by one
// corgi process can be found and reaped by another.
type PreviewStore struct {
	Previews []Preview `json:"previews"`
}

func previewStorePath(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", "previews.json")
}

// PreviewDir holds preview logs.
func PreviewDir(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".previews")
}

// LoadPreviews reads the store, returning an empty one when absent.
func LoadPreviews(composeDir string) (*PreviewStore, error) {
	data, err := os.ReadFile(previewStorePath(composeDir))
	if os.IsNotExist(err) {
		return &PreviewStore{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s PreviewStore
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SavePreviews writes the store with the tmp-write plus rename discipline the
// rest of corgi uses.
func SavePreviews(composeDir string, s *PreviewStore) error {
	path := previewStorePath(composeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	EnsureCorgiServicesIgnore(filepath.Dir(path), "previews.json")
	EnsureCorgiServicesIgnore(filepath.Dir(path), ".previews/")

	sort.Slice(s.Previews, func(i, j int) bool { return s.Previews[i].ID < s.Previews[j].ID })
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// PreviewOptions configures a new preview.
type PreviewOptions struct {
	ComposeDir string
	Workspace  string
	Service    string
	Branch     string
	Port       int
	Provider   string // cloudflared | ngrok | localtunnel
	// NamedTunnel records that the service declares a named tunnel in its
	// `tunnel:` block. corgi tunnel has no flag for this — it is compose
	// configuration — but it is what keeps the URL stable across a restart, so
	// the preview reports which kind the user has.
	NamedTunnel bool
	IdleMinutes int
	// Sensitive refuses to open a public tunnel at all. Set from the
	// workspace's committed config, which may restrict but never relax.
	Sensitive bool
	// CorgiBin is the binary to re-invoke for the tunnel. Defaults to the
	// running executable.
	CorgiBin string
}

// StartPreview opens a tunnel to a service's port and records the preview.
//
// It returns as soon as the tunnel process is spawned — the URL appears
// asynchronously, so callers poll PreviewStatus. That matters because this is
// reached through an MCP tool, and MCP handlers must never block.
func StartPreview(opts PreviewOptions) (*Preview, error) {
	if opts.Sensitive {
		return nil, fmt.Errorf(
			"workspace %s is marked sensitive, so it never opens a public tunnel — "+
				"use corgi_diff, which needs no tunnel", opts.Workspace)
	}
	if opts.Service == "" {
		return nil, fmt.Errorf("service is required")
	}
	if opts.Port <= 0 {
		return nil, fmt.Errorf("service %s has no port to tunnel", opts.Service)
	}
	if opts.IdleMinutes <= 0 {
		opts.IdleMinutes = DefaultPreviewIdleMinutes
	}

	store, err := LoadPreviews(opts.ComposeDir)
	if err != nil {
		return nil, err
	}
	// Reuse a live preview for the same service: a second tunnel would hand the
	// user a different URL for the same thing.
	for i := range store.Previews {
		p := &store.Previews[i]
		if p.Service == opts.Service && previewProcessAlive(*p) {
			p.LastTouched = time.Now().UTC()
			_ = SavePreviews(opts.ComposeDir, store)
			refreshPreviewFromLog(p)
			return p, nil
		}
	}

	bin := opts.CorgiBin
	if bin == "" {
		if exe, exeErr := os.Executable(); exeErr == nil {
			bin = exe
		} else {
			bin = "corgi"
		}
	}

	logDir := PreviewDir(opts.ComposeDir)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	id := previewID(opts.Service, opts.Branch)
	logFile := filepath.Join(logDir, id+".log")

	proc, err := spawnDetachedTunnel(bin, opts, logFile)
	if err != nil {
		return nil, err
	}

	p := Preview{
		ID:            id,
		Workspace:     opts.Workspace,
		Service:       opts.Service,
		Branch:        opts.Branch,
		Port:          opts.Port,
		State:         PreviewStarting,
		PID:           proc.Pid,
		LogFile:       logFile,
		StartedAt:     time.Now().UTC(),
		LastTouched:   time.Now().UTC(),
		IdleMinutes:   opts.IdleMinutes,
		TunnelIsQuick: !opts.NamedTunnel,
	}
	store.Previews = append(store.Previews, p)
	if err := SavePreviews(opts.ComposeDir, store); err != nil {
		return nil, err
	}
	return &p, nil
}

// spawnDetachedTunnel runs `corgi tunnel` in its own process group, writing to
// a log file, so the preview survives the process that asked for it.
func spawnDetachedTunnel(bin string, opts PreviewOptions, logFile string) (*os.Process, error) {
	// `corgi tunnel` takes only --port and --provider. A named tunnel is
	// configured in the service's `tunnel:` block in corgi-compose.yml, not on
	// the command line: passing invented flags produced a process that exited
	// immediately with "unknown flag", leaving a preview reporting "starting"
	// against an already-dead pid.
	args := []string{"tunnel", opts.Service}
	if opts.Provider != "" {
		args = append(args, "--provider", opts.Provider)
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cmd := exec.Command(bin, args...)
	cmd.Dir = opts.ComposeDir
	cmd.Stdout = f
	cmd.Stderr = f
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not start tunnel: %w", err)
	}

	// Reap the child when it exits. Without this a killed tunnel becomes a
	// zombie for as long as the spawning process lives, and a zombie still
	// answers kill(pid, 0) — so the preview would read as alive forever and
	// could never be reaped or restarted. Harmless in a short-lived CLI run;
	// essential inside `corgi agent serve`.
	go func() { _ = cmd.Wait() }()

	return cmd.Process, nil
}

// PreviewStatus returns the current state of one preview, refreshed from its
// log and a probe of the local port.
func PreviewStatus(composeDir, id string) (*Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		p := &store.Previews[i]
		if p.ID != id && p.Service != id {
			continue
		}
		refreshPreviewFromLog(p)
		p.LastTouched = time.Now().UTC()
		_ = SavePreviews(composeDir, store)
		return p, nil
	}
	return nil, fmt.Errorf("no preview called %q", id)
}

// ListPreviews returns every recorded preview, refreshed.
func ListPreviews(composeDir string) ([]Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		refreshPreviewFromLog(&store.Previews[i])
	}
	return store.Previews, nil
}

// FreezePreview pins a preview so idle reaping leaves it alone. Freezing means
// someone is actually looking at it.
func FreezePreview(composeDir, id string, frozen bool) (*Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		p := &store.Previews[i]
		if p.ID != id && p.Service != id {
			continue
		}
		p.Frozen = frozen
		p.LastTouched = time.Now().UTC()
		refreshPreviewFromLog(p)
		if err := SavePreviews(composeDir, store); err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, fmt.Errorf("no preview called %q", id)
}

// StopPreview tears one down.
func StopPreview(composeDir, id string) error {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return err
	}
	for i := range store.Previews {
		if store.Previews[i].ID != id && store.Previews[i].Service != id {
			continue
		}
		killPreview(store.Previews[i])
		store.Previews = append(store.Previews[:i], store.Previews[i+1:]...)
		return SavePreviews(composeDir, store)
	}
	return fmt.Errorf("no preview called %q", id)
}

// ReapPreviews tears down previews that have gone idle or whose process is
// gone, and returns what it removed. Safe to call on every corgi invocation.
func ReapPreviews(composeDir string, now time.Time) ([]Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	var kept, reaped []Preview
	for _, p := range store.Previews {
		if !previewProcessAlive(p) {
			reaped = append(reaped, p)
			continue
		}
		if p.Expired(now) {
			killPreview(p)
			p.State = PreviewStopped
			reaped = append(reaped, p)
			continue
		}
		kept = append(kept, p)
	}
	if len(reaped) == 0 {
		return nil, nil
	}
	store.Previews = kept
	if err := SavePreviews(composeDir, store); err != nil {
		return reaped, err
	}
	return reaped, nil
}

func killPreview(p Preview) {
	if p.PID <= 0 {
		return
	}
	// Kill the group: the tunnel CLI is a child of the corgi tunnel process.
	if err := KillProcessGroup(p.PID); err != nil {
		if proc, ferr := os.FindProcess(p.PID); ferr == nil {
			_ = proc.Kill()
		}
	}
}

func previewProcessAlive(p Preview) bool {
	if p.PID <= 0 {
		return false
	}
	return PidAlive(p.PID, "")
}

// anyTunnelURL matches the public URL any of corgi's providers print.
var anyTunnelURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.(trycloudflare\.com|ngrok(-free)?\.app|ngrok\.io|loca\.lt)[^\s"']*`)

// refreshPreviewFromLog re-reads the tunnel's log for a URL and probes the
// local port, so state reflects reality rather than what was true at start.
func refreshPreviewFromLog(p *Preview) {
	if !previewProcessAlive(*p) {
		p.State = PreviewStopped
		if p.Error == "" {
			p.Error = "tunnel process is no longer running"
		}
		return
	}
	if p.URL == "" && p.LogFile != "" {
		if data, err := os.ReadFile(p.LogFile); err == nil {
			if url := anyTunnelURL.FindString(string(data)); url != "" {
				p.URL = strings.TrimRight(url, ".,)")
			}
		}
	}
	if p.URL == "" {
		p.State = PreviewStarting
		return
	}
	// The tunnel is up; whether the page works depends on the dev server behind
	// it. Report broken rather than handing over a URL that shows a stack trace.
	if IsPortListening(p.Port) {
		p.State = PreviewReady
		p.Error = ""
	} else {
		p.State = PreviewBroken
		p.Error = fmt.Sprintf("nothing is listening on port %d yet — the service may still be building", p.Port)
	}
}

// previewID is stable for a service and branch, so re-asking for a preview of
// the same thing finds the existing one.
func previewID(service, branch string) string {
	id := service
	if branch != "" {
		id += "@" + strings.NewReplacer("/", "-").Replace(branch)
	}
	return id
}
