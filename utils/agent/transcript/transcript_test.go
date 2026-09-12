package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"type":"permission-mode","permissionMode":"auto"}
{"type":"user","uuid":"u1","timestamp":"2026-09-12T10:00:00Z","message":{"role":"user","content":[{"type":"text","text":"fix the login loop"}]}}
{"type":"user","uuid":"u1m","isMeta":true,"timestamp":"2026-09-12T10:00:01Z","message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-09-12T10:00:02Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"Reading the auth code first."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/Users/me/dev/acme/api/auth/session.go"}}]}}
{"type":"user","uuid":"u2","timestamp":"2026-09-12T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package auth\nconst key = \"sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789\"\nvar password = \"hunter22222\""}]}}
{"type":"assistant","uuid":"a2","isSidechain":true,"timestamp":"2026-09-12T10:00:04Z","message":{"role":"assistant","content":[{"type":"text","text":"a sub-agent talking"}]}}
{"type":"assistant","uuid":"a3","timestamp":"2026-09-12T10:00:05Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go test ./auth/...\necho done","description":"Run the auth tests"}}]}}
{"type":"user","uuid":"u3","timestamp":"2026-09-12T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"ok  \tacme/api/auth\t0.4s"}]}]}}
`

// The conversation as a person reads it: prompts, Claude's words, each
// tool call with its subject, each result — thinking, meta rows and side
// chains left out, secrets scrubbed, a read that continues where the
// last one stopped.
func TestReadTellsTheConversationAndScrubsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, off, err := Read(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "user,assistant,tool,result,tool,result" {
		t.Fatalf("kinds: %v", kinds)
	}
	if entries[0].Text != "fix the login loop" || entries[1].Text != "Reading the auth code first." {
		t.Fatalf("words: %+v", entries[:2])
	}
	if entries[2].Tool != "Read" || entries[2].Subject != "api/auth/session.go" || entries[2].ToolID != "t1" {
		t.Fatalf("the tool and its subject: %+v", entries[2])
	}
	if strings.Contains(entries[3].Text, "abcdefghijklmnop") || strings.Contains(entries[3].Text, "hunter2") || !strings.Contains(entries[3].Text, "•••") {
		t.Fatalf("secrets must not leave the machine: %q", entries[3].Text)
	}
	if entries[4].Subject != "go test ./auth/..." || entries[5].Text != "ok  \tacme/api/auth\t0.4s" {
		t.Fatalf("bash and its result: %+v %+v", entries[4], entries[5])
	}
	if off != int64(len(sample)) {
		t.Fatalf("offset %d, want %d", off, len(sample))
	}

	// More arrives, and only that is read next time.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"type":"assistant","uuid":"a4","timestamp":"2026-09-12T10:00:07Z","message":{"role":"assistant","content":[{"type":"text","text":"Tests pass."}]}}` + "\n" + `{"type":"user","uuid":"half"`)
	f.Close()
	more, off2, _ := Read(path, off, 0)
	if len(more) != 1 || more[0].Text != "Tests pass." || off2 <= off {
		t.Fatalf("the new line only, the half one not yet: %+v", more)
	}
	// A shorter file is a new file: read from the top.
	os.WriteFile(path, []byte(sample[:len(sample)/2]), 0o600)
	if again, _, _ := Read(path, off2, 0); len(again) == 0 {
		t.Fatal("a replaced file reads from the top")
	}
}

func TestLastGivesTheTailOfALongConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString(`{"type":"assistant","uuid":"a` + itoa(i) + `","timestamp":"2026-09-12T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"line ` + itoa(i) + `"}]}}` + "\n")
	}
	os.WriteFile(path, []byte(b.String()), 0o600)
	last, off, err := Last(path, 50)
	if err != nil || len(last) != 50 || last[0].Text != "line 450" || last[49].Text != "line 499" {
		t.Fatalf("the newest fifty: %d %v %s", len(last), err, last[0].Text)
	}
	if off != int64(b.Len()) {
		t.Fatal("and the position after them, to continue from")
	}
	long := strings.Repeat("x", MaxText*2)
	cut, truncated := clip(long)
	if !truncated || len(cut) > MaxText+4 {
		t.Fatal("a wall of text is cut")
	}
}

func TestScrubKeepsTheNameAndDropsTheValue(t *testing.T) {
	cases := map[string]string{
		"export GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz012345":     "•••",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456":       "Bearer •••",
		"AWS_KEY=AKIAABCDEFGHIJKLMNOP":                                 "•••",
		"password: correct-horse-battery":                              "password:•••",
		"the api_key = \"0123456789abcdef\" is set":                    "api_key =•••",
		"-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----": "•••",
	}
	for in, want := range cases {
		got := Scrub(in)
		if !strings.Contains(got, want) || got == in {
			t.Errorf("%q → %q, want it to carry %q", in, got, want)
		}
	}
	if Scrub("go test ./auth/... passed in 0.4s") != "go test ./auth/... passed in 0.4s" {
		t.Fatal("plain text is left alone")
	}
}
