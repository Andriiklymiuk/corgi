package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
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

// I1: empty-username connection strings must still mask the password.
func TestMaskSecretEmptyUsernameURL(t *testing.T) {
	got := maskSecret("REDIS_URL", "redis://:pass@host:6379")
	if !strings.Contains(got, "****") || strings.Contains(got, "pass") {
		t.Errorf("password leaked: maskSecret=%q", got)
	}
}

// I2: multibyte secret values must not be corrupted into invalid UTF-8.
func TestMaskSecretMultibyte(t *testing.T) {
	got := maskSecret("PASSWORD", "héllo")
	if !utf8.ValidString(got) {
		t.Errorf("masked value is not valid UTF-8: %q", got)
	}
}

// M3: a secret-named key holding a URL must be fully masked, not just its
// password segment.
func TestMaskSecretURLKeyTakesPrecedence(t *testing.T) {
	got := maskSecret("DB_PASSWORD", "postgres://u:p@h")
	if strings.Contains(got, "postgres://") {
		t.Errorf("secret-named key not fully masked: %q", got)
	}
}

func TestSelectEnvServices(t *testing.T) {
	all := map[string][]utils.EnvVar{
		"web": {{Key: "PORT", Value: "80"}},
		"api": {{Key: "PORT", Value: "81"}},
	}
	order, err := selectEnvServices(nil, nil, all)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "api" || order[1] != "web" {
		t.Errorf("want sorted [api web], got %v", order)
	}
	_, err = selectEnvServices(nil, []string{"nope"}, all)
	if err == nil || !strings.Contains(err.Error(), utils.ErrServiceNotFound) {
		t.Errorf("want %s error, got %v", utils.ErrServiceNotFound, err)
	}
}

func TestRunEnvMutuallyExclusiveFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("export", true, "")
	cmd.Flags().Bool("json", true, "")
	cmd.Flags().Bool("reveal", false, "")
	err := runEnv(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), utils.ErrUsage) {
		t.Errorf("want %s error, got %v", utils.ErrUsage, err)
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

func TestRenderExport(t *testing.T) {
	all := map[string][]utils.EnvVar{
		"api": {
			{Key: "MSG", Value: "it's a test", Source: "literal"},
			{Key: "API_PORT", Value: "8080", Source: "self:port"},
		},
	}
	out := renderExport(all, []string{"api"})
	if !strings.Contains(out, `export API_PORT='8080'`) {
		t.Errorf("missing export line:\n%s", out)
	}
	// single-quote escaping: ' -> '\''
	if !strings.Contains(out, `export MSG='it'\''s a test'`) {
		t.Errorf("bad shell escaping:\n%s", out)
	}
}

func TestRenderJSON(t *testing.T) {
	all := map[string][]utils.EnvVar{
		"api": {{Key: "DB_PASSWORD", Value: "supersecret", Source: "db:pg"}},
	}
	out, err := renderJSON(all, []string{"api"})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]struct {
		Value, Source string
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	e := parsed["api"]["DB_PASSWORD"]
	if e.Value != "supersecret" || e.Source != "db:pg" {
		t.Fatalf("json wrong: %+v", e)
	}
}
