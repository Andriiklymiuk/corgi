package watch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils/atomicfile"
)

type BoardCache struct {
	mu         sync.Mutex
	path       string
	Workspaces map[string]BoardInfo `json:"workspaces,omitempty"`
}

type BoardInfo struct {
	Tracker   string    `json:"tracker,omitempty"`
	Project   string    `json:"project,omitempty"`
	Statuses  []Status  `json:"statuses,omitempty"`
	Me        Identity  `json:"me,omitempty"`
	FetchedAt time.Time `json:"fetchedAt,omitempty"`
	Error     string    `json:"error,omitempty"`
}

const BoardMaxAge = 7 * 24 * time.Hour

func (b BoardInfo) Stale(now time.Time) bool {
	return b.FetchedAt.IsZero() || now.Sub(b.FetchedAt) > BoardMaxAge
}

func (b BoardInfo) Has() bool { return len(b.Statuses) > 0 }

func boardPath(agentDir string) string { return filepath.Join(agentDir, "watch", "board.json") }

func LoadBoardCache(agentDir string) *BoardCache {
	c := &BoardCache{path: boardPath(agentDir), Workspaces: map[string]BoardInfo{}}
	if data, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(data, c)
	}
	if c.Workspaces == nil {
		c.Workspaces = map[string]BoardInfo{}
	}
	return c
}

func (c *BoardCache) Get(workspace string) BoardInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Workspaces[workspace]
}

func (c *BoardCache) Set(workspace string, info BoardInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Workspaces == nil {
		c.Workspaces = map[string]BoardInfo{}
	}
	c.Workspaces[workspace] = info
	return c.save()
}

func (c *BoardCache) Forget(workspace string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.Workspaces[workspace]; !ok {
		return nil
	}
	delete(c.Workspaces, workspace)
	return c.save()
}

func (c *BoardCache) save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(c.path, data, 0o600)
}

func RefreshBoard(ctx context.Context, agentDir, workspace string, w Writer, project string) (BoardInfo, error) {
	info := BoardInfo{Project: project, FetchedAt: time.Now()}
	if w == nil {
		info.Error = "no tracker token for this workspace"
		_ = LoadBoardCache(agentDir).Set(workspace, info)
		return info, nil
	}
	info.Tracker = w.Name()
	statuses, err := w.Statuses(ctx)
	if err != nil {
		info.Error = err.Error()
		_ = LoadBoardCache(agentDir).Set(workspace, info)
		return info, err
	}
	info.Statuses = statuses
	if me, err := w.Whoami(ctx); err == nil {
		info.Me = me
	}
	if err := LoadBoardCache(agentDir).Set(workspace, info); err != nil {
		return info, err
	}
	return info, nil
}
