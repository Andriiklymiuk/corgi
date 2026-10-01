package templates

import (
	"strings"
	"testing"
	"text/template"
)

type valkeyTemplateData struct {
	ServiceName string
	Version     string
	Port        int
	Password    string
}

func renderValkey(t *testing.T, tpl string, data valkeyTemplateData) string {
	t.Helper()
	var out strings.Builder
	if err := template.Must(template.New("valkey").Parse(tpl)).Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestDockerComposeValkeyMapsTheValkeyPort(t *testing.T) {
	got := renderValkey(t, DockerComposeValkey, valkeyTemplateData{ServiceName: "cache", Port: 6390})
	for _, want := range []string{
		`- "6390:6379"`,
		"image: valkey/valkey:alpine",
		"container_name: valkey-cache",
		"command: valkey-server\n",
		"- corgi-network",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compose missing %q:\n%s", want, got)
		}
	}
}

func TestDockerComposeValkeyVersionAndPassword(t *testing.T) {
	got := renderValkey(t, DockerComposeValkey, valkeyTemplateData{ServiceName: "cache", Version: "8.1-alpine", Port: 6379, Password: "pw"})
	for _, want := range []string{"image: valkey/valkey:8.1-alpine", "command: valkey-server --requirepass pw"} {
		if !strings.Contains(got, want) {
			t.Errorf("compose missing %q:\n%s", want, got)
		}
	}
}

func TestMakefileValkeyCliPassword(t *testing.T) {
	noPassword := renderValkey(t, MakefileValkey, valkeyTemplateData{ServiceName: "cache"})
	if !strings.Contains(noPassword, "valkey-cli\n") || strings.Contains(noPassword, " -a ") {
		t.Errorf("cli without password should not pass -a:\n%s", noPassword)
	}
	withPassword := renderValkey(t, MakefileValkey, valkeyTemplateData{ServiceName: "cache", Password: "pw"})
	if !strings.Contains(withPassword, "valkey-cli -a pw\n") {
		t.Errorf("cli with password should pass -a pw:\n%s", withPassword)
	}
}
