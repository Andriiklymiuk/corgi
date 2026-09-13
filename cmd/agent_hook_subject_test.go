package cmd

import (
	"encoding/json"
	"testing"
)

// The hook says one word about what a tool would do, from the input it
// then drops: reads, writes, destructive.
func TestRiskOfAToolInput(t *testing.T) {
	cases := []struct{ tool, input, want string }{
		{"Read", `{"file_path":"/x/a.go"}`, "reads"},
		{"Grep", `{"pattern":"x"}`, "reads"},
		{"Edit", `{"file_path":"/x/a.go"}`, "writes"},
		{"Bash", `{"command":"go test ./..."}`, "writes"},
		{"Bash", `{"command":"git push origin main"}`, "writes"},
		{"Bash", `{"command":"git push --force origin main"}`, "destructive"},
		{"Bash", `{"command":"rm -rf build"}`, "destructive"},
		{"Bash", `{"command":"cd x && sudo make install"}`, "destructive"},
		{"Bash", `{"command":"psql -c 'drop table users'"}`, "destructive"},
		{"Bash", `{"command":"echo drop it"}`, "writes"},
		{"Unknown", `{}`, ""},
	}
	for _, c := range cases {
		if got := riskOf(c.tool, json.RawMessage(c.input)); got != c.want {
			t.Errorf("riskOf(%s, %s) = %q, want %q", c.tool, c.input, got, c.want)
		}
	}
}
