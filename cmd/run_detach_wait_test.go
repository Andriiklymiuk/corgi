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

func TestWaitForServicesReadySkipsManualRun(t *testing.T) {
	prev := utils.ServicesItemsFromFlag
	utils.ServicesItemsFromFlag = nil
	t.Cleanup(func() { utils.ServicesItemsFromFlag = prev })

	var probed []string
	ready := func(_ context.Context, svc utils.Service) error {
		probed = append(probed, svc.ServiceName)
		return nil
	}

	err := waitForServicesReady(context.Background(), []utils.Service{
		{ServiceName: "api", Port: 3000},
		{ServiceName: "manual", Port: 3001, ManualRun: true},
		{ServiceName: "noport"},
	}, ready)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(probed) != 1 || probed[0] != "api" {
		t.Errorf("a manualRun service is never started, so it must not be waited for; probed %v", probed)
	}
}

func TestWaitForServicesReadyWaitsForExplicitlySelectedManualRun(t *testing.T) {
	prev := utils.ServicesItemsFromFlag
	utils.ServicesItemsFromFlag = []string{"manual"}
	t.Cleanup(func() { utils.ServicesItemsFromFlag = prev })

	var probed []string
	ready := func(_ context.Context, svc utils.Service) error {
		probed = append(probed, svc.ServiceName)
		return nil
	}

	if err := waitForServicesReady(context.Background(), []utils.Service{
		{ServiceName: "manual", Port: 3001, ManualRun: true},
	}, ready); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(probed) != 1 {
		t.Errorf("--services asked for it, so it does start and must be waited for; probed %v", probed)
	}
}

func TestWaitForDbsReadySkipsManualRun(t *testing.T) {
	var probed []string
	ready := func(_ context.Context, db utils.DatabaseService) error {
		probed = append(probed, db.ServiceName)
		return nil
	}

	if err := waitForDbsReady(context.Background(), []utils.DatabaseService{
		{ServiceName: "pg", Port: 5432},
		{ServiceName: "manual", Port: 5433, ManualRun: true},
	}, ready); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(probed) != 1 || probed[0] != "pg" {
		t.Errorf("probed %v", probed)
	}
}
