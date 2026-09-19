package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func botRunKey(bot, eventKey string) string { return "bot:" + bot + ":" + eventKey }

func BotRunPrompt(b bots.Bot, e watch.Event) string {
	base := fixPrompt(e)
	if base == "" {
		base = fmt.Sprintf("%s on %s: %s", e.Kind, e.Ref, e.Title)
		if strings.TrimSpace(e.Body) != "" {
			base += "\n\n" + e.Body
		}
		if e.URL != "" {
			base += "\n" + e.URL
		}
	}
	return base + "\n\nYou are " + b.Display() + ". Do what your role says and nothing beyond it. End with one line that says what you did."
}

func (d *Daemon) startBots(ctx context.Context, spec WatchSpec, e watch.Event) {
	store, err := bots.Load(bots.Path(d.Dir))
	if err != nil {
		return
	}
	for _, b := range store.Bots {
		if b.Workspace != spec.Workspace || !b.RunsOn(string(e.Kind)) {
			continue
		}
		if reason := fixDeferral(spec, d.watchState.Fixes, time.Now()); reason != "" {
			utils.Infof("agent: bot %s not run on %s: %s\n", b.Name, e.Ref, reason)
			continue
		}
		bot := b
		d.runs.Add(1)
		go func() {
			defer d.runs.Done()
			d.runBot(ctx, spec, bot, e)
		}()
	}
}

func (d *Daemon) runBot(ctx context.Context, spec WatchSpec, b bots.Bot, e watch.Event) {
	defer d.takeSlot(spec.Workspace)()
	ctx, cancel := context.WithTimeout(ctx, botTimeout)
	defer cancel()

	key := botRunKey(b.Name, e.Key)
	logDir := filepath.Join(d.Dir, "watch", "runs")
	_ = os.MkdirAll(logDir, 0o700)
	logPath := filepath.Join(logDir, safeName(key)+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		utils.Infof("agent: bot %s on %s: %v\n", b.Name, e.Ref, err)
		return
	}
	defer logFile.Close()

	run := e
	run.Key = key
	d.watchState.Fixes.StartFor(run, time.Now())
	d.watchState.Fixes.SetBot(key, b.Name)
	fmt.Fprintf(logFile, "=== %s bot %s on %s %s\n", time.Now().Format(time.RFC3339), b.Name, e.Kind, e.Ref)

	env := headlessEnv(botConfigDir(spec, b), os.Getenv("CORGI_OMIT"))
	prompt := BotRunPrompt(b, e)
	dir := spec.Dir
	if b.Isolate && d.Isolate != nil {
		branch := FixBranch(e.Ref)
		if trees, err := d.Isolate(spec.Dir, branch); err == nil {
			d.watchState.Fixes.SetBranch(key, branch)
			prompt += IsolationNote(branch, trees)
			fmt.Fprintf(logFile, "=== worktrees on %s: %s\n", branch, strings.Join(trees, ", "))
		} else {
			fmt.Fprintf(logFile, "=== could not isolate, running in place: %v\n", err)
		}
	}
	attempt := botRun{spec: spec, bot: b, prompt: prompt, dir: dir, env: env, log: logFile}
	out, receipt, runErr := d.botAttempt(ctx, attempt, b.Model)
	cost, tokens := receipt.costUSD, receipt.tokens
	if runErr != nil && ctx.Err() == nil {
		if next := nextModel(b.Model); next != "" {
			fmt.Fprintf(logFile, "\n=== failed: %v — trying again on %s\n", runErr, next)
			d.watchState.Fixes.SetRetry(key, next)
			var again runReceipt
			out, again, runErr = d.botAttempt(ctx, attempt, next)
			cost, tokens = cost+again.costUSD, tokens+again.tokens
		}
	}
	if cost > 0 || tokens > 0 {
		fmt.Fprintf(logFile, "\n=== cost: $%.4f · %d tokens\n", cost, tokens)
		d.watchState.Fixes.SetCost(key, cost, tokens)
	}
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		d.watchState.Fixes.Finish(key, nil, "", runErr.Error(), time.Now())
		d.learn(spec, "bot "+b.Name, "failed on "+e.Ref+": "+lastLine(string(out))+" ("+runErr.Error()+")")
		if e.Kind == watch.KindRoutine {
			d.routineReport(spec, e, string(out), runErr)
			return
		}
		go d.notifyAttentionAt(notifyTitlePrefix+b.Display(), fmt.Sprintf("%s on %s failed: %v — log: %s", b.Display(), e.Ref, runErr, logPath), spec.Workspace, e.URL)
		return
	}
	links := uniqueStrings(prLink.FindAllString(string(out), -1))
	note := clipText(lastLine(string(out)), 200)
	d.watchState.Fixes.Finish(key, links, note, "", time.Now())
	if e.Kind == watch.KindRoutine {
		d.routineReport(spec, e, string(out), nil)
		return
	}
	body := b.Display() + " on " + e.Ref
	if note != "" {
		body += " — " + note
	}
	target := e.URL
	if len(links) > 0 {
		target = links[0]
	}
	go d.notifyAttentionAt(notifyTitlePrefix+b.Display(), body, spec.Workspace, target)
}

type botRun struct {
	spec   WatchSpec
	bot    bots.Bot
	prompt string
	dir    string
	env    []string
	log    *os.File
}

func (d *Daemon) botAttempt(ctx context.Context, run botRun, model string) ([]byte, runReceipt, error) {
	args := botArgs(run, model)
	cmd := claudeCommand(ctx, run.dir, run.env, args...)
	cmd.Stdin = nil
	raw, err := cmd.Output()
	out, rc := unwrapResult(raw)
	run.log.Write(out)
	if rc.ok {
		fmt.Fprintf(run.log, "\n=== attempt on %s: $%.4f · %d tokens · %d turns\n", modelWord(model), rc.costUSD, rc.tokens, rc.turns)
	}
	return out, rc, err
}

func botArgs(run botRun, model string) []string {
	args := []string{"-p", run.prompt, "--output-format", "json"}
	if soul := strings.TrimSpace(run.bot.Soul); soul != "" {
		args = append(args, "--append-system-prompt", soul)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if run.spec.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	return args
}

func nextModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "haiku":
		return "sonnet"
	case "sonnet", "":
		return "opus"
	}
	return ""
}

func modelWord(model string) string {
	if model == "" {
		return "the default model"
	}
	return model
}

func botConfigDir(spec WatchSpec, b bots.Bot) string {
	if b.Profile == "" {
		return spec.ConfigDir
	}
	user, err := config.LoadUser(filepath.Join(agentDirOf(spec), "config.yml"))
	if err != nil || user == nil {
		return spec.ConfigDir
	}
	repo, _ := config.LoadRepo(spec.Dir)
	resolved, err := config.ApplyProfile(config.Resolve(spec.Workspace, repo, user), user, b.Profile)
	if err != nil || resolved.ConfigDir == "" {
		return spec.ConfigDir
	}
	return expandHome(resolved.ConfigDir)
}

func agentDirOf(spec WatchSpec) string { return spec.AgentDir }

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
