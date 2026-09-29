package watch

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type failingSource struct {
	name  string
	err   error
	polls int
}

func (f *failingSource) Name() string { return f.name }
func (f *failingSource) Poll(ctx context.Context, cursor Cursor) ([]Event, Cursor, error) {
	f.polls++
	return nil, cursor, f.err
}

func TestIsNetworkError(t *testing.T) {
	dns := &net.DNSError{Err: "no such host", Name: "gitlab.com", IsNotFound: true}
	if !IsNetworkError(fmt.Errorf("gitlab todos: %w", dns)) {
		t.Error("a wrapped DNS error is a network error")
	}
	if IsNetworkError(errors.New("503 Service Unavailable")) {
		t.Error("an answer from the host is not a network error")
	}
}

func TestOnceLogsAnUnreachableSourceOnceAndSkipsTheRestWithoutDNS(t *testing.T) {
	dns := &net.DNSError{Err: "no such host", Name: "gitlab.com", IsNotFound: true}
	first := &failingSource{name: "gitlab", err: fmt.Errorf("todos: %w", dns)}
	second := &failingSource{name: "jira", err: fmt.Errorf("search: %w", dns)}
	var lines []string
	w := &Watch{
		Workspace: "ws",
		Sources:   []Source{first, second},
		State:     LoadState(t.TempDir()),
		Log:       func(s string) { lines = append(lines, s) },
	}
	w.Once(context.Background(), time.Now())
	w.Once(context.Background(), time.Now())
	if second.polls != 0 {
		t.Errorf("jira polled %d times, want 0 while DNS is down", second.polls)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "cannot reach") {
		t.Errorf("lines = %q, want one quiet line for the outage", lines)
	}
}
