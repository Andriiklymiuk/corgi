package utils

import (
	"errors"
	"strings"
	"testing"
)

func TestBeforeStartFailuresRecordAndReport(t *testing.T) {
	ResetBeforeStartFailures()
	t.Cleanup(ResetBeforeStartFailures)

	if err := BeforeStartFailureError(); err != nil {
		t.Fatalf("no failures means no error, got %v", err)
	}

	// nil must not register a failure.
	RecordBeforeStartFailure("api", nil)
	if got := BeforeStartFailed(); len(got) != 0 {
		t.Errorf("nil error must not record, got %v", got)
	}

	RecordBeforeStartFailure("web", errors.New("exit status 1"))
	RecordBeforeStartFailure("api", errors.New("exit status 127"))

	// Sorted, so the message is stable across runs.
	got := BeforeStartFailed()
	if len(got) != 2 || got[0] != "api" || got[1] != "web" {
		t.Errorf("expected [api web], got %v", got)
	}

	err := BeforeStartFailureError()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"api", "exit status 127", "web", "exit status 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q, got %q", want, err)
		}
	}
}

// corgi restart re-enters the run in the same process; a failure from the last
// boot must not fail the next one.
func TestBeforeStartFailuresResetBetweenRuns(t *testing.T) {
	ResetBeforeStartFailures()
	t.Cleanup(ResetBeforeStartFailures)

	RecordBeforeStartFailure("api", errors.New("exit status 1"))
	if BeforeStartFailureError() == nil {
		t.Fatal("expected the failure to register")
	}

	ResetBeforeStartFailures()
	if err := BeforeStartFailureError(); err != nil {
		t.Errorf("a new run starts clean, got %v", err)
	}
}
