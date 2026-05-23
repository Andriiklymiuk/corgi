package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		key, val, want string
	}{
		{"DB_PASSWORD", "supersecret", "su****et"},
		{"API_TOKEN", "abc", "***"},
		{"LOG_LEVEL", "debug", "debug"}, // not a secret
		{"DATABASE_URL", "postgres://u:pw@h:5432/d", "postgres://u:****@h:5432/d"},
	}
	for _, c := range cases {
		if got := maskSecret(c.key, c.val); got != c.want {
			t.Errorf("maskSecret(%q,%q)=%q want %q", c.key, c.val, got, c.want)
		}
	}
}

func TestRenderPlain(t *testing.T) {
	all := map[string][]utils.EnvVar{
		"api": {
			{Key: "API_PORT", Value: "8080", Source: "self:port"},
			{Key: "DB_PASSWORD", Value: "supersecret", Source: "db:pg"},
		},
	}
	out := renderPlain(all, []string{"api"}, false)
	if !strings.Contains(out, "# api") {
		t.Errorf("missing service header:\n%s", out)
	}
	if !strings.Contains(out, "API_PORT=8080") || !strings.Contains(out, "# self:port") {
		t.Errorf("missing port line/source:\n%s", out)
	}
	if strings.Contains(out, "supersecret") {
		t.Errorf("secret leaked in plain view:\n%s", out)
	}
	// --reveal=true unmasks
	if !strings.Contains(renderPlain(all, []string{"api"}, true), "supersecret") {
		t.Errorf("reveal did not unmask")
	}
}
