package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
)

// defaultMCPCallBudget is how long one tool call may run. Claude.ai and
// Desktop drop a call at 240 s; 200 s leaves room for the network and the
// envelope. Claude Code reads MCP_TOOL_TIMEOUT instead, so the budget is
// also what stdio callers get unless CORGI_MCP_CALL_BUDGET says otherwise.
const (
	defaultMCPCallBudget = 200 * time.Second
	minMCPCallBudget     = time.Second
	mcpUpWait            = 20 * time.Second
)

func mcpCallBudget() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("CORGI_MCP_CALL_BUDGET")); err == nil && d >= minMCPCallBudget {
		return d
	}
	return defaultMCPCallBudget
}

// clampToBudget bounds a caller-chosen wait; clamped reports that it did.
func clampToBudget(d time.Duration) (time.Duration, bool) {
	budget := mcpCallBudget()
	if d <= 0 || d > budget {
		return budget, d > budget
	}
	return d, false
}

// upLaunch is what corgi_up returns: the run-state when the child finished
// inside the wait, a handle to poll otherwise.
type upLaunch struct {
	Status string          `json:"status"`
	Handle *upHandle       `json:"handle,omitempty"`
	Next   string          `json:"next,omitempty"`
	State  *utils.RunState `json:"state,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type upHandle struct {
	PID     int    `json:"pid"`
	LogPath string `json:"logPath"`
}

const (
	upStatusStarting = "starting"
	upStatusStarted  = "started"
	upStatusFailed   = "failed"
	upNextPoll       = "poll corgi_status until healthy; corgi_logs reads each service"
)

// mcpUpChildCommand builds the child that boots the stack. A variable so a
// test can stand in a shell command for the real binary.
var mcpUpChildCommand = func(args upArgs, composePath, logPath string) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	argv := []string{"run", "--detach", "--ci", "--logs", "-f", composePath}
	if args.Profile != "" {
		argv = append(argv, "--profile", args.Profile)
	}
	if args.Omit != "" {
		argv = append(argv, "--omit", args.Omit)
	}
	if args.Seed {
		argv = append(argv, "--seed")
	}
	if args.ServiceDir != "" {
		argv = append(argv, "--service-dir", args.ServiceDir)
	}
	if args.ServiceBranch != "" {
		argv = append(argv, "--service-branch", args.ServiceBranch)
	}
	cmd := exec.Command(self, argv...)
	cmd.Dir = filepath.Dir(composePath)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	return cmd, nil
}

// mcpUpInFlight remembers the child booting each compose dir, so a second
// corgi_up during a cold boot gets the same handle instead of a second boot.
var (
	mcpUpInFlightMu sync.Mutex
	mcpUpInFlight   = map[string]upHandle{}
)

// mcpUpDetached starts the stack in a child process and waits at most wait
// for it. The child owns every beforeStart, so a cold boot never holds the
// tool call (or the handler lock) for minutes.
func mcpUpDetached(args upArgs, wait time.Duration) (upLaunch, error) {
	ctx, err := loadComposeCtx(args.ComposePath)
	if err != nil {
		return upLaunch{}, composeLoadError(err)
	}
	composePath, composeDir := utils.CorgiComposePath, utils.CorgiComposePathDir
	ctx.cleanup()

	statePath := utils.RunStatePath(composeDir)
	if isAlreadyRunning(statePath) {
		return upLaunch{}, fmt.Errorf(errFmt, utils.ErrAlreadyRunning, "corgi is already running for this project — call corgi_down first")
	}
	mcpUpInFlightMu.Lock()
	if h, ok := mcpUpInFlight[composeDir]; ok && utils.PidAlive(h.PID, "") {
		mcpUpInFlightMu.Unlock()
		return upLaunch{Status: upStatusStarting, Handle: &h, Next: upNextPoll}, nil
	}
	delete(mcpUpInFlight, composeDir)
	mcpUpInFlightMu.Unlock()

	servicesDir := utils.CorgiServicesIn(composeDir)
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		return upLaunch{}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	utils.EnsureCorgiServicesIgnore(servicesDir, "mcp-up-*.log")
	logPath := filepath.Join(servicesDir, fmt.Sprintf("mcp-up-%d.log", time.Now().Unix()))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return upLaunch{}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	defer logFile.Close()

	cmd, err := mcpUpChildCommand(args, composePath, logPath)
	if err != nil {
		return upLaunch{}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	utils.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return upLaunch{}, fmt.Errorf("%s: could not start corgi run: %v", utils.ErrExecFailed, err)
	}
	handle := upHandle{PID: cmd.Process.Pid, LogPath: logPath}
	mcpUpInFlightMu.Lock()
	mcpUpInFlight[composeDir] = handle
	mcpUpInFlightMu.Unlock()

	// The child is reaped here whenever it ends; the goroutine lives exactly
	// as long as the child, not the request.
	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		mcpUpInFlightMu.Lock()
		if mcpUpInFlight[composeDir].PID == handle.PID {
			delete(mcpUpInFlight, composeDir)
		}
		mcpUpInFlightMu.Unlock()
	}()

	select {
	case err := <-exited:
		mcpCache.invalidateStatus()
		if err != nil {
			return upLaunch{Status: upStatusFailed, Handle: &handle, Error: lastLogLines(logPath, 12)}, nil
		}
		state, readErr := utils.ReadRunState(statePath)
		if readErr != nil {
			return upLaunch{Status: upStatusFailed, Handle: &handle, Error: "corgi run finished but wrote no run-state: " + lastLogLines(logPath, 12)}, nil
		}
		return upLaunch{Status: upStatusStarted, Handle: &handle, State: &state, Next: upNextPoll}, nil
	case <-time.After(wait):
		return upLaunch{Status: upStatusStarting, Handle: &handle, Next: upNextPoll}, nil
	}
}

func lastLogLines(path string, n int) string {
	lines, err := tailLogFile(path, n)
	if err != nil || len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
