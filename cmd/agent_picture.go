package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"andriiklymiuk/corgi/utils/agent/transcript"
)

const maxPictureBytes = 4 << 20

func launchPictureHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, id} for a picture a tool returned")
		return
	}
	var req struct {
		Session string `json:"session"`
		ID      string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ID == "" {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if !streamAllowedFor(session.Label) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reading sessions of %s from a phone is off on the laptop: corgi agent stream enable --workspace %s", session.Label, session.Label))
		return
	}
	path := transcriptPathFor(session)
	if path == "" || !transcript.Exists(path) {
		writeLaunchError(w, http.StatusNotFound, "no conversation")
		return
	}
	mime, data, err := transcript.Picture(path, req.ID)
	if err != nil || len(data) == 0 || len(data) > maxPictureBytes {
		writeLaunchError(w, http.StatusNotFound, "no picture there")
		return
	}
	writeLaunchJSON(w, map[string]any{"type": mime, "data": base64.StdEncoding.EncodeToString(data)})
}
