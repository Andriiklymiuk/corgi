package cmd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/russross/blackfriday/v2"
)

const (
	runPagePath     = "/run/"
	runLinkKeyName  = "run-links.key"
	runLinkKeyBytes = 32
	runLinkLasts    = 14 * 24 * time.Hour
	runPageMaxBytes = 512 << 10
	runPageCSP      = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
)

var runPageMisses = &guestLimiter{seen: map[string][]time.Time{}}

func runLinkSecret(dir string) ([]byte, error) {
	path := filepath.Join(dir, runLinkKeyName)
	if data, err := os.ReadFile(path); err == nil && len(data) == runLinkKeyBytes {
		return data, nil
	}
	fresh := make([]byte, runLinkKeyBytes)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) != runLinkKeyBytes {
			err = fmt.Errorf("%s is not a run link key", path)
		}
		return data, err
	}
	if err != nil {
		return nil, err
	}
	_, err = f.Write(fresh)
	return fresh, errors.Join(err, f.Close())
}

func runLinkSignature(secret []byte, key string, expires int64) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%s\x00%d", key, expires)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

func runPageLink(dir, base, key string, now time.Time) string {
	if base == "" || key == "" {
		return ""
	}
	secret, err := runLinkSecret(dir)
	if err != nil {
		return ""
	}
	expires := now.Add(runLinkLasts).Unix()
	return fmt.Sprintf("%s%s%s?e=%d&k=%s", base, runPagePath, base64.RawURLEncoding.EncodeToString([]byte(key)), expires, runLinkSignature(secret, key, expires))
}

func runLinkFor(dir string) func(key string) string {
	return func(key string) string {
		local, public := ideaLinkBases()
		return runPageLink(dir, firstNonEmpty(public, local), key, time.Now())
	}
}

func runKeyFromLink(dir string, r *http.Request, now time.Time) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(r.URL.Path, runPagePath))
	if err != nil || len(raw) == 0 {
		return "", false
	}
	expires, err := strconv.ParseInt(r.URL.Query().Get("e"), 10, 64)
	if err != nil || now.Unix() > expires {
		return "", false
	}
	secret, err := runLinkSecret(dir)
	if err != nil {
		return "", false
	}
	key := string(raw)
	return key, hmac.Equal([]byte(runLinkSignature(secret, key, expires)), []byte(r.URL.Query().Get("k")))
}

func runPageHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeLaunchError(w, http.StatusMethodNotAllowed, "a run's report is for reading only")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, agentDirUnresolvedMessage)
		return
	}
	now := time.Now()
	key, ok := runKeyFromLink(dir, r, now)
	if !ok {
		if !runPageMisses.allow("run", guestAddress(r), now) {
			w.Header().Set("Retry-After", "60")
			writeLaunchError(w, http.StatusTooManyRequests, "too many tries - wait a minute")
			return
		}
		writeLaunchError(w, http.StatusForbidden, "this link is wrong or has expired - corgi agent history log shows the run on the machine it ran on")
		return
	}
	page, ok := runPageHTML(dir, key)
	if !ok {
		writeLaunchError(w, http.StatusNotFound, "no run for that key")
		return
	}
	setSharedPageHeaders(w, runPageCSP)
	_, _ = fmt.Fprint(w, page)
}

func runPageHTML(dir, key string) (string, bool) {
	var rec watch.FixRecord
	for _, r := range watch.LoadFixLog(dir).Records() {
		if r.Key == key {
			rec = r
		}
	}
	report := runReport(daemon.WholeRunLog(dir, key))
	if rec.Key == "" && report == "" {
		return "", false
	}
	title := firstNonEmpty(rec.Title, rec.Ref, "run")
	facts := []string{rec.Workspace}
	if !rec.StartedAt.IsZero() {
		facts = append(facts, rec.StartedAt.Local().Format("Mon 2 Jan, 15:04"))
	}
	switch {
	case rec.Key != "" && !rec.Done():
		facts = append(facts, "still running")
	case rec.Error != "":
		facts = append(facts, "failed: "+rec.Error)
	}
	var b strings.Builder
	fmt.Fprintf(&b, runPageHead, html.EscapeString(title), html.EscapeString(title), html.EscapeString(strings.Join(nonEmpty(facts), " · ")))
	b.WriteString(runTasksHTML(watch.LoadTasks(dir).Tasks, rec, report))
	b.WriteString(renderMarkdown(firstNonEmpty(report, "The run wrote no report."), blackfriday.NoEmptyLineBeforeBlock|blackfriday.Autolink))
	b.WriteString("</main></body></html>\n")
	return b.String(), true
}

func runTasksHTML(tasks []watch.Task, rec watch.FixRecord, report string) string {
	filed := watch.TasksFiledBy(tasks, rec.Workspace, rec.StartedAt, report)
	if len(filed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<ul class=\"tasks\">")
	for _, t := range filed {
		fmt.Fprintf(&b, "<li><b>%s</b> %s <i>%s</i></li>", html.EscapeString(t.Ref()), html.EscapeString(watch.TaskHeadline(t)), html.EscapeString(t.State))
	}
	b.WriteString("</ul>")
	return b.String()
}

func runReport(log string) string {
	var kept []string
	for _, line := range strings.Split(watch.LastRunOf(log), "\n") {
		if strings.HasPrefix(line, "=== ") || watch.RunOutcome(line) != "" {
			continue
		}
		kept = append(kept, line)
	}
	report := strings.TrimSpace(strings.Join(kept, "\n"))
	if len(report) > runPageMaxBytes {
		report = strings.ToValidUTF8(report[:runPageMaxBytes], "") + "\n\n…"
	}
	return report
}

func nonEmpty(words []string) []string {
	var kept []string
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			kept = append(kept, w)
		}
	}
	return kept
}

const runPageHead = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>%s</title>
<style>
:root{color-scheme:light dark}
body{margin:0;font:16px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:light-dark(#fff,#111);color:light-dark(#1c1c1e,#ececee)}
main{max-width:46rem;margin:0 auto;padding:1.5rem 1.1rem 4rem;overflow-wrap:anywhere}
h1{font-size:1.4rem;line-height:1.25;margin:0}
h2{font-size:1.1rem;margin:2rem 0 .5rem}
h3{font-size:1rem;margin:1.5rem 0 .4rem}
.facts{color:light-dark(#6b6b70,#9a9aa0);margin:.25rem 0 1.5rem;font-size:.9rem}
ul,ol{padding-left:1.25rem}
li{margin:.3rem 0}
.tasks{list-style:none;padding:0;margin:0 0 1.5rem;border-top:1px solid light-dark(#e4e4e7,#2c2c30)}
.tasks li{margin:0;padding:.6rem 0;border-bottom:1px solid light-dark(#e4e4e7,#2c2c30)}
.tasks i{font-style:normal;color:light-dark(#6b6b70,#9a9aa0)}
code{font:.9em ui-monospace,Menlo,monospace;background:light-dark(#f1f1f3,#222226);padding:.1em .3em;border-radius:4px}
pre{overflow-x:auto;background:light-dark(#f1f1f3,#222226);padding:.8rem;border-radius:8px}
pre code{background:none;padding:0}
table{display:block;overflow-x:auto;border-collapse:collapse;font-size:.92rem}
th,td{text-align:left;vertical-align:top;padding:.45rem .7rem .45rem 0;border-bottom:1px solid light-dark(#e4e4e7,#2c2c30);min-width:6rem}
a{color:light-dark(#4650d6,#98a0ff)}
</style></head><body><main>
<h1>%s</h1>
<p class="facts">%s</p>
`
