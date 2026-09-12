package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pngHead = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}

func TestSaveUploadKeepsOnlyPictures(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 12, 21, 30, 0, 0, time.UTC)
	path, err := saveUpload(dir, "sess-1", "../../etc/passwd shot.PNG", pngHead, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, "uploads", "sess-1")+string(filepath.Separator)) {
		t.Fatalf("saved outside the session folder: %s", path)
	}
	if filepath.Base(path) != "20260912-213000-passwd-shot.png" {
		t.Fatalf("name: %s", filepath.Base(path))
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v", st.Mode().Perm())
	}
	if _, err := saveUpload(dir, "sess-1", "notes.txt", []byte("hello, not a picture at all, really"), now); err == nil {
		t.Fatal("text saved as a picture")
	}
	if _, err := saveUpload(dir, "sess-1", "x.png", nil, now); err == nil {
		t.Fatal("empty saved")
	}
	if _, err := saveUpload(dir, "sess-1", "x.png", append(pngHead, make([]byte, maxUploadBytes)...), now); err == nil {
		t.Fatal("oversize saved")
	}
}

func TestPruneUploadsDropsOldPictures(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old, _ := saveUpload(dir, "a", "old.png", pngHead, now.Add(-8*24*time.Hour))
	_ = os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	fresh, _ := saveUpload(dir, "b", "new.png", pngHead, now)
	pruneUploads(dir, now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old picture kept")
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Fatal("empty folder kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh picture dropped")
	}
}
