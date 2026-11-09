package utils

import "testing"

func TestNearest(t *testing.T) {
	fields := []string{"healthCheck", "depends_on_db", "depends_on_services", "port", "port2", "start"}
	cases := map[string]string{
		"helthCheck":   "healthCheck",
		"healthcheck":  "healthCheck",
		"dependsOnDb":  "depends_on_db",
		"depends_on":   "depends_on_db",
		"strat":        "start",
		"prot":         "port",
		"zzzzzz":       "",
		"":             "",
		"--detatch":    "",
		"completely_x": "",
	}
	for in, want := range cases {
		if got := Nearest(in, fields); got != want {
			t.Errorf("Nearest(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Nearest("--detatch", []string{"--detach", "--docker"}); got != "--detach" {
		t.Errorf("flags: got %q", got)
	}
	if got := Nearest("postgress", KnownDrivers); got != "postgres" {
		t.Errorf("drivers: got %q", got)
	}
}
