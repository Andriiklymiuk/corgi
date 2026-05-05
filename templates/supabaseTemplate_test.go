package templates

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSupabasePortsDefaults(t *testing.T) {
	got := ReadSupabasePorts(filepath.Join(t.TempDir(), "missing"))
	if got.API != 54321 || got.DB != 54322 || got.Studio != 54323 || got.Inbucket != 54324 {
		t.Errorf("defaults wrong: %+v", got)
	}
}

func TestReadSupabasePortsParsesToml(t *testing.T) {
	dir := t.TempDir()
	tomlDir := filepath.Join(dir, "supabase")
	if err := os.MkdirAll(tomlDir, 0755); err != nil {
		t.Fatal(err)
	}
	body := `
[api]
port = 60001

[db]
port = 60002

[studio]
port = 60003

[inbucket]
port = 60004
`
	if err := os.WriteFile(filepath.Join(tomlDir, "config.toml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	got := ReadSupabasePorts(dir)
	if got.API != 60001 || got.DB != 60002 || got.Studio != 60003 || got.Inbucket != 60004 {
		t.Errorf("parse wrong: %+v", got)
	}
}

func TestReadSupabasePortsDirectTomlPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("[api]\nport = 9999\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadSupabasePorts(p)
	if got.API != 9999 {
		t.Errorf("API = %d want 9999", got.API)
	}
	if got.DB != 54322 {
		t.Errorf("DB should fall back to default, got %d", got.DB)
	}
}

func TestReadSupabasePortsZeroIgnored(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte("[api]\nport = 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := ReadSupabasePorts(p)
	if got.API != 54321 {
		t.Errorf("port=0 must be ignored, got %d", got.API)
	}
}

func TestSignSupabaseJWTStructure(t *testing.T) {
	tok := SignSupabaseJWT("secret", "anon")
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 segments, got %d: %s", len(parts), tok)
	}

	hdr, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if !strings.Contains(string(hdr), "HS256") {
		t.Errorf("header missing HS256: %s", hdr)
	}
	pl, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !strings.Contains(string(pl), `"role":"anon"`) {
		t.Errorf("payload missing role: %s", pl)
	}

	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if parts[2] != wantSig {
		t.Errorf("signature mismatch")
	}
}

func TestSignSupabaseJWTDifferentRoles(t *testing.T) {
	a := SignSupabaseJWT("k", "anon")
	b := SignSupabaseJWT("k", "service_role")
	if a == b {
		t.Error("anon and service_role should differ")
	}
}

func TestSignSupabaseJWTSecretMatters(t *testing.T) {
	a := SignSupabaseJWT("k1", "anon")
	b := SignSupabaseJWT("k2", "anon")
	if a == b {
		t.Error("different secrets must produce different sigs")
	}
}
