package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

const headlessTimeout = 30 * time.Minute

func label(s sessions.Session) string {
	if s.Display != "" {
		return s.Display
	}
	return s.Label
}

func (d *Daemon) continueHeadless(ctx context.Context, ref, text string) {
	if d.Sessions == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s, err := d.Sessions.Lookup(ref)
	if err != nil {
		ended, ok := d.Sessions.LookupEnded(ref)
		if !ok {
			d.Sessions.SetNotice(err)
			return
		}
		s = ended
	}
	if s.Status != sessions.StatusGone {
		err := fmt.Errorf("%s still has a terminal - Send types into it; a headless turn is for a session whose terminal is gone", label(s))
		utils.Infof("agent: continue %s: %v\n", ref, err)
		d.Sessions.SetNotice(err)
		return
	}
	if s.Cwd == "" || sessions.Placeholder(s.ID) {
		d.Sessions.SetNotice(fmt.Errorf("%s has no checkout or id on record to resume", s.Display))
		return
	}
	d.headlessMu.Lock()
	if d.headless == nil {
		d.headless = map[string]bool{}
	}
	if d.headless[s.ID] {
		d.headlessMu.Unlock()
		d.Sessions.SetNotice(fmt.Errorf("%s is already on a headless turn", s.Display))
		return
	}
	d.headless[s.ID] = true
	d.headlessMu.Unlock()
	d.Sessions.SetHeadless(s.ID, true, "", time.Now())
	d.runs.Add(1)
	go func() {
		defer d.runs.Done()
		defer func() {
			d.headlessMu.Lock()
			delete(d.headless, s.ID)
			d.headlessMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(ctx, headlessTimeout)
		defer cancel()
		h := harness.For(s.Agent, "")
		env := headlessEnv(s.ConfigDir, os.Getenv("CORGI_OMIT"))
		if h.Name != harness.Claude {
			env = withoutClaudeHome(env)
		}
		args := h.PrintArgs(harness.Print{Prompt: text, Resume: s.ID})
		logDir := filepath.Join(d.Dir, "watch", "runs")
		_ = os.MkdirAll(logDir, 0o700)
		logPath := filepath.Join(logDir, "continue-"+safeName(s.ID)+".log")
		logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if logFile != nil {
			fmt.Fprintf(logFile, "=== %s headless turn: %s\n", time.Now().Format(time.RFC3339), firstLine(text))
			defer logFile.Close()
		}
		cmd := WatchSpec{}.runCommand(ctx, h, s.Cwd, env, args...)
		cmd.Stdin = nil
		raw, runErr := cmd.Output()
		out, receipt := unwrapWith(h, raw)
		if logFile != nil {
			logFile.Write(out)
			if receipt.ok {
				fmt.Fprintf(logFile, "\n=== cost: $%.4f · %d tokens · %d turns\n", receipt.costUSD, receipt.tokens, receipt.turns)
			}
		}
		errText := ""
		if runErr != nil {
			errText = firstLine(runErr.Error())
			utils.Infof("agent: headless turn for %s failed: %v\n", s.Display, runErr)
		} else {
			utils.Infof("agent: headless turn for %s done\n", s.Display)
		}
		d.Sessions.SetHeadless(s.ID, false, errText, time.Now())
		d.flushSessions()
	}()
}
