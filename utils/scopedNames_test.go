package utils

import "testing"

func withScope(t *testing.T, c *CorgiCompose) {
	t.Helper()
	SetContainerScope(c)
	t.Cleanup(func() { SetContainerScope(nil) })
}

func TestScopeOffKeepsLegacyNames(t *testing.T) {
	withScope(t, &CorgiCompose{Name: "My Stack"})
	if ContainerName("postgres", "db") != "postgres-db" {
		t.Fatal("scope off must keep legacy db names")
	}
	if (Service{ServiceName: "MyApi"}).DockerName() != "myapi" {
		t.Fatal("scope off must keep legacy service names")
	}
}

func TestScopeOnPrefixesNames(t *testing.T) {
	withScope(t, &CorgiCompose{Name: "My Stack", ScopeContainers: true})
	if got := ContainerName("postgres", "db"); got != "postgres-my-stack-db" {
		t.Fatalf("got %q", got)
	}
	if got := (Service{ServiceName: "MyApi"}).DockerName(); got != "my-stack-myapi" {
		t.Fatalf("got %q", got)
	}
	if got := ServiceContainerName("api"); got != "my-stack-api" {
		t.Fatalf("got %q", got)
	}
}

func TestScopeFallsBackToComposeDirName(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = "/tmp/Cool Project"
	t.Cleanup(func() { CorgiComposePathDir = prev })
	withScope(t, &CorgiCompose{ScopeContainers: true})
	if got := ScopedContainerBase("api"); got != "cool-project-api" {
		t.Fatalf("got %q", got)
	}
}
