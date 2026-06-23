package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

// waitForServicesReady should probe every service that has a port and skip
// the ones that don't, using the injected readiness function.
func TestWaitForServicesReady_SkipsNoPortAndProbesRest(t *testing.T) {
	var probed []string
	ready := func(_ context.Context, s utils.Service) error {
		probed = append(probed, s.ServiceName)
		return nil
	}
	services := []utils.Service{
		{ServiceName: "api", Port: 3000},
		{ServiceName: "worker", Port: 0}, // no port -> skipped
	}
	if err := waitForServicesReady(context.Background(), services, ready); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(probed) != 1 || probed[0] != "api" {
		t.Errorf("probed %v, want only [api]", probed)
	}
}

// A readiness failure must be returned, naming the offending service.
func TestWaitForServicesReady_PropagatesError(t *testing.T) {
	ready := func(_ context.Context, _ utils.Service) error {
		return errors.New("boom")
	}
	services := []utils.Service{{ServiceName: "api", Port: 3000}}
	err := waitForServicesReady(context.Background(), services, ready)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "api not ready") {
		t.Errorf("error %q, want it to name the service", got)
	}
}

// With no listening ports and no databases, the composed gate returns
// immediately with no error.
func TestWaitDetachedReady_NoPortsReturnsNil(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{{ServiceName: "api", Port: 0}},
	}
	if err := waitDetachedReady(context.Background(), corgi); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// An unreachable service makes the gate fail once the context expires, so
// --wait surfaces a real timeout instead of returning early.
func TestWaitDetachedReady_UnreachableServiceTimesOut(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{{ServiceName: "ghost", Port: 59997}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := waitDetachedReady(ctx, corgi); err == nil {
		t.Fatal("expected a timeout error for an unreachable service")
	}
}
