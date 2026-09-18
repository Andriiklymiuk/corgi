package cmd

import (
	"bufio"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
)

func readFrame(t *testing.T, r *bufio.Reader) (string, string) {
	t.Helper()
	var event, data string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, ":"):
			continue
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && data != "":
			return event, data
		}
	}
}

func openStream(t *testing.T, url, token string, headers map[string]string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	return resp, bufio.NewReader(resp.Body)
}

func TestTheStreamSaysWhatMovedWhileSomeoneListens(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", base)
	dir := filepath.Join(base, "agent")
	_ = os.MkdirAll(dir, 0o700)
	streamTick = 20 * time.Millisecond
	t.Cleanup(func() { streamTick = 250 * time.Millisecond })

	srv := httptest.NewServer(launchStream("tok", ""))
	t.Cleanup(srv.Close)

	resp, rd := openStream(t, srv.URL+"/launch/stream", "tok", nil)
	defer resp.Body.Close()
	event, data := readFrame(t, rd)
	var hello streamFrame
	_ = json.Unmarshal([]byte(data), &hello)
	if event != "hello" || hello.Seq != 0 || len(hello.What) != 0 {
		t.Fatalf("first the hello with the seq to resume from: %s %s", event, data)
	}
	if !changeWatchFor(dir).running() {
		t.Fatal("a listener starts the watch")
	}

	if err := os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(`{"sessions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	event, data = readFrame(t, rd)
	var frame streamFrame
	_ = json.Unmarshal([]byte(data), &frame)
	if event != "change" || frame.Seq != 1 || strings.Join(frame.What, ",") != "board,kanban" {
		t.Fatalf("the board moved: %s %s", event, data)
	}
	if strings.Contains(data, "sessions") {
		t.Fatal("a frame says what moved, never what it holds")
	}

	_ = os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte("{}\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "watch", "pulls.json"), []byte("{}"), 0o600)
	_, data = readFrame(t, rd)
	_ = json.Unmarshal([]byte(data), &frame)
	if frame.Seq != 2 || strings.Join(frame.What, ",") != "inbox,kanban" {
		t.Fatalf("the inbox moved: %s", data)
	}

	resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for changeWatchFor(dir).running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if changeWatchFor(dir).running() {
		t.Fatal("the last listener gone, the watch stops")
	}
}

func TestTheStreamCatchesUpAListenerThatWasAway(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", base)
	dir := filepath.Join(base, "agent")
	_ = os.MkdirAll(dir, 0o700)
	streamTick = 20 * time.Millisecond
	t.Cleanup(func() { streamTick = 250 * time.Millisecond })
	srv := httptest.NewServer(launchStream("tok", ""))
	t.Cleanup(srv.Close)

	first, rd := openStream(t, srv.URL+"/launch/stream", "tok", nil)
	readFrame(t, rd)
	_ = os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(`{}`), 0o600)
	readFrame(t, rd)
	_ = os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{}`), 0o600)
	readFrame(t, rd)

	second, rd2 := openStream(t, srv.URL+"/launch/stream?after=1", "tok", nil)
	defer second.Body.Close()
	event, data := readFrame(t, rd2)
	var frame streamFrame
	_ = json.Unmarshal([]byte(data), &frame)
	if event != "hello" || frame.Seq != 2 || strings.Join(frame.What, ",") != strings.Join(allFeeds, ",") {
		t.Fatalf("away since 1, now at 2: read everything — %s %s", event, data)
	}
	first.Body.Close()
}

func TestTheStreamSealsEveryFrameForAKeyedDevice(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", base)
	dir := filepath.Join(base, "agent")
	_ = os.MkdirAll(dir, 0o700)
	streamTick = 20 * time.Millisecond
	t.Cleanup(func() { streamTick = 250 * time.Millisecond })
	session, code, store := pairingFixture(t)
	phone, _ := ecdh.X25519().GenerateKey(rand.Reader)

	mux := http.NewServeMux()
	mux.Handle("/pair", pairingHandler(session, store))
	mux.Handle("/launch/stream", launchStream("server-token", store))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/pair", mimeJSON, strings.NewReader(`{"code":"`+code+`","device":"phone","pubKey":"`+pairing.PublicKeyString(phone.PublicKey())+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var paired struct {
		Token        string `json:"token"`
		ServerPubKey string `json:"serverPubKey"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&paired)
	resp.Body.Close()
	serverPub, _ := pairing.ParsePublicKey(paired.ServerPubKey)
	key, _ := pairing.SharedKeyOnDevice(phone, serverPub)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/launch/stream", nil)
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	plain, _ := http.DefaultClient.Do(req)
	if plain.StatusCode != http.StatusForbidden {
		t.Fatalf("plaintext from a keyed device: %d", plain.StatusCode)
	}
	plain.Body.Close()

	stream, rd := openStream(t, srv.URL+"/launch/stream", paired.Token, map[string]string{pairing.E2EHeader: "1"})
	defer stream.Body.Close()
	if stream.Header.Get(pairing.E2EHeader) != "1" {
		t.Fatal("the stream says it is sealed")
	}
	_, data := readFrame(t, rd)
	var frame streamFrame
	if err := json.Unmarshal([]byte(data), &frame); err == nil && frame.What != nil {
		t.Fatal("the frame must not read as plain")
	}
	opened, err := pairing.Open(key, http.MethodGet, "/launch/stream", []byte(data), time.Now())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = json.Unmarshal(opened, &frame)
	if frame.Seq != 0 {
		t.Fatalf("hello inside: %s", opened)
	}
	_ = os.WriteFile(filepath.Join(dir, "sessions.json"), []byte(`{}`), 0o600)
	_, data = readFrame(t, rd)
	opened, err = pairing.Open(key, http.MethodGet, "/launch/stream", []byte(data), time.Now())
	if err != nil {
		t.Fatalf("open change: %v", err)
	}
	_ = json.Unmarshal(opened, &frame)
	if frame.Seq != 1 || frame.What[0] != "board" {
		t.Fatalf("change inside: %s", opened)
	}
}

func TestASendWithAnIdIsTypedOnce(t *testing.T) {
	if !sendOnce.first("phone:abc", time.Now()) {
		t.Fatal("first time through")
	}
	if sendOnce.first("phone:abc", time.Now().Add(time.Minute)) {
		t.Fatal("the same id a minute later is a repeat")
	}
	if !sendOnce.first("phone:abc", time.Now().Add(sendOnceFor+time.Minute)) {
		t.Fatal("after the window the id is forgotten")
	}
	if !sendOnce.first("", time.Now()) || !sendOnce.first("", time.Now()) {
		t.Fatal("no id, no memory")
	}
}
