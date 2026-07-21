package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// The launcher and the readiness gate must agree; disagreeing is the bug this
// pair of functions exists to prevent.
func TestSkipReadinessWaitAgreesWithLauncher(t *testing.T) {
	prev := utils.ServicesItemsFromFlag
	t.Cleanup(func() { utils.ServicesItemsFromFlag = prev })

	cases := []struct {
		name     string
		selected []string
		service  utils.Service
	}{
		{"plain service", nil, utils.Service{ServiceName: "api"}},
		{"manual, nothing selected", nil, utils.Service{ServiceName: "m", ManualRun: true}},
		{"manual, selected", []string{"m"}, utils.Service{ServiceName: "m", ManualRun: true}},
		{"manual, other selected", []string{"api"}, utils.Service{ServiceName: "m", ManualRun: true}},
		{"plain, other selected", []string{"api"}, utils.Service{ServiceName: "web"}},
	}
	for _, c := range cases {
		utils.ServicesItemsFromFlag = c.selected
		if got, want := skipReadinessWait(c.service), shouldSkipManualRun(c.service); got != want {
			t.Errorf("%s: wait skips=%v but launcher skips=%v", c.name, got, want)
		}
	}
}

// The reason a boot failed is in the logs; a failed run should not make the
// reader go looking for them.
func TestPrintFailureLogsShowsEachServiceTail(t *testing.T) {
	root := t.TempDir()
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = root
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	for _, svc := range []string{"api", "web"} {
		dir := filepath.Join(root, "corgi_services", ".logs", svc)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "noise\n" + svc + " exploded\n"
		if err := os.WriteFile(filepath.Join(dir, "2026-01-01T00-00-00.log"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, printFailureLogs)
	for _, want := range []string{"api", "api exploded", "web", "web exploded"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the failure output, got %q", want, out)
		}
	}
}

func TestTailLinesKeepsOnlyTheEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.log")
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "line-%d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tailLines(path, 10)
	if len(got) != 10 || got[0] != "line-190" || got[9] != "line-199" {
		t.Errorf("expected the last 10 lines, got %v", got)
	}
	if tailLines(filepath.Join(dir, "missing.log"), 10) != nil {
		t.Error("a missing file yields nothing, not a panic")
	}
}
