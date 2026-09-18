package daemon

import (
	"testing"
	"time"
)

func TestASilentWorkspaceNeverRings(t *testing.T) {
	d := testDaemon(t)
	got := make(chan string, 4)
	d.Notify = func(_, body string) { got <- body }
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: "/tmp/acme", Silent: true}, {Workspace: "shop", Dir: "/tmp/shop"}}
	d.notifyAttention("corgi agent · acme", "fixed ABC-1", "acme")
	d.notifyAttention("corgi agent · acme", "needs you", "/tmp/acme/api")
	select {
	case body := <-got:
		t.Fatalf("silent workspace rang: %q", body)
	case <-time.After(50 * time.Millisecond):
	}
	d.notifyAttention("corgi agent · shop", "fixed SHOP-1", "shop")
	select {
	case body := <-got:
		if body != "fixed SHOP-1" {
			t.Fatalf("body %q", body)
		}
	case <-time.After(time.Second):
		t.Fatal("the other workspace rings")
	}
	if d.silenced("/tmp/acme-other/x") {
		t.Fatal("a sibling folder with the same prefix is not under the workspace")
	}
}
