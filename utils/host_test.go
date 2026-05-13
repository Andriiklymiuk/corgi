package utils

import (
	"net"
	"strings"
	"testing"
)

func TestServiceHost_DefaultLocalhost(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = ""
	if got := ServiceHost(); got != "localhost" {
		t.Fatalf("expected localhost, got %q", got)
	}
}

func TestServiceHost_OverrideUsed(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = "192.168.1.42"
	if got := ServiceHost(); got != "192.168.1.42" {
		t.Fatalf("expected override IP, got %q", got)
	}
}

func TestDetectHostIP_ReturnsValidIPv4(t *testing.T) {
	ip, err := DetectHostIP()
	if err != nil {
		t.Skipf("no LAN interface available in test env: %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("DetectHostIP returned non-parseable %q", ip)
	}
	if parsed.To4() == nil {
		t.Fatalf("expected IPv4, got %q", ip)
	}
	if parsed.IsLoopback() {
		t.Fatalf("returned loopback IP %q", ip)
	}
}

func TestIsVirtualIface(t *testing.T) {
	cases := map[string]bool{
		"en0":          false,
		"eth0":         false,
		"wlan0":        false,
		"utun0":        true,
		"bridge100":    true,
		"vmnet1":       true,
		"docker0":      true,
		"awdl0":        true,
		"llw0":         true,
		"lo0":          true,
		"tun0":         true,
	}
	for name, want := range cases {
		if got := isVirtualIface(name); got != want {
			t.Errorf("isVirtualIface(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestAppendDependentServiceEnv_HostOverrideAppliesToPortFromEnv(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = "10.0.0.5"

	producer := Service{
		ServiceName: "api",
		Environment: []string{"PORT=4000"},
	}
	consumer := Service{
		ServiceName:       "client",
		DependsOnServices: []DependsOnService{{Name: "api", EnvAlias: "API_URL"}},
	}
	corgi := CorgiCompose{Services: []Service{producer, consumer}}

	got := handleDependentServices(consumer, corgi)
	if !strings.Contains(got, "API_URL=http://10.0.0.5:4000") {
		t.Fatalf("expected PORT-from-env path to use override, got %q", got)
	}
}

func TestRenderEnvFileContent_LocalhostNameInEnvWinsOverHostOverride(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = "10.0.0.5"

	service := Service{
		ServiceName:        "client",
		LocalhostNameInEnv: "host.docker.internal",
		Port:               3000,
		DependsOnServices:  []DependsOnService{{Name: "api", EnvAlias: "API_URL"}},
	}
	// Simulate env body that already had HostOverride applied for service URLs
	// but db host stays "localhost".
	envBody := "API_URL=http://10.0.0.5:4000\nDB_HOST=localhost\n"

	out := renderEnvFileContent("/nonexistent/path/to/.env", envBody, service)

	// LocalhostNameInEnv ReplaceAll touches "localhost" only — HostOverride
	// value escapes since it's not the literal "localhost".
	if !strings.Contains(out, "API_URL=http://10.0.0.5:4000") {
		t.Fatalf("expected HostOverride preserved, got %q", out)
	}
	if !strings.Contains(out, "DB_HOST=host.docker.internal") {
		t.Fatalf("expected LocalhostNameInEnv applied to db host, got %q", out)
	}
}

func TestAppendDependentServiceEnv_UsesHostOverride(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = "10.0.0.5"

	producer := Service{ServiceName: "api", Port: 3000}
	consumer := Service{
		ServiceName:       "client",
		DependsOnServices: []DependsOnService{{Name: "api", EnvAlias: "API_URL"}},
	}
	corgi := CorgiCompose{Services: []Service{producer, consumer}}

	got := handleDependentServices(consumer, corgi)
	want := "API_URL=http://10.0.0.5:3000"
	if !strings.Contains(got, want) {
		t.Fatalf("expected %q in output, got %q", want, got)
	}
	if strings.Contains(got, "localhost") {
		t.Fatalf("expected no localhost in output, got %q", got)
	}
}
