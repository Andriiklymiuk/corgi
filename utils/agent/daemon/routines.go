package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/atomicfile"
)

type routineState struct {
	mu   sync.Mutex
	path string
	Last map[string]time.Time `json:"last"`
}

func loadRoutineState(agentDir string) *routineState {
	s := &routineState{path: filepath.Join(agentDir, "watch", "routines.json"), Last: map[string]time.Time{}}
	if data, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(data, s)
	}
	if s.Last == nil {
		s.Last = map[string]time.Time{}
	}
	return s
}

func (s *routineState) get(key string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Last[key]
}

func (s *routineState) set(key string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Last[key] = at
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(s.path), 0o700)
		_ = atomicfile.Write(s.path, data, 0o600)
	}
}

func RoutineEvent(workspace string, r config.Routine, now time.Time) (watch.Event, bool) {
	prompt := strings.TrimSpace(r.Prompt)
	title := r.Name
	if k, ok := watch.CatalogKind(r.Kind); ok && prompt == "" {
		prompt = k.Prompt
		if title == "" {
			title = k.Name
		}
	}
	if prompt == "" || title == "" {
		return watch.Event{}, false
	}
	return watch.Event{
		Key:       "routine:" + workspace + ":" + title + ":" + now.Format("20060102-1504"),
		Source:    "routine",
		Kind:      watch.KindRoutine,
		Workspace: workspace,
		Ref:       "routine/" + title,
		Title:     title,
		Body:      prompt,
		At:        now,
	}, true
}

func (d *Daemon) runRoutines(ctx context.Context, now time.Time) {
	if d.watchState == nil {
		return
	}
	if d.routines == nil {
		d.routines = loadRoutineState(d.Dir)
	}
	for _, spec := range d.Watches {
		for _, r := range spec.Routines {
			if r.Off {
				continue
			}
			sched, err := watch.ParseSchedule(r.Schedule)
			if err != nil {
				continue
			}
			key := spec.Workspace + "/" + firstNonEmpty(r.Name, r.Kind)
			if !sched.Due(d.routines.get(key), now) {
				continue
			}
			e, ok := RoutineEvent(spec.Workspace, r, now)
			if !ok {
				continue
			}
			if reason := fixDeferral(spec, d.watchState.Fixes, now); reason != "" {
				utils.Infof("agent: routine %s waits: %s\n", e.Title, reason)
				continue
			}
			if !d.claimFix(spec.Workspace, e.Ref) {
				continue
			}
			d.routines.set(key, now)
			d.startRoutine(ctx, spec, r, e)
			break
		}
	}
}

func (d *Daemon) startRoutine(ctx context.Context, spec WatchSpec, r config.Routine, e watch.Event) {
	if b, ok := d.routineBot(spec, r); ok {
		utils.Infof("agent: routine %s starts in %s as %s\n", e.Title, spec.Workspace, b.Display())
		d.runs.Add(1)
		go func() {
			defer d.runs.Done()
			defer d.releaseFix(spec.Workspace, e.Ref)
			d.runBot(ctx, spec, b, e)
		}()
		return
	}
	d.watchState.Fixes.StartFor(e, time.Now())
	run := spec
	if r.Model != "" {
		run.Models = &config.ModelPolicy{Kinds: map[string]string{string(watch.KindRoutine): r.Model}}
	}
	utils.Infof("agent: routine %s starts in %s\n", e.Title, spec.Workspace)
	d.spawnFix(ctx, run, e)
}

func (d *Daemon) routineBot(spec WatchSpec, r config.Routine) (bots.Bot, bool) {
	if strings.TrimSpace(r.Bot) == "" {
		return bots.Bot{}, false
	}
	store, err := bots.Load(bots.Path(d.Dir))
	if err != nil {
		return bots.Bot{}, false
	}
	b, ok := store.Find(r.Bot)
	if !ok || b.Workspace != spec.Workspace {
		utils.Infof("agent: routine %s: no bot %q in %s, running plain\n", r.Name, r.Bot, spec.Workspace)
		return bots.Bot{}, false
	}
	return b, true
}

func routineFor(spec WatchSpec, e watch.Event) config.Routine {
	for _, r := range spec.Routines {
		if strings.EqualFold(firstNonEmpty(r.Name, r.Kind), e.Title) {
			return r
		}
	}
	return config.Routine{}
}

func (d *Daemon) routineReport(spec WatchSpec, e watch.Event, out string, failed error) {
	if e.Kind != watch.KindRoutine {
		return
	}
	headline := firstLine(strings.TrimSpace(out))
	if failed != nil {
		headline = "failed: " + failed.Error()
	}
	if headline == "" {
		headline = "finished with nothing to say"
	}
	report := e
	report.Key = e.Key + ":report"
	report.Title = e.Title + " — " + clipText(headline, 160)
	report.Body = ""
	d.watchState.MarkSeen(report.Key)
	d.appendWatchEvent(report)
	go d.notifyAttention(notifyTitlePrefix+spec.Workspace, e.Title+": "+clipText(headline, 160), spec.Workspace)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
