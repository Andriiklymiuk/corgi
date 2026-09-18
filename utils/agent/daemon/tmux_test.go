package daemon

import (
	"reflect"
	"testing"
)

func TestTmuxSendArgsSplitsLiteralTextFromKeys(t *testing.T) {
	cases := []struct {
		text  string
		enter bool
		want  []string
	}{
		{"fix the tests", false, []string{"send-keys", "-t", "%3", "-l", "fix the tests"}},
		{"fix the tests", true, []string{"send-keys", "-t", "%3", "-l", "fix the tests", ";", "send-keys", "-t", "%3", "Enter"}},
		{"\r", false, []string{"send-keys", "-t", "%3", "Enter"}},
		{"2\r", false, []string{"send-keys", "-t", "%3", "-l", "2", ";", "send-keys", "-t", "%3", "Enter"}},
		{"\x1b", false, []string{"send-keys", "-t", "%3", "Escape"}},
		{"a -l b", false, []string{"send-keys", "-t", "%3", "-l", "a -l b"}},
	}
	for _, c := range cases {
		if got := tmuxSendArgs("%3", c.text, c.enter); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q enter=%v:\n got %q\nwant %q", c.text, c.enter, got, c.want)
		}
	}
}

func TestTmuxFocusSelectsWindowThenPane(t *testing.T) {
	want := []string{"select-window", "-t", "%3", ";", "select-pane", "-t", "%3"}
	if got := tmuxFocusArgs("%3"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}
