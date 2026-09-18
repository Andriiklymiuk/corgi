package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

const maxUploadBytes = 8 << 20

const maxUploadSealed = 2 * maxUploadBytes

const uploadKeep = 7 * 24 * time.Hour

var uploadTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func uploadDir(dir, sessionID string) string {
	return filepath.Join(dir, "uploads", sessionID)
}

func saveUpload(dir, sessionID, name string, data []byte, now time.Time) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("nothing to save")
	}
	if len(data) > maxUploadBytes {
		return "", fmt.Errorf("that picture is too big (%d MB; up to %d MB)", len(data)>>20, maxUploadBytes>>20)
	}
	ext, ok := uploadTypes[strings.ToLower(http.DetectContentType(data))]
	if !ok {
		return "", fmt.Errorf("only pictures can be sent (png, jpeg, gif, webp)")
	}
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(name)), filepath.Ext(name))
	base = strings.Trim(unsafeName.ReplaceAllString(base, "-"), "-.")
	if base == "" || base == "." || base == ".." {
		base = "image"
	}
	if len(base) > 48 {
		base = base[:48]
	}
	folder := uploadDir(dir, sessionID)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(folder, now.Format("20060102-150405")+"-"+base+ext)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func pruneUploads(dir string, now time.Time) {
	root := filepath.Join(dir, "uploads")
	folders, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, f := range folders {
		if !f.IsDir() {
			continue
		}
		p := filepath.Join(root, f.Name())
		files, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		left := 0
		for _, file := range files {
			info, err := file.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > uploadKeep {
				_ = os.Remove(filepath.Join(p, file.Name()))
				continue
			}
			left++
		}
		if left == 0 {
			_ = os.Remove(p)
		}
	}
}

func launchUploadHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, name, data} to send a picture to a session")
		return
	}
	var req struct {
		Session string `json:"session"`
		Name    string `json:"name"`
		Data    string `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxUploadSealed)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the picture")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if session.Status == sessions.StatusGone {
		writeLaunchError(w, http.StatusConflict, "that session is closed")
		return
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Data))
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, "the picture did not decode")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	path, err := saveUpload(dir, session.ID, req.Name, data, now)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	pruneUploads(dir, now)
	writeLaunchJSON(w, map[string]any{"path": path, "bytes": len(data)})
}
