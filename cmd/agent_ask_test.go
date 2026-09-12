package cmd

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// The chief gets the board, not the souls or the transcripts, and the
// phone's Ask box carries the answer back; an empty question is refused
// before any claude runs.
func TestAskHandsTheBoardToClaudeAndTheAnswerBack(t *testing.T) {
	dir := phoneBoard(t, true,
		sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Status: sessions.StatusNeedsInput, Pending: &sessions.Pending{Tool: "Bash", Subject: "go test"}, Ticket: "APP-1"},
		sessions.Session{ID: "s2", Label: "web", Display: "web·cart", Status: sessions.StatusWorking, Detail: "Edit cart.ts", Bot: "reviewer"},
	)
	orig := runClaudePrint
	defer func() { runClaudePrint = orig }()
	var seenPrompt, seenSystem, seenModel string
	runClaudePrint = func(_ context.Context, model, system, prompt string) (string, error) {
		seenModel, seenSystem, seenPrompt = model, system, prompt
		return "Look at api·auth first: it waits on Bash go test.", nil
	}
	answer, err := askBoard(context.Background(), dir, "what first?")
	if err != nil || !strings.Contains(answer, "api·auth") {
		t.Fatalf("answer: %q %v", answer, err)
	}
	if seenModel != askModel || !strings.Contains(seenSystem, "chief") {
		t.Fatalf("model %q system %q", seenModel, seenSystem[:40])
	}
	for _, want := range []string{`"name":"api·auth"`, `"waitingOn":"Bash go test"`, `"ticket":"APP-1"`, `"bot":"reviewer"`, "QUESTION: what first?"} {
		if !strings.Contains(seenPrompt, want) {
			t.Fatalf("the board reaches claude: missing %s in %s", want, seenPrompt)
		}
	}
	if _, err := askBoard(context.Background(), dir, "   "); err == nil {
		t.Fatal("an empty question runs nothing")
	}

	rec := post(launchAskHandler, "/launch/ask", `{"question":"what first?"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "api·auth first") {
		t.Fatalf("phone: %d %s", rec.Code, rec.Body)
	}
	if rec := post(launchAskHandler, "/launch/ask", `{"question":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty: %d", rec.Code)
	}
}
