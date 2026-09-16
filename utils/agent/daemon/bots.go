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

// A bot that runs on its own. A bot is a persona (bots.Bot); with `on`
// set it also acts when one of those events arrives in its workspace — a
// review lands, a build goes red — as an unattended run under its own
// soul and model, logged and costed like a fix, filed under the bot's
// name so every surface can say "reviewer ran on #302: two findings".
//
// It is not a fix: it takes no lease, moves no ticket, isolates only when
// the bot says so, and a blocked ticket does not stop it (a review is
// still wanted on a blocked ticket). The watch's caps, quiet hours and
// days off still hold — a bot spends the same account.

// botRunKey files a bot's run apart from the fix on the same event.
func botRunKey(bot, eventKey string) string { return "bot:" + bot + ":" + eventKey }

// BotRunPrompt is what a bot is told when its event arrives: the event in
// the words the fix would get, then who it is and what to leave behind.
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

// startBots runs every bot in the workspace that acts on this kind.
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

// runBot is one bot's run on one event, in one of the workspace's slots.
func (d *Daemon) runBot(ctx context.Context, spec WatchSpec, b bots.Bot, e watch.Event) {
	defer d.takeSlot(spec.Workspace)()
	ctx, cancel := context.WithTimeout(ctx, fixTimeout)
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

	var env []string
	if configDir := botConfigDir(spec, b); configDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+configDir)
	}
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
	// One failure gets one more try, a rung up the ladder: the model that
	// looped or fell over is often the model that was too small for it.
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
		go d.notifyAttentionAt(notifyTitlePrefix+b.Display(), fmt.Sprintf("%s on %s failed: %v — log: %s", b.Display(), e.Ref, runErr, logPath), spec.Workspace, e.URL)
		return
	}
	links := uniqueStrings(prLink.FindAllString(string(out), -1))
	note := clipText(lastLine(string(out)), 200)
	d.watchState.Fixes.Finish(key, links, note, "", time.Now())
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

// botRun is one bot's run on one event: everything an attempt needs that
// does not change between the first try and the retry on a bigger model.
type botRun struct {
	spec   WatchSpec
	bot    bots.Bot
	prompt string
	dir    string
	env    []string
	log    *os.File
}

// botAttempt is one `claude -p` under the bot's soul on one model; what
// it printed goes to the log as it comes.
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

// botArgs is the claude command line for one attempt.
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

// nextModel is the rung above: the retry after a failed run. Past opus
// there is nowhere to go; a bot on the default model retries on opus.
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

// botConfigDir is the account a bot runs under: its own profile applied
// to the workspace's config, else the workspace's own.
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

// agentDirOf is the agent dir a spec's daemon runs from, kept on the spec
// by the daemon so a run can find the user config beside the bots.
func agentDirOf(spec WatchSpec) string { return spec.AgentDir }

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
