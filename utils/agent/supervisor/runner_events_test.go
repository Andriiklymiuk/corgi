package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRunnerEmitsLifecycleEvents(t *testing.T) {
	start, _ := scriptedStarter(
		&fakeProcess{pid: 7, code: 0, uptime: 20 * time.Millisecond},
	)
	r := testRunner(t, start)

	var mu sync.Mutex
	var got []RunEvent
	r.OnEvent = func(e RunEvent) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) >= 2
	})
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if got[0].Kind != "started" || got[0].PID != 7 {
		t.Errorf("first event = %+v, want started pid 7", got[0])
	}
	var exited *RunEvent
	for i := range got {
		if got[i].Kind == "exited" {
			exited = &got[i]
			break
		}
	}
	if exited == nil || exited.Cause == "" {
		t.Errorf("an exit must carry its cause, events: %+v", got)
	}
}

func TestRunnerEmitsSessionEvent(t *testing.T) {
	r := testRunner(t, nil)
	var got []RunEvent
	r.OnEvent = func(e RunEvent) { got = append(got, e) }

	r.addSessionLink("session_01A")
	r.addSessionLink("session_01A")

	if len(got) != 1 || got[0].Kind != "session" || got[0].URL != "https://claude.ai/code/session_01A" {
		t.Errorf("events = %+v, want one session event with the canonical URL", got)
	}
}
