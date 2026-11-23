package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func runPageRig(t *testing.T) (dir, key string) {
	t.Helper()
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	key = "routine:acme:retro:20261123-0742"
	log := "=== 2026-11-23T07:42:14+02:00 routine routine/retro\n=== model: x\nOutcome: nothing\n\n**4 runs, 1 task filed: TASK-1.**\n\n<script>alert(1)</script>\n\n| a | b |\n|---|---|\n| one | two |\n=== cost: $0.5\n"
	path := filepath.Join(dir, "watch", "runs", "routine_acme_retro_20261123-0742.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	began := time.Now().Add(-time.Minute)
	fixes := watch.LoadFixLog(dir)
	fixes.StartFor(watch.Event{Key: key, Workspace: "acme", Ref: "routine/retro", Kind: watch.KindRoutine, Title: "retro"}, began)
	fixes.Finish(key, nil, "4 runs", "", time.Now())
	if _, err := watch.LoadTasks(dir).Add("Retro: a build nobody asked for", "", "acme", "cli", time.Now()); err != nil {
		t.Fatal(err)
	}
	return dir, key
}

func openRunPage(link string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	runPageHandler(rec, httptest.NewRequest(http.MethodGet, link, nil))
	return rec
}

func TestARunLinkOpensTheReportAndTheTasksItFiled(t *testing.T) {
	dir, key := runPageRig(t)
	link := runPageLink(dir, "https://corgi.example", key, time.Now())
	if !strings.HasPrefix(link, "https://corgi.example/run/") || strings.ContainsAny(link, " |#<>") {
		t.Fatalf("a plain https link: %s", link)
	}
	rec := openRunPage(link)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, body)
	}
	for _, want := range []string{"4 runs, 1 task filed", "<td>one</td>", "TASK-1", "a build nobody asked for", "acme ·"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	for _, not := range []string{"<script>", "Outcome:", "=== ", "Retro: a build"} {
		if strings.Contains(body, not) {
			t.Errorf("%q does not belong on the page", not)
		}
	}
	if csp := rec.Header().Get(headerContentSecurityPolicy); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("no script runs here: %q", csp)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("headers: %v", rec.Header())
	}
}

func TestARunLinkIsForOneRunAndEnds(t *testing.T) {
	dir, key := runPageRig(t)
	now := time.Now()
	good := runPageLink(dir, "https://corgi.example", key, now)
	other := runPageLink(dir, "https://corgi.example", "routine:acme:digest:1", now)
	signature := good[strings.Index(good, "?"):]
	for name, link := range map[string]string{
		"another run under this signature": strings.SplitN(other, "?", 2)[0] + signature,
		"a longer life":                    strings.Replace(good, "?e=", "?e=9", 1),
		"no signature":                     strings.SplitN(good, "?", 2)[0],
		"expired":                          runPageLink(dir, "https://corgi.example", key, now.Add(-runLinkLasts-time.Hour)),
		"not a key":                        "https://corgi.example/run/%21%21",
	} {
		if rec := openRunPage(link); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := openRunPage(other); rec.Code != http.StatusNotFound {
		t.Errorf("a run that never was: %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	runPageHandler(rec, httptest.NewRequest(http.MethodPost, good, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("reading only: %d", rec.Code)
	}
	if runPageLink(dir, "", key, now) != "" || runPageLink(dir, "https://x", "", now) != "" {
		t.Error("no address or no run, no link")
	}
	info, err := os.Stat(filepath.Join(dir, runLinkKeyName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the key is the owner's alone: %v %v", info, err)
	}
}

func TestARunThatFailedOrStillRunsSaysSo(t *testing.T) {
	dir, _ := runPageRig(t)
	fixes := watch.LoadFixLog(dir)
	fixes.StartFor(watch.Event{Key: "k1", Workspace: "acme", Ref: "ABC-1", Title: "Cap the retries"}, time.Now())
	if page, ok := runPageHTML(dir, "k1"); !ok || !strings.Contains(page, "still running") || !strings.Contains(page, "wrote no report") {
		t.Fatalf("running: %v %s", ok, page)
	}
	fixes.Finish("k1", nil, "", "exit <1>", time.Now())
	if page, _ := runPageHTML(dir, "k1"); !strings.Contains(page, "failed: exit &lt;1&gt;") {
		t.Fatalf("failed: %s", page)
	}
}

func TestANotificationNeverCarriesALinkOnlyThisMachineOpens(t *testing.T) {
	for link, want := range map[string]string{
		"file:///Users/me/Library/Application%20Support/corgi/run.log": "",
		"/Users/me/run.log":                   "",
		"http://127.0.0.1:8765/run/x":         "",
		"http://localhost:8765/app":           "",
		"http://[::1]:8765/app":               "",
		"https://corgi.example/run/x?e=1&k=2": "https://corgi.example/run/x?e=1&k=2",
		"http://100.64.0.7:8765/app":          "http://100.64.0.7:8765/app",
	} {
		if got := awayLink(link); got != want {
			t.Errorf("%s: %q, want %q", link, got, want)
		}
		body := decodeBodyFor(t, link)
		if want == "" && strings.Contains(body, "://") || want != "" && !strings.Contains(body, want) {
			t.Errorf("%s: the post says %q", link, body)
		}
	}
}

func decodeBodyFor(t *testing.T, link string) string {
	t.Helper()
	req, err := buildNotifyRequest(webhookTelegram, "https://api.telegram.org/bot1:x/sendMessage?chat_id=7", "corgi agent · acme", "retro: 4 runs", link)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(req.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Text
}

func TestTheMacOpensARunHereNotThroughTheTunnel(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	_ = os.MkdirAll(dir, 0o700)
	srv := httptest.NewServer(http.NewServeMux())
	defer srv.Close()
	recordPublicURL("https://corgi.example")
	if err := os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(strings.TrimPrefix(srv.URL, "http://")), 0o600); err != nil {
		t.Fatal(err)
	}
	for link, want := range map[string]string{
		"https://corgi.example/run/abc?e=1&k=2": srv.URL + "/run/abc?e=1&k=2",
		"https://corgi.example/app":             srv.URL + "/app",
		"https://corgi.example/idea/x":          "https://corgi.example/idea/x",
		"https://corgi.example.evil/run/abc":    "https://corgi.example.evil/run/abc",
		"https://github.com/acme/api/pull/9":    "https://github.com/acme/api/pull/9",
		"":                                      "",
	} {
		if got := preferLocalLink(link); got != want {
			t.Errorf("%s: %s, want %s", link, got, want)
		}
	}
	if link := runLinkFor(dir)("routine:acme:retro:1"); !strings.HasPrefix(link, "https://corgi.example/run/") {
		t.Errorf("away it is the tunnel: %s", link)
	}
}
