package term

import (
	"os"
	"testing"
)

func TestAShellStartsInTheFolderAskedForOrAtHome(t *testing.T) {
	want := t.TempDir()
	if got := shellCmd(want).Dir; got != want {
		t.Fatalf("dir %q, want %q", got, want)
	}
	home, _ := os.UserHomeDir()
	for _, missing := range []string{"", "/no/such/folder"} {
		if got := shellCmd(missing).Dir; got != home {
			t.Fatalf("%q started in %q, want home", missing, got)
		}
	}
}
