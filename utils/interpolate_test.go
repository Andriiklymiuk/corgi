package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
