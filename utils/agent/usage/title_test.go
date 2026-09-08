package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTitleOfPrefersTheCustomName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	body := `{"type":"user"}
{"type":"ai-title","aiTitle":"First title","sessionId":"x"}
{"type":"ai-title","aiTitle":"Second title","sessionId":"x"}
`
	_ = os.WriteFile(path, []byte(body), 0o600)
	if got := TitleOf(path); got != "Second title" {
		t.Fatalf("got %q", got)
	}
	_ = os.WriteFile(path, []byte(body+`{"type":"custom-title","customTitle":"  mine "}`+"\n"), 0o600)
	if got := TitleOf(path); got != "mine" {
		t.Fatalf("got %q", got)
	}
	if TitleOf(filepath.Join(t.TempDir(), "none")) != "" {
		t.Fatal("missing file has no title")
	}
}
