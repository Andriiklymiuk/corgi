package utils

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenOnReady_ParseBool(t *testing.T) {
	var s struct {
		OpenOnReady *OpenOnReady `yaml:"openOnReady"`
	}
	if err := yaml.Unmarshal([]byte("openOnReady: true\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.OpenOnReady == nil || !s.OpenOnReady.Enabled {
		t.Fatalf("bool true should enable: %+v", s.OpenOnReady)
	}
}

func TestOpenOnReady_ParseObject(t *testing.T) {
	var s struct {
		OpenOnReady *OpenOnReady `yaml:"openOnReady"`
	}
	data := "openOnReady:\n  path: /auth/signin/en\n  scheme: https\n  browser: Google Chrome\n"
	if err := yaml.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	o := s.OpenOnReady
	if o == nil || !o.Enabled || o.Path != "/auth/signin/en" || o.Scheme != "https" || o.Browser != "Google Chrome" {
		t.Fatalf("object parse wrong: %+v", o)
	}
}

func TestOpenOnReady_URL(t *testing.T) {
	cases := []struct {
		o    OpenOnReady
		port int
		want string
	}{
		{OpenOnReady{Enabled: true}, 3000, "http://localhost:3000/"},
		{OpenOnReady{Enabled: true, Scheme: "https", Path: "/login"}, 3000, "https://localhost:3000/login"},
		{OpenOnReady{Enabled: true, Path: "auth"}, 5173, "http://localhost:5173/auth"},
	}
	for _, c := range cases {
		if got := c.o.URL(c.port); got != c.want {
			t.Fatalf("URL(%d) = %q, want %q", c.port, got, c.want)
		}
	}
}
