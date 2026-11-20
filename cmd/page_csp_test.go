package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSharedPagesRunOnlyTheirOwnScript(t *testing.T) {
	for name, c := range map[string]struct {
		policy string
		pages  []string
	}{
		"board": {boardPagePolicy(), []string{boardPageHTML}},
		"idea":  {ideaPagePolicy(), []string{ideaPageHTML, ideaIndexHTML}},
	} {
		if strings.Contains(c.policy, inlineScriptSource) {
			t.Fatalf("%s: any inline script may still run: %s", name, c.policy)
		}
		hashes := scriptHashes(c.pages...)
		if len(hashes) == 0 {
			t.Fatalf("%s: no script found to pin", name)
		}
		for _, hash := range hashes {
			if !strings.Contains(c.policy, hash) {
				t.Fatalf("%s: the page's own script is not allowed: %s", name, hash)
			}
		}
		for _, page := range c.pages {
			if strings.Contains(page, " onclick=") || strings.Contains(page, " onerror=") || strings.Contains(page, "javascript:") {
				t.Fatalf("%s: an inline handler would be blocked by the policy", name)
			}
		}
	}
}

func TestTheIdeaShellKeepsItsScriptUnchangedForEveryReader(t *testing.T) {
	phoneBoard(t, false)
	for _, trusted := range []bool{true, false} {
		shell := ideaShellFor(httptest.NewRequest(http.MethodGet, "/idea/brewshift?k=abc", nil), "brewshift", trusted)
		for _, hash := range scriptHashes(shell) {
			if !strings.Contains(ideaPagePolicy(), hash) {
				t.Fatalf("trusted=%v: the served page's script differs from the pinned one", trusted)
			}
		}
	}
}

func TestSharedPagesSendTheirHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	boardShareHandler()(rec, httptest.NewRequest(http.MethodGet, "/board", nil))
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "'sha256-") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("board policy: %q", got)
	}
	if rec.Header().Get("Permissions-Policy") == "" || rec.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" {
		t.Fatalf("board headers: %v", rec.Header())
	}
}
