package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestLaunchPictureServesAToolImage(t *testing.T) {
	phoneBoard(t, true,
		sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Status: sessions.StatusWorking, Cwd: "/w/api"},
		sessions.Session{ID: "s2", Label: "web", Display: "web·cart", Status: sessions.StatusWorking, Cwd: "/w/web"},
	)
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	line := `{"type":"user","uuid":"u1","timestamp":"2026-09-18T10:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png + `"}}]}]}}` + "\n"
	os.WriteFile(path, []byte(line), 0o600)
	origAllowed, origPath := streamAllowedFor, transcriptPathFor
	defer func() { streamAllowedFor, transcriptPathFor = origAllowed, origPath }()
	streamAllowedFor = func(ws string) bool { return ws == "api" }
	transcriptPathFor = func(s sessions.Session) string { return path }

	if rec := post(launchPictureHandler, "/launch/picture", `{"session":"s2","id":"u1"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("web is not allowed: %d", rec.Code)
	}
	if rec := post(launchPictureHandler, "/launch/picture", `{"session":"s1"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no id: %d", rec.Code)
	}
	if rec := post(launchPictureHandler, "/launch/picture", `{"session":"s1","id":"u9"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown picture: %d", rec.Code)
	}
	rec := post(launchPictureHandler, "/launch/picture", `{"session":"s1","id":"u1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("picture: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Type != "image/png" || got.Data != png {
		t.Fatalf("got %+v", got)
	}
}
