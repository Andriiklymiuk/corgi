package cmd

import (
	"strings"
	"testing"
	"text/template"

	"andriiklymiuk/corgi/templates"
	"andriiklymiuk/corgi/utils"
)

func renderDockerCompose(t *testing.T, d utils.DockerServiceTemplateData) string {
	t.Helper()
	tmp := template.Must(template.New("t").
		Funcs(template.FuncMap{"yamlQuote": func(s string) string { return `"` + s + `"` }}).
		Parse(templates.DockerComposeService))
	var b strings.Builder
	if err := tmp.Execute(&b, d); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestComposeTemplateImageMode(t *testing.T) {
	out := renderDockerCompose(t, utils.DockerServiceTemplateData{
		DockerName:    "pdf",
		Image:         "gotenberg/gotenberg:8",
		Port:          3005,
		ContainerPort: 3000,
	})
	if !strings.Contains(out, `image: "gotenberg/gotenberg:8"`) {
		t.Fatalf("missing image line:\n%s", out)
	}
	if strings.Contains(out, "build:") || strings.Contains(out, "develop:") {
		t.Fatalf("image mode must not emit build/develop blocks:\n%s", out)
	}
	if !strings.Contains(out, `"3005:3000"`) {
		t.Fatalf("missing port mapping:\n%s", out)
	}
}

func TestComposeTemplateWatchMode(t *testing.T) {
	out := renderDockerCompose(t, utils.DockerServiceTemplateData{
		DockerName:     "api",
		BuildContext:   "/ctx",
		DockerfilePath: "Dockerfile",
		Port:           3000,
		ContainerPort:  3000,
		Watch:          true,
	})
	if !strings.Contains(out, "develop:") || !strings.Contains(out, "action: rebuild") {
		t.Fatalf("watch must emit develop.watch:\n%s", out)
	}
	if !strings.Contains(templates.MakefileService, "upw:") {
		t.Fatal("Makefile must carry the upw target")
	}
}

func TestBuildableFilterSkipsImageAndNonDocker(t *testing.T) {
	services := []utils.Service{
		{ServiceName: "img", Port: 1, Runner: utils.Runner{Name: "docker", Image: "nginx:alpine"},
			ResolvedDockerSource: utils.SourceImage},
		{ServiceName: "native", Start: []string{"go run ."}},
		{ServiceName: "df", Port: 2, Runner: utils.Runner{Name: "docker"},
			ResolvedDockerSource: utils.SourceDockerfile},
	}
	var buildable []string
	for _, s := range services {
		if s.Runner.IsDocker() && s.ResolvedDockerSource != utils.SourceNone && s.Runner.Image == "" {
			buildable = append(buildable, s.ServiceName)
		}
	}
	if len(buildable) != 1 || buildable[0] != "df" {
		t.Fatalf("want [df], got %v", buildable)
	}
}

func TestShouldCreateServiceImageWithoutPortSkips(t *testing.T) {
	s := utils.Service{
		ServiceName:          "pdf",
		Runner:               utils.Runner{Name: "docker", Image: "nginx:alpine"},
		AbsolutePath:         t.TempDir(),
		ResolvedDockerSource: utils.SourceImage,
	}
	if shouldCreateService(&s) {
		t.Fatal("image without port must be skipped")
	}
	s.Port = 8080
	if !shouldCreateService(&s) {
		t.Fatal("image with port must create")
	}
}
