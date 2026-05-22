package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestInterpolateBasic(t *testing.T) {
	out, err := Interpolate([]byte("password: ${PW}"), lookupFrom(map[string]string{"PW": "secret"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "password: secret" {
		t.Errorf("got %q", out)
	}
}

func TestInterpolateDefault(t *testing.T) {
	// Unset -> default.
	out, err := Interpolate([]byte("port: ${PORT:-5432}"), lookupFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "port: 5432" {
		t.Errorf("unset default: got %q", out)
	}

	// Set -> value, default ignored.
	out, err = Interpolate([]byte("port: ${PORT:-5432}"), lookupFrom(map[string]string{"PORT": "6000"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "port: 6000" {
		t.Errorf("set value: got %q", out)
	}
}

func TestInterpolateUnsetNoDefaultErrors(t *testing.T) {
	_, err := Interpolate([]byte("x: ${MISSING}"), lookupFrom(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), ErrMissingField) || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("error should name code and var: %v", err)
	}
}

func TestInterpolateTolerantLeavesUnresolved(t *testing.T) {
	out, unresolved := InterpolateTolerant([]byte("x: ${MISSING}"), lookupFrom(nil))
	if string(out) != "x: ${MISSING}" {
		t.Errorf("token should be left untouched: got %q", out)
	}
	if len(unresolved) != 1 || unresolved[0] != "MISSING" {
		t.Errorf("expected MISSING in unresolved, got %#v", unresolved)
	}
}

func TestInterpolateTolerantDottedUntouched(t *testing.T) {
	// Cross-service ${producer.VAR} refs must be left fully untouched and NOT
	// reported as unresolved — the cross-service resolver owns them.
	out, unresolved := InterpolateTolerant([]byte("host: ${a.b}"), lookupFrom(nil))
	if string(out) != "host: ${a.b}" {
		t.Errorf("dotted form should be untouched: got %q", out)
	}
	if len(unresolved) != 0 {
		t.Errorf("dotted form must not be reported: %#v", unresolved)
	}
}

func TestInterpolateTolerantDedupesAndKeepsSetDefaultEscape(t *testing.T) {
	in := []byte("${MISSING}-${MISSING}-${SET}-${DEF:-d}-$${ESC}")
	out, unresolved := InterpolateTolerant(in, lookupFrom(map[string]string{"SET": "s"}))
	if string(out) != "${MISSING}-${MISSING}-s-d-${ESC}" {
		t.Errorf("got %q", out)
	}
	if len(unresolved) != 1 || unresolved[0] != "MISSING" {
		t.Errorf("expected single deduped MISSING, got %#v", unresolved)
	}
}

func TestInterpolateInsideStartCommand(t *testing.T) {
	// A braced ${VAR} inside a start command string is resolved at LOAD time
	// (baked into the parsed Start entry), while $${VAR} is left as the literal
	// ${VAR} for the runtime shell to expand.
	in := []byte("start:\n  - echo ${MYVAR}\n  - echo $${MYVAR}\n")
	out, unresolved := InterpolateTolerant(in, lookupFrom(map[string]string{"MYVAR": "hello"}))
	if len(unresolved) != 0 {
		t.Fatalf("expected nothing unresolved, got %#v", unresolved)
	}

	var parsed struct {
		Start []string `yaml:"start"`
	}
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Start) != 2 {
		t.Fatalf("expected 2 start entries, got %#v", parsed.Start)
	}
	if parsed.Start[0] != "echo hello" {
		t.Errorf("set var should be baked at load: got %q", parsed.Start[0])
	}
	if parsed.Start[1] != "echo ${MYVAR}" {
		t.Errorf("escaped var should defer to shell: got %q", parsed.Start[1])
	}
}

func TestInterpolateEscape(t *testing.T) {
	// $${X} -> literal ${X}, no lookup attempted.
	out, err := Interpolate([]byte("cmd: $${HOME}/bin"), lookupFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "cmd: ${HOME}/bin" {
		t.Errorf("escape: got %q", out)
	}
}

func TestInterpolateMultiplePerLine(t *testing.T) {
	out, err := Interpolate([]byte("${A}-${B}"), lookupFrom(map[string]string{"A": "1", "B": "2"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "1-2" {
		t.Errorf("multi: got %q", out)
	}
}

func TestInterpolateBareDollarUntouched(t *testing.T) {
	// Bare $VAR is not a braced form and must be left as-is.
	out, err := Interpolate([]byte("run: echo $HOME"), lookupFrom(map[string]string{"HOME": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "run: echo $HOME" {
		t.Errorf("bare dollar: got %q", out)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	body := "# comment\n\nFOO=bar\nQUOTED=\"q v\"\n  SPACED = trimmed \n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadDotEnv(p)
	if err != nil {
		t.Fatal(err)
	}
	if m["FOO"] != "bar" || m["QUOTED"] != "q v" || m["SPACED"] != "trimmed" {
		t.Errorf("parsed: %#v", m)
	}
}

func TestLoadDotEnvMissingIsEmpty(t *testing.T) {
	m, err := LoadDotEnv(filepath.Join(t.TempDir(), "nope.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty, got %#v", m)
	}
}

func TestEnvThenDotEnvPrecedence(t *testing.T) {
	t.Setenv("PREC_KEY", "from-env")
	lookup := EnvThenDotEnv(map[string]string{"PREC_KEY": "from-dotenv", "ONLY_FILE": "fv"})

	if v, ok := lookup("PREC_KEY"); !ok || v != "from-env" {
		t.Errorf("env should win: %q %v", v, ok)
	}
	if v, ok := lookup("ONLY_FILE"); !ok || v != "fv" {
		t.Errorf("dotenv fallback: %q %v", v, ok)
	}
	if _, ok := lookup("ABSENT_XYZ"); ok {
		t.Error("absent should be false")
	}
}
