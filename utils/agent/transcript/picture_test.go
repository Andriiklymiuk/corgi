package transcript

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageToolResultBecomesPicture(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG fake"))
	line := `{"type":"user","uuid":"u1","timestamp":"2026-09-18T10:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"shot taken"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png + `"}}]}]}}`
	plain := `{"type":"user","uuid":"u2","timestamp":"2026-09-18T10:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}`
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(line+"\n"+plain+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, _, err := Read(path, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Picture != "u1" || entries[0].PictureType != "image/png" || entries[0].Text != "shot taken" {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[1].Picture != "" {
		t.Fatal("a text result has no picture")
	}
	if strings.Contains(entries[0].Text, png) {
		t.Fatal("base64 must not ride in the entry")
	}
	mime, data, err := Picture(path, "u1")
	if err != nil || mime != "image/png" || string(data) != "\x89PNG fake" {
		t.Fatalf("picture: %v %s %q", err, mime, data)
	}
	if _, _, err := Picture(path, "u1/1"); err == nil {
		t.Fatal("no block 1")
	}
	if _, _, err := Picture(path, "u2"); err == nil {
		t.Fatal("no picture in a text result")
	}
	if _, _, err := Picture(path, "nope"); err == nil {
		t.Fatal("unknown row")
	}
}
