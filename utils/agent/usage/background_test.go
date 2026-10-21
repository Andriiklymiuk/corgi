package usage

import (
	"strings"
	"testing"
)

func TestBackgroundCountsWorkStillRunning(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command running in background with ID: b1."}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"Async agent launched successfully."}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"grep: Command running in background with ID"}]}}`,
		`{"type":"user","message":{"content":"<task-notification> <tool-use-id>t2</tool-use-id> <status>completed</status> </task-notification>"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Waiting for CI, then I announce."}]}}`,
	}, "\n")
	if got := backgroundFrom(strings.NewReader(transcript), false); got != 1 {
		t.Fatalf("one shell still runs, the agent finished, output that only mentions a launch is none: got %d", got)
	}
}
