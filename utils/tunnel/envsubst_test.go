package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	t.Run("empty path returns empty map no error", func(t *testing.T) {
		m, err := LoadEnvFile("")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if len(m) != 0 {
			t.Errorf("want empty map, got %v", m)
		}
	})

	t.Run("missing file returns empty map no error", func(t *testing.T) {
		m, err := LoadEnvFile(filepath.Join(t.TempDir(), "nope.env"))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if len(m) != 0 {
			t.Errorf("want empty map, got %v", m)
		}
	})

	t.Run("parses KEY=VALUE skipping comments and blanks", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "x.env")
		body := `# comment

FOO=bar
BAZ="quoted"
SINGLE='also quoted'
EMPTY=
NO_EQUALS
=novalueneededforkey
SPACED = padded
`
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		m, err := LoadEnvFile(p)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if m["FOO"] != "bar" {
			t.Errorf("FOO = %q", m["FOO"])
		}
		if m["BAZ"] != "quoted" {
			t.Errorf("BAZ = %q want quoted", m["BAZ"])
		}
		if m["SINGLE"] != "also quoted" {
			t.Errorf("SINGLE = %q", m["SINGLE"])
		}
		if _, ok := m["EMPTY"]; !ok {
			t.Errorf("EMPTY missing")
		}
		if _, ok := m["NO_EQUALS"]; ok {
			t.Errorf("NO_EQUALS should not be parsed")
		}
		if m["SPACED"] != "padded" {
			t.Errorf("SPACED = %q want padded", m["SPACED"])
		}
	})
}

func TestSubstitute(t *testing.T) {
	t.Setenv("SHELL_VAR", "from-shell")

	t.Run("brace and bare refs substituted", func(t *testing.T) {
		fileEnv := map[string]string{"FOO": "fileval"}
		var missing []string
		got := Substitute("a=${FOO} b=$FOO c=${SHELL_VAR}", fileEnv, &missing)
		if got != "a=fileval b=fileval c=from-shell" {
			t.Errorf("got %q", got)
		}
		if len(missing) != 0 {
			t.Errorf("unexpected missing: %v", missing)
		}
	})

	t.Run("shell takes precedence over file", func(t *testing.T) {
		fileEnv := map[string]string{"SHELL_VAR": "from-file"}
		got := Substitute("$SHELL_VAR", fileEnv, nil)
		if got != "from-shell" {
			t.Errorf("got %q want from-shell (shell wins)", got)
		}
	})

	t.Run("missing keys recorded and ref preserved", func(t *testing.T) {
		var missing []string
		got := Substitute("hi ${NOPE}", map[string]string{}, &missing)
		if got != "hi ${NOPE}" {
			t.Errorf("got %q", got)
		}
		if len(missing) != 1 || missing[0] != "NOPE" {
			t.Errorf("missing = %v", missing)
		}
	})

	t.Run("nil missing slice tolerated", func(t *testing.T) {
		got := Substitute("$NOTSET_X", map[string]string{}, nil)
		if got != "$NOTSET_X" {
			t.Errorf("got %q", got)
		}
	})
}

func TestMissingError(t *testing.T) {
	err := MissingError("services.foo.url", []string{"A", "B", "A"})
	if err == nil {
		t.Fatal("nil err")
	}
	msg := err.Error()
	if !strings.Contains(msg, "services.foo.url") {
		t.Errorf("missing field name: %q", msg)
	}
	if !strings.Contains(msg, "A") || !strings.Contains(msg, "B") {
		t.Errorf("missing keys: %q", msg)
	}
	if strings.Count(msg, "A") != 1 {
		t.Errorf("dedup failed: %q", msg)
	}
}

func TestProvidersNames(t *testing.T) {
	got := Names()
	if len(got) != len(Providers) {
		t.Errorf("Names len = %d, want %d", len(got), len(Providers))
	}
	want := map[string]bool{"cloudflared": true, "ngrok": true, "localtunnel": true}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected provider %q", n)
		}
	}
}
