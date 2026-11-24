package cmd

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func grantHome(t *testing.T, s watch.Secrets) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	home, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(home, 0o700)
	old := phoneForgeSecrets
	phoneForgeSecrets = func(string, string) watch.Secrets { return s }
	t.Cleanup(func() { phoneForgeSecrets = old })
	return home
}

func grantCall(headers map[string]string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, "/launch/reviews/grant", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	launchReviewsGrantHandler(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestThePhoneGetsTheReviewAccessOnlyWhenTurnedOnForIt(t *testing.T) {
	home := grantHome(t, watch.Secrets{GitHub: "gh-tok", GitLab: "gl-tok", GitLabURL: "https://git.acme.dev/"})
	sealed := map[string]string{deviceHeader: "phone", sealedHeader: "1"}

	if code, out := grantCall(sealed); code != http.StatusForbidden || !strings.Contains(out["error"].(string), "corgi agent reviews phone on") {
		t.Fatalf("off = %d %v", code, out)
	}
	if err := savePhoneReviews(home, phoneReviews{Devices: map[string]phoneReviewsDevice{"phone": {Since: time.Now()}}}); err != nil {
		t.Fatal(err)
	}
	for name, headers := range map[string]map[string]string{
		"no device":  {sealedHeader: "1"},
		"not sealed": {deviceHeader: "phone"},
		"a viewer":   {deviceHeader: "phone", sealedHeader: "1", viewerHeader: "1"},
		"another":    {deviceHeader: "tablet", sealedHeader: "1"},
	} {
		if code, out := grantCall(headers); code != http.StatusForbidden {
			t.Fatalf("%s = %d %v", name, code, out)
		}
	}
	code, out := grantCall(sealed)
	if code != http.StatusOK {
		t.Fatalf("on = %d %v", code, out)
	}
	forge := out["forges"].([]any)[0].(map[string]any)
	if forge["github"] != "gh-tok" || forge["githubApi"] != phoneGitHubAPI || forge["gitlab"] != "gl-tok" || forge["gitlabUrl"] != "https://git.acme.dev" {
		t.Fatalf("forge = %v", forge)
	}
	if loadPhoneReviews(home).Devices["phone"].Given.IsZero() {
		t.Fatal("the hand-over was not written down")
	}
}

func TestAGitLabOnPlainHTTPIsLeftOut(t *testing.T) {
	home := grantHome(t, watch.Secrets{GitLab: "gl-tok", GitLabURL: "http://git.acme.dev"})
	_ = savePhoneReviews(home, phoneReviews{Devices: map[string]phoneReviewsDevice{"phone": {}}})
	if code, out := grantCall(map[string]string{deviceHeader: "phone", sealedHeader: "1"}); code != http.StatusNotFound {
		t.Fatalf("plain http = %d %v", code, out)
	}
}

func TestADeviceCannotSayItIsSealed(t *testing.T) {
	home := grantHome(t, watch.Secrets{GitHub: "gh-tok"})
	store := pairing.StorePath(home)
	session, code, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	token, err := pairing.PairWithRole(store, session, code, "phone", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = savePhoneReviews(home, phoneReviews{Devices: map[string]phoneReviewsDevice{"phone": {}}})
	srv := httptest.NewServer(launchAuth("", http.HandlerFunc(launchReviewsGrantHandler), store))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/launch/reviews/grant", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(sealedHeader, "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a device without a key got the tokens: %d", resp.StatusCode)
	}
	if viewerMay(http.MethodGet, "/launch/reviews/grant") {
		t.Fatal("a viewer may ask for the tokens")
	}
}

func pairKeyedPhone(t *testing.T, home, name string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	session, code, err := pairing.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairing.PairWithKey(pairing.StorePath(home), session, code, name, pairing.PublicKeyString(key.PublicKey())); err != nil {
		t.Fatal(err)
	}
}

func TestReviewsWithoutTheMacAreTurnedOnPerPhone(t *testing.T) {
	home := grantHome(t, watch.Secrets{GitHub: "gh-tok"})
	pairKeyedPhone(t, home, "phone")
	pairKeyedPhone(t, home, "tablet")

	out := captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, nil) })
	if !strings.Contains(out, "phone") || !strings.Contains(out, "off") {
		t.Fatalf("status = %q", out)
	}
	captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, []string{"on", "phone"}) })
	if !phoneReviewsAllowed(home, "phone") || phoneReviewsAllowed(home, "tablet") {
		t.Fatalf("on = %+v", loadPhoneReviews(home))
	}
	if code, _ := grantCall(map[string]string{deviceHeader: "phone", sealedHeader: "1"}); code != http.StatusOK {
		t.Fatalf("grant = %d", code)
	}
	out = captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, nil) })
	if !strings.Contains(out, "last taken") {
		t.Fatalf("status after = %q", out)
	}
	captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, []string{"off", "phone"}) })
	if phoneReviewsAllowed(home, "phone") {
		t.Fatal("off did not take it back")
	}
	captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, []string{"on", "phone"}) })
	captureStdout(t, func() { agentReviewsPhoneCmd.Run(agentReviewsPhoneCmd, []string{"off"}) })
	if len(loadPhoneReviews(home).Devices) != 0 {
		t.Fatal("off for everyone left someone")
	}
	if got := deviceNames(phoneReviewsCandidates(home)); len(got) != 2 {
		t.Fatalf("candidates = %v", got)
	}
}

func TestAReviewIsSentFromTheCommandLine(t *testing.T) {
	posts := reviewsHome(t, watch.PullStatus{State: "open", Checks: "passing", Review: "approved"})
	flags := agentReviewsSendCmd.Flags()
	_ = flags.Set("verdict", "request")
	_ = flags.Set("notes", `[{"path":"src/retry.ts","line":2,"body":"cap this"}]`)
	t.Cleanup(func() {
		_ = flags.Set("verdict", "comment")
		_ = flags.Set("notes", "")
		_ = flags.Set("head", "")
	})
	out := captureStdout(t, func() { agentReviewsSendCmd.Run(agentReviewsSendCmd, []string{"https://github.com/acme/api/pull/3"}) })
	if !strings.Contains(out, "changes requested") || len(*posts) != 1 || !strings.Contains((*posts)[0], "cap this") {
		t.Fatalf("send = %q %v", out, *posts)
	}
	_ = flags.Set("head", "not-the-head")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	home, _ := agentDir()
	if _, status, why := runReviewAction(ctx, home, reviewRequest{Link: "https://github.com/acme/api/pull/3", Do: "send", Verdict: "approve", Head: "not-the-head"}, "cli"); status != http.StatusConflict || !strings.Contains(why, "new commits") {
		t.Fatalf("a moved pull request = %d %q", status, why)
	}
	if len(*posts) != 1 {
		t.Fatalf("a review landed on commits nobody read: %v", *posts)
	}
	if _, status, _ := runReviewAction(ctx, home, reviewRequest{Link: "https://github.com/acme/api/pull/3", Do: "merge"}, "cli"); status != http.StatusBadRequest {
		t.Fatalf("someone else's pull request merged: %d", status)
	}
}
