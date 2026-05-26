package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestMaybeOpenOnReady_OpensWhenEnabled(t *testing.T) {
	pf, po := openOnReadyFlag, browserOpener
	t.Cleanup(func() { openOnReadyFlag, browserOpener = pf, po })

	openOnReadyFlag = true
	var gotURL, gotBrowser string
	browserOpener = func(url, browser string) error { gotURL, gotBrowser = url, browser; return nil }

	svc := utils.Service{
		ServiceName: "web", Port: 5173,
		OpenOnReady: &utils.OpenOnReady{Enabled: true, Scheme: "https", Path: "/login", Browser: "Google Chrome"},
	}
	maybeOpenOnReady(svc)
	if gotURL != "https://localhost:5173/login" || gotBrowser != "Google Chrome" {
		t.Fatalf("got url=%q browser=%q", gotURL, gotBrowser)
	}
}

func TestMaybeOpenOnReady_SkipsWhenFlagOff(t *testing.T) {
	pf, po := openOnReadyFlag, browserOpener
	t.Cleanup(func() { openOnReadyFlag, browserOpener = pf, po })

	openOnReadyFlag = false
	called := false
	browserOpener = func(string, string) error { called = true; return nil }
	maybeOpenOnReady(utils.Service{Port: 3000, OpenOnReady: &utils.OpenOnReady{Enabled: true}})
	if called {
		t.Fatal("must not open without --open")
	}
}

func TestMaybeOpenOnReady_SkipsWhenNotOptedIn(t *testing.T) {
	pf, po := openOnReadyFlag, browserOpener
	t.Cleanup(func() { openOnReadyFlag, browserOpener = pf, po })
	openOnReadyFlag = true
	called := false
	browserOpener = func(string, string) error { called = true; return nil }
	maybeOpenOnReady(utils.Service{Port: 3000}) // no OpenOnReady
	if called {
		t.Fatal("must not open when service did not opt in")
	}
}
