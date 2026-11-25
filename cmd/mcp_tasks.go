package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const (
	mcpTaskKeep         = time.Hour
	mcpTaskPollMs       = 5000
	mcpTasksRunningMax  = 8
	mcpTaskWorking      = "working"
	mcpTaskCompleted    = "completed"
	mcpTaskFailed       = "failed"
	mcpTaskCancelled    = "cancelled"
	mcpTaskStatusOfTool = "Running "
)

var mcpTaskTools = map[string]bool{
	"corgi_up": true, "corgi_test": true, "corgi_restart": true, "corgi_doctor": true,
	"corgi_wait_for_log": true, "corgi_db_snapshot": true, "corgi_db_restore": true,
	"corgi_worktrees_materialize": true, "corgi_preview_start": true, "corgi_idea_build": true,
}

type mcpTask struct {
	ID      string
	Owner   string
	Status  string
	Message string
	Created time.Time
	Updated time.Time
	Result  json.RawMessage
	Error   json.RawMessage
	cancel  context.CancelFunc
}

type mcpTaskStore struct {
	mu    sync.Mutex
	tasks map[string]*mcpTask
	now   func() time.Time
}

func newMCPTaskStore() *mcpTaskStore {
	return &mcpTaskStore{tasks: map[string]*mcpTask{}, now: time.Now}
}

func (s *mcpTaskStore) start(owner, tool string, cancel context.CancelFunc) (*mcpTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now, running := s.now(), 0
	for id, t := range s.tasks {
		if t.Status == mcpTaskWorking {
			running++
		} else if now.Sub(t.Updated) > mcpTaskKeep {
			delete(s.tasks, id)
		}
	}
	if running >= mcpTasksRunningMax {
		return nil, false
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return nil, false
	}
	t := &mcpTask{ID: hex.EncodeToString(raw), Owner: owner, Status: mcpTaskWorking, Message: mcpTaskStatusOfTool + tool, Created: now, Updated: now, cancel: cancel}
	s.tasks[t.ID] = t
	return t, true
}

func (s *mcpTaskStore) finish(id string, result, failure json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil || t.Status != mcpTaskWorking {
		return
	}
	t.Status, t.Result, t.Message = mcpTaskCompleted, result, ""
	if len(failure) > 0 {
		t.Status, t.Error, t.Result = mcpTaskFailed, failure, nil
	}
	t.Updated = s.now()
}

func (s *mcpTaskStore) cancel(id, owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil || t.Owner != owner {
		return false
	}
	if t.Status == mcpTaskWorking {
		t.Status, t.Message, t.Updated = mcpTaskCancelled, "", s.now()
		t.cancel()
	}
	return true
}

func (s *mcpTaskStore) view(id, owner string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil || t.Owner != owner {
		return nil, false
	}
	out := map[string]any{
		"taskId": t.ID, "status": t.Status,
		"createdAt": t.Created.UTC().Format(time.RFC3339), "lastUpdatedAt": t.Updated.UTC().Format(time.RFC3339),
		"ttlMs": mcpTaskKeep.Milliseconds(), "pollIntervalMs": mcpTaskPollMs,
	}
	if t.Message != "" {
		out["statusMessage"] = t.Message
	}
	if len(t.Result) > 0 {
		out["result"] = t.Result
	}
	if len(t.Error) > 0 {
		out["error"] = t.Error
	}
	return out, true
}
