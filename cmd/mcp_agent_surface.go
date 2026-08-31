package cmd

import (
	"fmt"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type contextArgs struct {
	ComposePath string `json:"composePath"`
	NoGit       bool   `json:"noGit"`
}

type whyArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	LogLines    int    `json:"logLines"`
}

type waitForLogArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Pattern     string `json:"pattern"`
	TimeoutSec  int    `json:"timeoutSec"`
}

type checkoutArgs struct {
	ComposePath string `json:"composePath"`
	Branch      string `json:"branch"`
	AllowDirty  bool   `json:"allowDirty"`
}

type checkpointArgs struct {
	ComposePath string `json:"composePath"`
	Name        string `json:"name"`
}

func registerAgentSurfaceTools(s *server.MCPServer, composeOpt mcp.ToolOption) {
	s.AddTool(mcp.NewTool("corgi_context",
		mcp.WithDescription("One call to orient: every service and db_service with port, status and repo state (branch, dirty, ahead/behind), the active env tier, declared profiles, and validation findings."),
		composeOpt,
		mcp.WithBoolean("noGit", mcp.Description("Skip per-repo git state (faster on a big workspace)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpContext(contextArgs{
			ComposePath: r.GetString("composePath", ""),
			NoGit:       r.GetBool("noGit", false),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_why",
		mcp.WithDescription("Explain why one service is not up: unmet dependencies, who owns its port, last exit code, missing or unresolved env, and its last log lines. Returns a single verdict to branch on."),
		composeOpt,
		mcp.WithString("service", mcp.Required(), mcp.Description("Service name from corgi-compose.yml")),
		mcp.WithNumber("logLines", mcp.Description("How many trailing log lines to include (default 8)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWhy(whyArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			LogLines:    int(r.GetFloat("logLines", 8)),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_wait_for_log",
		mcp.WithDescription("Block until a service's log matches a regexp, then return the matching line. Use instead of polling corgi_logs on a timer."),
		composeOpt,
		mcp.WithString("service", mcp.Required(), mcp.Description("Service name whose log to watch")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Go regexp the line must match")),
		mcp.WithNumber("timeoutSec", mcp.Description("Give up after this many seconds (default 60)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWaitForLog(waitForLogArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Pattern:     r.GetString("pattern", ""),
			TimeoutSec:  int(r.GetFloat("timeoutSec", 60)),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_checkout",
		mcp.WithDescription("Put the workspace repo and every service repo on a branch and fast-forward it. A repo without that branch falls back to its own default branch. Dirty repos are skipped, never clobbered."),
		composeOpt,
		mcp.WithString("branch", mcp.Description("Branch to check out; empty means each repo's own default branch")),
		mcp.WithBoolean("allowDirty", mcp.Description("Attempt the switch even with uncommitted changes")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpCheckout(checkoutArgs{
			ComposePath: r.GetString("composePath", ""),
			Branch:      r.GetString("branch", ""),
			AllowDirty:  r.GetBool("allowDirty", false),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_checkpoint",
		mcp.WithDescription("Record every repo's branch, HEAD and uncommitted work under one name, so a cross-repo change can be undone with corgi_restore."),
		composeOpt,
		mcp.WithString("name", mcp.Description("Checkpoint name; defaults to a timestamp")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpCheckpoint(checkpointArgs{
			ComposePath: r.GetString("composePath", ""),
			Name:        r.GetString("name", ""),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_restore",
		mcp.WithDescription("Put every repo back to a checkpoint. Uncommitted work present now is captured under a safety checkpoint first, whose name is returned."),
		composeOpt,
		mcp.WithString("name", mcp.Required(), mcp.Description("Checkpoint name from corgi_checkpoint")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpRestore(checkpointArgs{
			ComposePath: r.GetString("composePath", ""),
			Name:        r.GetString("name", ""),
		})
	}))
}

func mcpContext(args contextArgs) (contextReport, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return contextReport{}, composeLoadError(err)
	}
	previous := contextNoGit
	contextNoGit = args.NoGit
	defer func() { contextNoGit = previous }()
	return buildContextReport(corgi), nil
}

func mcpWhy(args whyArgs) (whyReport, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return whyReport{}, composeLoadError(err)
	}
	service, found := findServiceByName(corgi, args.Service)
	if !found {
		return whyReport{}, fmt.Errorf("%s: no service named %q in corgi-compose.yml", utils.ErrServiceNotFound, args.Service)
	}
	previous := whyLogLines
	if args.LogLines > 0 {
		whyLogLines = args.LogLines
	}
	defer func() { whyLogLines = previous }()
	return diagnoseService(corgi, service), nil
}

type waitForLogResult struct {
	Service  string `json:"service"`
	Matched  bool   `json:"matched"`
	Line     string `json:"line,omitempty"`
	WaitedMs int64  `json:"waitedMs"`
}

func mcpWaitForLog(args waitForLogArgs) (waitForLogResult, error) {
	if _, err := loadComposeForMCP(args.ComposePath); err != nil {
		return waitForLogResult{}, composeLoadError(err)
	}
	if args.Pattern == "" {
		return waitForLogResult{}, fmt.Errorf("%s: pattern is required", utils.ErrUsage)
	}
	timeout := time.Duration(args.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Minute
	}
	started := time.Now()
	line, matched, err := utils.WaitForLogLine(logsBase(), utils.LogWait{
		Service: args.Service,
		Pattern: args.Pattern,
		Timeout: timeout,
	})
	if err != nil {
		return waitForLogResult{}, err
	}
	return waitForLogResult{
		Service:  args.Service,
		Matched:  matched,
		Line:     line,
		WaitedMs: time.Since(started).Milliseconds(),
	}, nil
}

func mcpCheckout(args checkoutArgs) ([]utils.RepoCheckout, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	results := []utils.RepoCheckout{}
	handled := map[string]string{}
	previousDirty := checkoutAllowDirty
	checkoutAllowDirty = args.AllowDirty
	defer func() { checkoutAllowDirty = previousDirty }()
	for _, target := range checkpointTargets(corgi) {
		results = append(results, checkoutOne(target, args.Branch, handled))
	}
	return results, nil
}

func mcpCheckpoint(args checkpointArgs) (checkpointFile, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return checkpointFile{}, composeLoadError(err)
	}
	name := args.Name
	if name == "" {
		name = utils.DefaultSnapshotName(time.Now())
	}
	if !checkpointNameRe.MatchString(name) {
		return checkpointFile{}, fmt.Errorf("%s: a checkpoint name may hold only letters, digits, dot, dash and underscore", utils.ErrUsage)
	}
	file := checkpointFile{Name: name, CreatedAt: time.Now().UTC()}
	for _, target := range checkpointTargets(corgi) {
		if repo, ok := captureRepo(target, name); ok {
			file.Repos = append(file.Repos, repo)
		}
	}
	if len(file.Repos) == 0 {
		return checkpointFile{}, fmt.Errorf("%s: no git repositories to checkpoint", utils.ErrConfig)
	}
	if err := writeCheckpoint(file); err != nil {
		return checkpointFile{}, err
	}
	return file, nil
}

type restoreResult struct {
	Checkpoint string   `json:"checkpoint"`
	Safety     string   `json:"safetyCheckpoint,omitempty"`
	Restored   []string `json:"restored"`
	Failed     []string `json:"failed,omitempty"`
}

func mcpRestore(args checkpointArgs) (restoreResult, error) {
	if _, err := loadComposeForMCP(args.ComposePath); err != nil {
		return restoreResult{}, composeLoadError(err)
	}
	file, err := readCheckpoint(args.Name)
	if err != nil {
		return restoreResult{}, err
	}
	result := restoreResult{Checkpoint: file.Name, Restored: []string{}}
	if len(reposAtRisk(file.Repos)) > 0 {
		result.Safety = "restore-" + utils.DefaultSnapshotName(time.Now())
		if err := saveSafetyCheckpoint(file.Repos, result.Safety); err != nil {
			return restoreResult{}, fmt.Errorf("nothing restored, the safety checkpoint failed: %v", err)
		}
	}
	for _, repo := range file.Repos {
		if err := utils.RestoreWorkTree(repo.Path, repo.Branch, repo.Head, repo.StashSha); err != nil {
			result.Failed = append(result.Failed, repo.Name+": "+err.Error())
			continue
		}
		result.Restored = append(result.Restored, repo.Name)
	}
	return result, nil
}
