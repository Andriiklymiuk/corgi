package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestFilterEnvCheckRowsKeepsRequestedOrder(t *testing.T) {
	rows := []utils.EnvCheckRow{
		{Service: "api"}, {Service: "web"}, {Service: "worker"},
	}
	got, err := filterEnvCheckRows(rows, []string{"worker", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Service != "worker" || got[1].Service != "api" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterEnvCheckRowsNoNamesReturnsAll(t *testing.T) {
	rows := []utils.EnvCheckRow{{Service: "api"}}
	got, err := filterEnvCheckRows(rows, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// A typo'd service name must error — silently checking everything (or
// nothing) would let the caller read the exit code as their service's verdict.
func TestFilterEnvCheckRowsRejectsUnknownService(t *testing.T) {
	rows := []utils.EnvCheckRow{{Service: "api"}}
	_, err := filterEnvCheckRows(rows, []string{"apo"})
	if err == nil || !strings.Contains(err.Error(), "apo") {
		t.Fatalf("expected an error naming the unknown service, got %v", err)
	}
}
