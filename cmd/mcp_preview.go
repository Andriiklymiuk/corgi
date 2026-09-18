package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const previewFor = 10 * time.Minute

type previewTicket struct {
	Workspace string
	Service   string
	Port      int
	Until     time.Time
}

var (
	previewMu            sync.Mutex
	previewTickets       = map[string]previewTicket{}
	previewWorkspaceRoot = workspaceRoot
	previewSnapshot      = stackSnapshot
	previewNow           = time.Now
)

func launchPreviewHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {workspace, service} for a preview link")
		return
	}
	var req struct {
		Workspace string `json:"workspace"`
		Service   string `json:"service"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	id, service := strings.TrimSpace(req.Workspace), strings.TrimSpace(req.Service)
	if id == "" || service == "" {
		writeLaunchError(w, http.StatusBadRequest, "a workspace and a service")
		return
	}
	root, err := previewWorkspaceRoot(id)
	if err != nil || root == "" {
		writeLaunchError(w, http.StatusNotFound, "no workspace named "+id)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), stackTimeout)
	defer cancel()
	services, _, err := previewSnapshot(ctx, root)
	if err != nil {
		writeLaunchError(w, http.StatusBadGateway, "could not read the stack: "+err.Error())
		return
	}
	var found *StackService
	for i := range services {
		if services[i].Name == service {
			found = &services[i]
		}
	}
	switch {
	case found == nil:
		writeLaunchError(w, http.StatusNotFound, "no service named "+service+" in the stack")
		return
	case found.Kind == "database":
		writeLaunchError(w, http.StatusBadRequest, service+" is a database, not a page")
		return
	case found.Status != "running":
		writeLaunchError(w, http.StatusConflict, service+" is not running — start the stack first")
		return
	case found.Port == 0:
		writeLaunchError(w, http.StatusBadRequest, service+" has no port to open")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not make a ticket")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := previewNow()
	until := now.Add(previewFor)
	previewMu.Lock()
	for k, t := range previewTickets {
		if now.After(t.Until) {
			delete(previewTickets, k)
		}
	}
	previewTickets[token] = previewTicket{Workspace: id, Service: service, Port: found.Port, Until: until}
	previewMu.Unlock()
	writeLaunchJSON(w, map[string]any{"url": "/launch/preview/" + token + "/", "expiresAt": until, "service": service, "port": found.Port, "workspace": id})
}

func previewProxyHandler(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/launch/preview/")
	token, path, ok := strings.Cut(rest, "/")
	if !ok || token == "" {
		http.NotFound(w, r)
		return
	}
	previewMu.Lock()
	t, found := previewTickets[token]
	previewMu.Unlock()
	if !found || previewNow().After(t.Until) {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Upgrade") != "" {
		writeLaunchError(w, http.StatusNotImplemented, "the preview carries pages, not sockets")
		return
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(t.Port)}
	prefix := "/launch/preview/" + token + "/"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.URL.Path = "/" + path
			pr.Out.URL.RawPath = ""
			pr.Out.Host = target.Host
			pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
			pr.Out.Header.Del("Authorization")
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeLaunchError(w, http.StatusBadGateway, t.Service+" did not answer: "+err.Error())
		},
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if loc := resp.Header.Get("Location"); strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, prefix) {
			resp.Header.Set("Location", prefix+strings.TrimPrefix(loc, "/"))
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	proxy.ServeHTTP(w, r.WithContext(ctx))
}
