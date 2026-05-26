package cmd

import (
	"net"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestPortPreflight_NoServicePortsIsNil(t *testing.T) {
	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "x"}}}
	if err := portPreflight(corgi, false); err != nil {
		t.Fatalf("no ports should be nil, got %v", err)
	}
}

func TestPortPreflight_BusyServicePortAborts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api", Port: port}}}
	err = portPreflight(corgi, false)
	if err == nil || !strings.Contains(err.Error(), "kill-port") {
		t.Fatalf("want abort with --kill-port hint, got %v", err)
	}
}

func TestCheckPortConflicts_ReportsBusyWithOwner(t *testing.T) {
	ports := []portOwnerInfo{
		{Port: 3000, Desc: "services.api"},
		{Port: 5432, Desc: "db_services.pg"},
	}
	isBusy := func(p int) bool { return p == 3000 }
	owner := func(p int) string { return "PID 123 (puma)" }

	got := checkPortConflicts(ports, isBusy, owner)
	if len(got) != 1 {
		t.Fatalf("want 1 conflict, got %v", got)
	}
	if !strings.Contains(got[0], "3000") || !strings.Contains(got[0], "puma") || !strings.Contains(got[0], "services.api") {
		t.Fatalf("conflict should name port, owner, desc: %q", got[0])
	}
}

func TestCheckPortConflicts_NoneBusy(t *testing.T) {
	ports := []portOwnerInfo{{Port: 3000, Desc: "services.api"}}
	got := checkPortConflicts(ports, func(int) bool { return false }, func(int) string { return "" })
	if len(got) != 0 {
		t.Fatalf("want no conflicts, got %v", got)
	}
}
