package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestVocabAnswersEachKindOfName(t *testing.T) {
	cases := []struct {
		args []string
		kind string
	}{
		{[]string{"E_PORT_CONFLICT"}, "code"},
		{[]string{"w_no_branch"}, "code"},
		{[]string{"depends_on_db"}, "field"},
		{[]string{"healthCheck"}, "field"},
		{[]string{"run"}, "command"},
		{[]string{"agent", "watch"}, "command"},
		{[]string{"corgi"}, "command"},
		{[]string{"--detach"}, "flag"},
	}
	for _, tc := range cases {
		got, ok := resolveVocab(tc.args)
		if !ok {
			t.Errorf("%v: no answer", tc.args)
			continue
		}
		var kind string
		switch a := got.(type) {
		case vocabCode:
			kind = a.Kind
		case vocabField:
			kind = a.Kind
		case vocabCommand:
			kind = a.Kind
		case vocabFlagOwners:
			kind = a.Kind
		}
		if kind != tc.kind {
			t.Errorf("%v: kind %q, want %q", tc.args, kind, tc.kind)
		}
	}
	if _, ok := resolveVocab([]string{"helthCheck"}); ok {
		t.Error("a typo must not resolve")
	}
}

func TestVocabRunListsItsFlagsAndTheSameNamedKey(t *testing.T) {
	got, _ := resolveVocab([]string{"run"})
	c := got.(vocabCommand)
	has := map[string]bool{}
	for _, f := range c.Flags {
		has[f.Name] = true
	}
	for _, want := range []string{"--detach", "--services", "--profile"} {
		if !has[want] {
			t.Errorf("corgi run's flags miss %s", want)
		}
	}
	if len(c.AlsoField) == 0 || c.AlsoField[0].Path != "e2e.run" {
		t.Errorf("run is also e2e.run: %+v", c.AlsoField)
	}
}

func TestVocabDriverListsTheKnownDrivers(t *testing.T) {
	got, _ := resolveVocab([]string{"driver"})
	f := got.(vocabField)
	if len(f.Places) == 0 || len(f.Places[0].Enum) != len(utils.KnownDrivers) {
		t.Fatalf("got %+v", f)
	}
}

func TestVocabFlagNamesEveryOwner(t *testing.T) {
	got, _ := resolveVocab([]string{"--detach"})
	owners := got.(vocabFlagOwners).Commands
	want := map[string]bool{"corgi run": false, "corgi restart": false}
	for _, o := range owners {
		if _, ok := want[o]; ok {
			want[o] = true
		}
	}
	for c, seen := range want {
		if !seen {
			t.Errorf("%s takes --detach: %v", c, owners)
		}
	}
}

func TestUnknownFlagName(t *testing.T) {
	cases := map[string]string{
		"unknown flag: --wach":              "--wach",
		"unknown shorthand flag: 'q' in -q": "'q'",
		"some other error":                  "",
	}
	for msg, want := range cases {
		if got := unknownFlagName(errors.New(msg)); got != want {
			t.Errorf("%q: got %q, want %q", msg, got, want)
		}
	}
}

func TestFlagRefusalNamesTheNearestFlag(t *testing.T) {
	var code int
	prev := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = prev })
	out := captureStdoutForVocab(t, func() {
		utils.JSONOutput = true
		defer func() { utils.JSONOutput = false }()
		_ = flagRefusal(statusCmd, errors.New("unknown flag: --wach"))
	})
	if code != 2 {
		t.Errorf("exit %d, want 2 (usage)", code)
	}
	for _, want := range []string{`"code": "E_USAGE"`, `did you mean --watch?`, "corgi vocab status"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestFeedbackNotesRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	notes, err := readFeedback()
	if err != nil || len(notes) != 0 {
		t.Fatalf("a fresh machine has no notes: %v %v", notes, err)
	}
	for _, n := range []string{"first", "second"} {
		if _, err := appendFeedback(feedbackNote{Version: APP_VERSION, Note: n, Command: "run"}); err != nil {
			t.Fatal(err)
		}
	}
	notes, err = readFeedback()
	if err != nil || len(notes) != 2 || notes[0].Note != "first" || notes[1].Command != "run" {
		t.Fatalf("got %+v %v", notes, err)
	}
	info, err := os.Stat(filepath.Join(home, ".corgi", "feedback.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("notes are private to the user: %v", info.Mode())
	}
}

func captureStdoutForVocab(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	buf := make([]byte, 64<<10)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestMCPErrorsCarryTheFix(t *testing.T) {
	got := mcpErrorText("E_NOT_RUNNING: service web is not running")
	if !strings.HasSuffix(got, "\nfix: start it with `corgi run --detach` first") {
		t.Errorf("got %q", got)
	}
	if got := mcpErrorText("plain failure"); got != "plain failure" {
		t.Errorf("an uncoded error is left alone: %q", got)
	}
}
