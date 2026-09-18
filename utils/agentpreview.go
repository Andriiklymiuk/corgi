package utils

import (
	"andriiklymiuk/corgi/utils/atomicfile"
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

type PreviewState string

const (
	PreviewStarting PreviewState = "starting"
	PreviewReady    PreviewState = "ready"
	PreviewBroken   PreviewState = "broken"
	PreviewStopped  PreviewState = "stopped"
)

const DefaultPreviewIdleMinutes = 20

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

func (p Preview) Expired(now time.Time) bool {
	if p.Frozen || p.IdleMinutes <= 0 {
		return false
	}
	return now.Sub(p.LastTouched) > time.Duration(p.IdleMinutes)*time.Minute
}

type PreviewStore struct {
	Previews []Preview `json:"previews"`
}

func previewStorePath(composeDir string) string {
	return filepath.Join(CorgiServicesIn(composeDir), "previews.json")
}

func PreviewDir(composeDir string) string {
	return filepath.Join(CorgiServicesIn(composeDir), ".previews")
}

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
	return atomicfile.Write(path, data, 0o644)
}

type PreviewOptions struct {
	ComposeDir  string
	Workspace   string
	Service     string
	Branch      string
	Port        int
	Provider    string
	NamedTunnel bool
	IdleMinutes int
	Sensitive   bool
	CorgiBin    string
}

// Returns once the tunnel is spawned; MCP handlers must never block.
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
	wantID := previewID(opts.Service, opts.Branch)
	for i := range store.Previews {
		if store.Previews[i].ID != wantID || !previewProcessAlive(store.Previews[i]) {
			continue
		}
		store.Previews[i].LastTouched = time.Now().UTC()
		if err := SavePreviews(opts.ComposeDir, store); err != nil {
			return nil, err
		}
		return refreshedByID(opts.ComposeDir, wantID)
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

func spawnDetachedTunnel(bin string, opts PreviewOptions, logFile string) (*os.Process, error) {
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

	go func() { _ = cmd.Wait() }()

	return cmd.Process, nil
}

func refreshedByID(composeDir, id string) (*Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		if store.Previews[i].ID == id {
			p := store.Previews[i]
			refreshPreviewFromLog(&p)
			return &p, nil
		}
	}
	return nil, fmt.Errorf("no preview called %q", id)
}

func PreviewStatus(composeDir, id string) (*Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		if store.Previews[i].ID != id && store.Previews[i].Service != id {
			continue
		}
		refreshPreviewFromLog(&store.Previews[i])
		store.Previews[i].LastTouched = time.Now().UTC()
		found := store.Previews[i]
		_ = SavePreviews(composeDir, store)
		return &found, nil
	}
	return nil, fmt.Errorf("no preview called %q", id)
}

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

func FreezePreview(composeDir, id string, frozen bool) (*Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	for i := range store.Previews {
		if store.Previews[i].ID != id && store.Previews[i].Service != id {
			continue
		}
		store.Previews[i].Frozen = frozen
		store.Previews[i].LastTouched = time.Now().UTC()
		refreshPreviewFromLog(&store.Previews[i])
		found := store.Previews[i]
		if err := SavePreviews(composeDir, store); err != nil {
			return nil, err
		}
		return &found, nil
	}
	return nil, fmt.Errorf("no preview called %q", id)
}

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

func ReapPreviews(composeDir string, now time.Time) ([]Preview, error) {
	store, err := LoadPreviews(composeDir)
	if err != nil {
		return nil, err
	}
	var kept, reaped []Preview
	for _, p := range store.Previews {
		if !previewProcessAlive(p) {
			p.State = PreviewStopped
			p.URL = ""
			p.Error = "tunnel process is no longer running"
			reaped = append(reaped, p)
			continue
		}
		if p.Expired(now) {
			killPreview(p)
			p.State = PreviewStopped
			p.URL = ""
			p.Error = "torn down after going unwatched"
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

var anyTunnelURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.(trycloudflare\.com|ngrok(-free)?\.app|ngrok\.io|loca\.lt)[^\s"']*`)

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
	if IsPortListening(p.Port) {
		p.State = PreviewReady
		p.Error = ""
	} else {
		p.State = PreviewBroken
		p.Error = fmt.Sprintf("nothing is listening on port %d yet — the service may still be building", p.Port)
	}
}

func previewID(service, branch string) string {
	id := service
	if branch != "" {
		id += "@" + strings.NewReplacer("/", "-").Replace(branch)
	}
	return id
}
