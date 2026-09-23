package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/push"
)

type StackService struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Port      int        `json:"port,omitempty"`
	Status    string     `json:"status"`
	URL       string     `json:"url,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

const (
	stackTimeout = 3 * time.Minute
	stackTestMax = 25 * time.Minute
)

var stackServiceName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func composePathIn(root string) string {
	for _, name := range []string{"corgi-compose.yml", "corgi-compose.yaml"} {
		p := filepath.Join(root, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func corgiIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	self, err := os.Executable()
	if err != nil {
		self = "corgi"
	}
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CORGI_CI=1", "NO_COLOR=1")
	return cmd.CombinedOutput()
}

func stackSnapshot(ctx context.Context, root string) (services []StackService, running int, err error) {
	out, err := corgiIn(ctx, root, "ps", "--json")
	if err != nil && len(out) == 0 {
		return nil, 0, err
	}
	var rows []StackService
	if json.Unmarshal(out, &rows) != nil {
		var wrapped struct {
			Rows []StackService `json:"rows"`
		}
		if json.Unmarshal(out, &wrapped) == nil {
			rows = wrapped.Rows
		}
	}
	if rows == nil {
		rows = []StackService{}
	}
	for _, r := range rows {
		if r.Status == "running" {
			running++
		}
	}
	return rows, running, nil
}

func launchStackHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("workspace"))
		root, err := workspaceRoot(id)
		if err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		compose := composePathIn(root)
		if compose == "" {
			writeLaunchJSON(w, map[string]any{"workspace": id, "compose": false, "services": []StackService{}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		services, running, err := stackSnapshot(ctx, root)
		if err != nil {
			writeLaunchError(w, http.StatusBadGateway, "corgi ps did not answer: "+firstLineOf(err.Error()))
			return
		}
		writeLaunchJSON(w, map[string]any{"workspace": id, "compose": true, "path": filepath.Base(compose), "services": services, "running": running, "at": time.Now()})
	case http.MethodPost:
		var req struct {
			Workspace string   `json:"workspace"`
			Do        string   `json:"do"`
			Services  []string `json:"services"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		root, err := workspaceRoot(strings.TrimSpace(req.Workspace))
		if err != nil {
			writeLaunchError(w, http.StatusNotFound, err.Error())
			return
		}
		if composePathIn(root) == "" {
			writeLaunchError(w, http.StatusBadRequest, "no corgi-compose.yml in that workspace")
			return
		}
		var picked []string
		for _, s := range req.Services {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !stackServiceName.MatchString(s) {
				writeLaunchError(w, http.StatusBadRequest, "a service name is letters, digits, dots, dashes")
				return
			}
			picked = append(picked, s)
		}
		var args []string
		switch strings.TrimSpace(req.Do) {
		case "run", "start":
			args = []string{"run", "--detach", "--ci", "--logs"}
			if len(picked) > 0 {
				args = append(args, "--services", strings.Join(picked, ","))
			}
		case "stop":
			args = []string{"stop"}
			if len(picked) == 1 {
				args = append(args, "--service", picked[0])
			}
		case "restart":
			args = []string{"restart"}
		case "test", "e2e":
			go runStackTests(dir, req.Workspace, root, picked, req.Do == "e2e")
			writeLaunchJSON(w, map[string]any{"done": "running the " + map[bool]string{true: "e2e", false: "tests"}[req.Do == "e2e"] + " - a push says how they went", "workspace": req.Workspace})
			return
		default:
			writeLaunchError(w, http.StatusBadRequest, "do is run, stop, restart, test or e2e")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), stackTimeout)
		defer cancel()
		out, err := corgiIn(ctx, root, args...)
		tail := lastLines(string(out), 6)
		if err != nil {
			writeLaunchError(w, http.StatusBadGateway, "corgi "+args[0]+" failed: "+tail)
			return
		}
		services, running, _ := stackSnapshot(ctx, root)
		writeLaunchJSON(w, map[string]any{"done": map[string]string{"run": "started", "start": "started", "stop": "stopped", "restart": "restarted"}[strings.TrimSpace(req.Do)], "output": tail, "services": services, "running": running})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET ?workspace=<id> for the stack, POST {workspace, do, services} to change it")
	}
}

func runStackTests(agentDir, workspace, root string, services []string, e2e bool) {
	ctx, cancel := context.WithTimeout(context.Background(), stackTestMax)
	defer cancel()
	args := []string{"test"}
	what := "tests"
	if e2e {
		args = append(args, "--e2e")
		what = "e2e"
	} else if len(services) == 1 {
		args = append(args, "--service", services[0])
	}
	started := time.Now()
	out, err := corgiIn(ctx, root, args...)
	body := what + " passed in " + shortSince(started)
	if err != nil {
		body = what + " failed after " + shortSince(started) + ": " + lastLines(string(out), 3)
	}
	utils.Notify("corgi · "+workspace, body)
	store := push.Load(agentDir)
	_ = store.Send(context.Background(), push.Message{Title: "corgi · " + workspace, Body: body, Category: "inbox", Data: map[string]string{"workspace": workspace, "needs": map[bool]string{true: "1", false: ""}[err != nil]}, Thread: workspace})
}

func shortSince(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return d.Round(time.Minute).String()
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
