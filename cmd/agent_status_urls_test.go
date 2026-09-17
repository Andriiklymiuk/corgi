package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentStatusPrintsPublicURLs(t *testing.T) {
	dir := tempAgentHome(t)

	lines := publicURLLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "no public URL yet") {
		t.Fatalf("without public.url = %q", lines)
	}
	if connectorURL() != "" {
		t.Errorf("connectorURL without public.url = %q", connectorURL())
	}

	if err := os.WriteFile(filepath.Join(dir, publicURLName), []byte("https://corgi.example.ngrok.app/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines = publicURLLines()
	if len(lines) != 2 {
		t.Fatalf("with public.url = %q", lines)
	}
	if !strings.HasPrefix(lines[0], "  launcher   https://corgi.example.ngrok.app/app") {
		t.Errorf("launcher line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  connector  https://corgi.example.ngrok.app/mcp   add in Claude: Connect") {
		t.Errorf("connector line = %q", lines[1])
	}
	if got := connectorURL(); got != "https://corgi.example.ngrok.app/mcp" {
		t.Errorf("connectorURL = %q", got)
	}
}
