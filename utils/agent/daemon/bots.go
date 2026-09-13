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

// runBot is one bot's run on one event, one at a time per workspace.
func (d *Daemon) runBot(ctx context.Context, spec WatchSpec, b bots.Bot, e watch.Event) {
	mu := d.fixBusy[spec.Workspace]
	mu.Lock()
	defer mu.Unlock()
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
	args := []string{"-p", BotRunPrompt(b, e), "--output-format", "json"}
	if soul := strings.TrimSpace(b.Soul); soul != "" {
		args = append(args, "--append-system-prompt", soul)
	}
	if b.Model != "" {
		args = append(args, "--model", b.Model)
	}
	if spec.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	dir := spec.Dir
	if b.Isolate && d.Isolate != nil {
		branch := FixBranch(e.Ref)
		if trees, err := d.Isolate(spec.Dir, branch); err == nil {
			d.watchState.Fixes.SetBranch(key, branch)
			args[1] += IsolationNote(branch, trees)
			fmt.Fprintf(logFile, "=== worktrees on %s: %s\n", branch, strings.Join(trees, ", "))
		} else {
			fmt.Fprintf(logFile, "=== could not isolate, running in place: %v\n", err)
		}
	}
	cmd := claudeCommand(ctx, dir, env, args...)
	cmd.Stdin = nil
	raw, runErr := cmd.Output()
	out, receipt := unwrapResult(raw)
	logFile.Write(out)
	if receipt.ok {
		fmt.Fprintf(logFile, "\n=== cost: $%.4f · %d tokens · %d turns\n", receipt.costUSD, receipt.tokens, receipt.turns)
		d.watchState.Fixes.SetCost(key, receipt.costUSD, receipt.tokens)
	}
	if runErr != nil {
		fmt.Fprintf(logFile, "\n=== failed: %v\n", runErr)
		d.watchState.Fixes.Finish(key, nil, "", runErr.Error(), time.Now())
		go d.notifyAttentionAt("corgi agent · "+b.Display(), fmt.Sprintf("%s on %s failed: %v — log: %s", b.Display(), e.Ref, runErr, logPath), spec.Workspace, e.URL)
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
	go d.notifyAttentionAt("corgi agent · "+b.Display(), body, spec.Workspace, target)
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
