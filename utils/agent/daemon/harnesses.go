package daemon

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// A workspace names the agents to try in order (agents: [claude, codex]).
// Every unattended run picks the first one that can work right now; the
// pick is not remembered, so the first agent gets the next run back the
// moment its window resets or its login is back.

// harnessInstalled is a seam for tests: whether the agent's binary is here.
var harnessInstalled = func(h harness.Harness) bool { return h.Installed() }

// codexWindowSpent is a seam for tests: codex's own rate-limit reading, from
// its newest session, says the window is used up.
var codexWindowSpent = func(now time.Time) bool {
	w, ok := usage.ReadCodexWindow(usage.CodexHome())
	return ok && w.Spent(now)
}

// agents is the order to try; an empty spec means claude alone.
func (s WatchSpec) agents() []string {
	if len(s.Agents) > 0 {
		return s.Agents
	}
	return []string{harness.For(s.Kind, "").Name}
}

// harnessNamed is the harness for one agent in the order. Bin only ever
// points the first agent elsewhere.
func (s WatchSpec) harnessNamed(name string) harness.Harness {
	if name == s.agents()[0] {
		return harness.For(name, s.Bin)
	}
	return harness.For(name, "")
}

// harness is the first choice, for the places that only describe a run.
func (s WatchSpec) harness() harness.Harness { return s.harnessNamed(s.agents()[0]) }

// agentUnwell says why this agent cannot take a run now: "missing" when it
// is not installed, "limit" when its window is spent, else the failure of
// its newest finished run of the day when that was a login, a permission
// or a limit. Claude's window is read for the first agent; codex's wherever
// it stands, from what its own sessions logged.
func agentUnwell(spec WatchSpec, name string, log *watch.FixLog, now time.Time) string {
	h := spec.harnessNamed(name)
	if !harnessInstalled(h) {
		return "missing"
	}
	if name == spec.agents()[0] && h.Name == harness.Claude {
		if b := freeBudget(spec.ConfigDir); b >= 0 && b <= 2 {
			return "limit"
		}
	}
	if h.Name == harness.Codex && codexWindowSpent(now) {
		return "limit"
	}
	if log == nil {
		return ""
	}
	for i := len(log.Started) - 1; i >= 0; i-- {
		r := log.Started[i]
		if r.FinishedAt.IsZero() || r.Workspace != spec.Workspace {
			continue
		}
		if ran := strings.TrimSpace(r.Harness); ran != name && !(ran == "" && name == harness.Claude) {
			continue
		}
		if now.Sub(r.FinishedAt) > 24*time.Hour {
			break
		}
		if watch.Sidelining(r.Failure) {
			return r.Failure
		}
		return ""
	}
	return ""
}

// unwellWord says a reason the way a notification should.
func unwellWord(name, why string) string {
	switch why {
	case "missing":
		return name + " is not installed"
	case "limit":
		return name + " is at its limit"
	case watch.FailureNoAuth:
		return name + " wants a login"
	case watch.FailurePermission:
		return name + " has no permission"
	}
	return name + ": " + why
}

// pickHarness is the agent that takes this run, and what was wrong with
// each one passed over ("claude is at its limit"). When none can work the
// first is returned so the run fails where it always did.
func pickHarness(spec WatchSpec, log *watch.FixLog, now time.Time) (harness.Harness, []string) {
	var skipped []string
	for _, name := range spec.agents() {
		why := agentUnwell(spec, name, log, now)
		if why == "" {
			return spec.harnessNamed(name), skipped
		}
		skipped = append(skipped, unwellWord(name, why))
	}
	return spec.harness(), skipped
}

// hasFallback says another agent in the order could take a run now, so a
// spent window on the first is not a reason to defer.
func hasFallback(spec WatchSpec, log *watch.FixLog, now time.Time) bool {
	for _, name := range spec.agents()[1:] {
		if agentUnwell(spec, name, log, now) == "" {
			return true
		}
	}
	return false
}

// laptopUnwell is what the peers hear: this laptop cannot run fixes for
// the workspace only when every agent in its order is out. The reason is
// the first agent's, the one a person would go and fix.
func laptopUnwell(spec WatchSpec, log *watch.FixLog, now time.Time) string {
	first := ""
	for i, name := range spec.agents() {
		why := agentUnwell(spec, name, log, now)
		if why == "" {
			return ""
		}
		if i == 0 {
			first = why
		}
	}
	if first == "missing" {
		return "no-credential"
	}
	return first
}

// pickHarnessFor is the daemon's pick: it logs the fallback and rings once
// a day per workspace and agent, so a traveller knows codex is doing the
// work while claude waits for its window.
func (d *Daemon) pickHarnessFor(spec WatchSpec) harness.Harness {
	var log *watch.FixLog
	if d.watchState != nil {
		log = d.watchState.Fixes
	}
	now := time.Now()
	h, skipped := pickHarness(spec, log, now)
	if len(skipped) == 0 {
		return h
	}
	line := spec.Workspace + ": " + strings.Join(skipped, ", ")
	if h.Name != spec.agents()[0] {
		line += " - this run goes through " + h.Name
		if d.rangOnce("fallback|"+spec.Workspace+"|"+h.Name, now) {
			go d.notifyAttention("corgi agent", line, spec.Workspace)
		}
	} else {
		line += " - no agent can take it"
	}
	utils.Infof("agent: %s\n", line)
	return h
}
