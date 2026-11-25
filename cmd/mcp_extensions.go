package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/mark3labs/mcp-go/server"
)

const (
	mcpSkillsExtension = "io.modelcontextprotocol/skills"
	mcpTasksExtension  = "io.modelcontextprotocol/tasks"
	mcpClientCapsMeta  = "io.modelcontextprotocol/clientCapabilities"
	mcpInvalidParams   = -32602
	mcpBodyMax         = 8 << 20
	mcpStdioOwner      = "stdio"
)

var mcpExtensionCapabilities = map[string]any{
	mcpSkillsExtension: map[string]any{"directoryRead": true},
	mcpTasksExtension:  map[string]any{},
}

type mcpExtensions struct {
	server *server.MCPServer
	tasks  *mcpTaskStore
	skills func() skillSet
}

func newMCPExtensions(s *server.MCPServer) *mcpExtensions {
	return &mcpExtensions{server: s, tasks: newMCPTaskStore(), skills: bundledSkills}
}

type mcpCall struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		URI    string                     `json:"uri"`
		Name   string                     `json:"name"`
		TaskID string                     `json:"taskId"`
		Meta   map[string]json.RawMessage `json:"_meta"`
	} `json:"params"`
}

func (c mcpCall) declaresTasks() bool {
	var caps struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	_ = json.Unmarshal(c.Params.Meta[mcpClientCapsMeta], &caps)
	_, ok := caps.Extensions[mcpTasksExtension]
	return ok
}

func (x *mcpExtensions) answer(ctx context.Context, raw []byte, owner string) ([]byte, bool) {
	var call mcpCall
	if json.Unmarshal(raw, &call) != nil || len(call.ID) == 0 {
		return nil, false
	}
	switch call.Method {
	case "skills/list":
		return mcpResult(call.ID, cached(map[string]any{"skills": x.skills().entries})), true
	case "skills/get":
		if entry, ok := x.skills().entry(call.Params.URI); ok {
			return mcpResult(call.ID, cached(map[string]any{"skill": entry})), true
		}
		return mcpFailure(call.ID, "no skill at "+call.Params.URI), true
	case "resources/read":
		if !strings.HasPrefix(call.Params.URI, skillScheme) {
			return nil, false
		}
		if content, ok := x.skills().content(call.Params.URI); ok {
			return mcpResult(call.ID, cached(map[string]any{"contents": []any{content}})), true
		}
		return mcpFailure(call.ID, "no skill file at "+call.Params.URI), true
	case "resources/directory/read":
		if kids, ok := x.skills().children(call.Params.URI); ok {
			return mcpResult(call.ID, cached(map[string]any{"resources": kids})), true
		}
		return mcpFailure(call.ID, "no skill directory at "+call.Params.URI), true
	case "tasks/get":
		if view, ok := x.tasks.view(call.Params.TaskID, owner); ok {
			return mcpResult(call.ID, complete(view)), true
		}
		return mcpFailure(call.ID, "no such task"), true
	case "tasks/cancel":
		if x.tasks.cancel(call.Params.TaskID, owner) {
			return mcpResult(call.ID, complete(map[string]any{})), true
		}
		return mcpFailure(call.ID, "no such task"), true
	case "tasks/update":
		if _, ok := x.tasks.view(call.Params.TaskID, owner); ok {
			return mcpResult(call.ID, complete(map[string]any{})), true
		}
		return mcpFailure(call.ID, "no such task"), true
	case "tools/call":
		if !call.declaresTasks() || !mcpTaskTools[call.Params.Name] {
			return nil, false
		}
		return x.startTask(ctx, call, raw, owner)
	}
	return nil, false
}

func (x *mcpExtensions) startTask(ctx context.Context, call mcpCall, raw []byte, owner string) ([]byte, bool) {
	taskCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	task, ok := x.tasks.start(owner, call.Params.Name, cancel)
	if !ok {
		cancel()
		return nil, false
	}
	go func() {
		defer cancel()
		var answer struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		out, err := json.Marshal(x.server.HandleMessage(taskCtx, raw))
		if err == nil {
			err = json.Unmarshal(out, &answer)
		}
		if err != nil {
			answer.Error, _ = json.Marshal(map[string]any{"code": -32603, "message": err.Error()})
		}
		x.tasks.finish(task.ID, answer.Result, answer.Error)
	}()
	view, _ := x.tasks.view(task.ID, owner)
	view["resultType"] = "task"
	return mcpResult(call.ID, view), true
}

func complete(result map[string]any) map[string]any {
	result["resultType"] = "complete"
	return result
}

func cached(result map[string]any) map[string]any {
	result["ttlMs"], result["cacheScope"] = skillsTTLMs, "public"
	return complete(result)
}

func mcpResult(id json.RawMessage, result any) []byte {
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	return out
}

func mcpFailure(id json.RawMessage, message string) []byte {
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": mcpInvalidParams, "message": message}})
	return out
}

func (x *mcpExtensions) overHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpBodyMax))
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		caller := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if answer, ok := x.answer(r.Context(), body, hex.EncodeToString(caller[:])); ok {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(answer)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (x *mcpExtensions) overStdio(ctx context.Context, in io.Reader, out io.Writer, listen func(context.Context, io.Reader, io.Writer) error) error {
	shared := &lockedWriter{w: out}
	forLibrary, pass := io.Pipe()
	go func() {
		reader := bufio.NewReader(in)
		for {
			line, err := reader.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				if answer, ok := x.answer(ctx, line, mcpStdioOwner); ok {
					_, _ = shared.Write(append(answer, '\n'))
				} else if _, werr := pass.Write(line); werr != nil {
					return
				}
			}
			if err != nil {
				_ = pass.CloseWithError(err)
				return
			}
		}
	}()
	err := listen(ctx, forLibrary, shared)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func serveStdioWithExtensions(s *server.MCPServer) error {
	stdio := server.NewStdioServer(s)
	server.WithWorkerPoolSize(1)(stdio)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		cancel()
	}()
	return newMCPExtensions(s).overStdio(ctx, os.Stdin, os.Stdout, stdio.Listen)
}
