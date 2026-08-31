package utils

import (
	"path/filepath"
	"testing"
)

func leaseWorkspace(t *testing.T) {
	t.Helper()
	previous := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() {
		CorgiComposePathDir = previous
		containerScope = ""
	})
}

func leaseCompose() *CorgiCompose {
	return &CorgiCompose{
		Name: "acme",
		Services: []Service{
			{ServiceName: "api", Port: 4000},
			{ServiceName: "worker"},
		},
		DatabaseServices: []DatabaseService{
			{ServiceName: "pg", Driver: "postgres", Port: 5432, Port2: 5433, DatabaseName: "acme"},
		},
	}
}

func TestApplyIsolationLeaseShiftsPortsAndNames(t *testing.T) {
	leaseWorkspace(t)
	corgi := leaseCompose()

	if err := ApplyIsolationLease(corgi, "agent-a"); err != nil {
		t.Fatal(err)
	}
	if corgi.Services[0].Port != 4100 {
		t.Errorf("api port = %d, want 4100", corgi.Services[0].Port)
	}
	if corgi.Services[1].Port != 0 {
		t.Errorf("a service with no port must stay portless, got %d", corgi.Services[1].Port)
	}
	if corgi.DatabaseServices[0].Port != 5532 || corgi.DatabaseServices[0].Port2 != 5533 {
		t.Errorf("db ports = %d/%d, want 5532/5533", corgi.DatabaseServices[0].Port, corgi.DatabaseServices[0].Port2)
	}
	if corgi.DatabaseServices[0].DatabaseName != "acme_agent-a" {
		t.Errorf("database name = %q, want acme_agent-a", corgi.DatabaseServices[0].DatabaseName)
	}
	if ContainerScope() == "" {
		t.Error("a lease must scope container names")
	}
}

func TestApplyIsolationLeaseIsStableAcrossCalls(t *testing.T) {
	leaseWorkspace(t)

	first := leaseCompose()
	if err := ApplyIsolationLease(first, "agent-a"); err != nil {
		t.Fatal(err)
	}
	again := leaseCompose()
	if err := ApplyIsolationLease(again, "agent-a"); err != nil {
		t.Fatal(err)
	}
	if first.Services[0].Port != again.Services[0].Port {
		t.Errorf("the same lease must reuse its block: %d then %d", first.Services[0].Port, again.Services[0].Port)
	}
}

func TestApplyIsolationLeaseGivesEachLeaseItsOwnBlock(t *testing.T) {
	leaseWorkspace(t)

	a := leaseCompose()
	b := leaseCompose()
	if err := ApplyIsolationLease(a, "agent-a"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyIsolationLease(b, "agent-b"); err != nil {
		t.Fatal(err)
	}
	if a.Services[0].Port == b.Services[0].Port {
		t.Fatalf("two leases collided on port %d", a.Services[0].Port)
	}
	if len(ListLeases()) != 2 {
		t.Errorf("expected 2 leases, got %d", len(ListLeases()))
	}
}

func TestApplyIsolationLeaseNoopWithoutName(t *testing.T) {
	leaseWorkspace(t)
	corgi := leaseCompose()
	if err := ApplyIsolationLease(corgi, ""); err != nil {
		t.Fatal(err)
	}
	if corgi.Services[0].Port != 4000 || ContainerScope() != "" {
		t.Error("no --isolate means nothing changes")
	}
}

func TestApplyIsolationLeaseRejectsBadName(t *testing.T) {
	leaseWorkspace(t)
	if err := ApplyIsolationLease(leaseCompose(), "bad name/../etc"); err == nil {
		t.Error("a lease name with a path separator must be rejected")
	}
}

func TestReleaseLease(t *testing.T) {
	leaseWorkspace(t)
	if err := ApplyIsolationLease(leaseCompose(), "agent-a"); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseLease("agent-a"); err != nil {
		t.Fatal(err)
	}
	if len(ListLeases()) != 0 {
		t.Error("released lease still listed")
	}
	if err := ReleaseLease("agent-a"); err == nil {
		t.Error("releasing an unknown lease must error")
	}
}

func TestLeasesDirLivesUnderCorgiServices(t *testing.T) {
	if got := LeasesDir("/ws"); got != filepath.Join("/ws", "corgi_services", ".leases") {
		t.Errorf("LeasesDir = %q", got)
	}
}
