package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/templates"
)

func TestSupabaseDriver_RegisteredAndPrefixed(t *testing.T) {
	cfg, ok := DriverConfigs["supabase"]
	if !ok {
		t.Fatal("supabase driver not registered in DriverConfigs")
	}
	if cfg.Prefix != "SUPABASE_" {
		t.Fatalf("expected prefix SUPABASE_, got %q", cfg.Prefix)
	}
}

func TestSupabaseDriver_EmitsCoreVars(t *testing.T) {
	db := DatabaseService{
		ServiceName: "supabase",
		Driver:      "supabase",
		Host:        "localhost",
		Port:        54321,
	}
	got := DriverConfigs["supabase"].EnvGenerator("SUPABASE_", db)

	wantContains := []string{
		"SUPABASE_URL=http://localhost:54321",
		"SUPABASE_ANON_KEY=eyJ",
		"SUPABASE_SERVICE_ROLE_KEY=eyJ",
		"SUPABASE_JWT_SECRET=super-secret-jwt-token",
		"SUPABASE_DB_URL=postgresql://postgres:postgres@localhost:54322/postgres",
		"SUPABASE_DB_HOST=localhost",
		"SUPABASE_DB_PORT=54322",
		"SUPABASE_STUDIO_URL=http://localhost:54323",
		"SUPABASE_INBUCKET_URL=http://localhost:54324",
		"SUPABASE_STORAGE_S3_URL=http://localhost:54321/storage/v1/s3",
		"SUPABASE_S3_PROTOCOL_ACCESS_KEY_ID=",
		"SUPABASE_S3_PROTOCOL_ACCESS_KEY_SECRET=",
		"SUPABASE_S3_PROTOCOL_REGION=local",
	}
	for _, w := range wantContains {
		if !strings.Contains(got, w) {
			t.Errorf("env missing %q\n--- got ---\n%s", w, got)
		}
	}
}

func TestSupabaseDriver_EmitsBucketEnv(t *testing.T) {
	db := DatabaseService{
		ServiceName: "supabase",
		Driver:      "supabase",
		Host:        "localhost",
		Port:        54321,
		Buckets:     []string{"clients-documents", "public-assets"},
	}
	got := DriverConfigs["supabase"].EnvGenerator("SUPABASE_", db)

	if !strings.Contains(got, "SUPABASE_BUCKET_CLIENTS_DOCUMENTS=clients-documents") {
		t.Errorf("missing CLIENTS_DOCUMENTS bucket emission. got:\n%s", got)
	}
	if !strings.Contains(got, "SUPABASE_BUCKET_PUBLIC_ASSETS=public-assets") {
		t.Errorf("missing PUBLIC_ASSETS bucket emission. got:\n%s", got)
	}
}

func TestSupabaseDriver_DefaultPortFallback(t *testing.T) {
	db := DatabaseService{
		ServiceName: "supabase",
		Driver:      "supabase",
		Host:        "",
		Port:        0,
	}
	got := DriverConfigs["supabase"].EnvGenerator("SUPABASE_", db)

	if !strings.Contains(got, "SUPABASE_URL=http://localhost:54321") {
		t.Errorf("expected default URL fallback, got:\n%s", got)
	}
}

func TestSupabaseDriver_CustomJWTSignsKeys(t *testing.T) {
	custom := "another-32-character-jwt-secret-for-testing-x"
	db := DatabaseService{
		ServiceName: "supabase",
		Driver:      "supabase",
		Host:        "localhost",
		Port:        54321,
		JWTSecret:   custom,
	}
	got := DriverConfigs["supabase"].EnvGenerator("SUPABASE_", db)

	if !strings.Contains(got, "SUPABASE_JWT_SECRET="+custom) {
		t.Errorf("custom JWT_SECRET not propagated. got:\n%s", got)
	}
	stockAnon := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6ImFub24iLCJleHAiOjE5ODM4MTI5OTZ9.CRXP1A7WOeoJeXxjNni43kdQwgnWNReilDMblYTn_I0"
	if strings.Contains(got, "SUPABASE_ANON_KEY="+stockAnon) {
		t.Errorf("stock ANON_KEY leaked when custom secret set. got:\n%s", got)
	}
	for _, prefix := range []string{"SUPABASE_ANON_KEY=", "SUPABASE_SERVICE_ROLE_KEY="} {
		idx := strings.Index(got, prefix)
		if idx < 0 {
			t.Fatalf("missing %q in env. got:\n%s", prefix, got)
		}
		end := strings.Index(got[idx:], "\n")
		if end < 0 {
			end = len(got) - idx
		}
		val := got[idx+len(prefix) : idx+end]
		if strings.Count(val, ".") != 2 {
			t.Errorf("%s not a 3-part JWT: %q", prefix, val)
		}
	}
}

func TestSupabaseDriver_StockSecretMatchesPublishedAnonKey(t *testing.T) {
	signed := templates.SignSupabaseJWT(templates.SupabaseJWTSecret, "anon")
	want := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6ImFub24iLCJleHAiOjE5ODM4MTI5OTZ9.CRXP1A7WOeoJeXxjNni43kdQwgnWNReilDMblYTn_I0"
	if signed != want {
		t.Errorf("anon JWT drift.\n got: %s\nwant: %s", signed, want)
	}
}

func TestSupabaseDriver_StockSecretMatchesPublishedServiceRoleKey(t *testing.T) {
	signed := templates.SignSupabaseJWT(templates.SupabaseJWTSecret, "service_role")
	want := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU"
	if signed != want {
		t.Errorf("service_role JWT drift.\n got: %s\nwant: %s", signed, want)
	}
}

func TestReadSupabasePorts_Defaults(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	got := templates.ReadSupabasePorts("")
	if got.API != 54321 || got.DB != 54322 || got.Studio != 54323 || got.Inbucket != 54324 {
		t.Errorf("expected stock defaults, got %+v", got)
	}
}

func TestReadSupabasePorts_HonorsConfigToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "supabase"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `[api]
port = 8000

[db]
port = 8001

[studio]
port = 8002

[inbucket]
port = 8003
`
	if err := os.WriteFile(filepath.Join(dir, "supabase", "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	got := templates.ReadSupabasePorts("")
	if got.API != 8000 || got.DB != 8001 || got.Studio != 8002 || got.Inbucket != 8003 {
		t.Errorf("expected ports parsed from config.toml, got %+v", got)
	}
}

func TestReadSupabasePorts_PartialConfigFallsBackPerField(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "supabase"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `[api]
port = 9000
`
	if err := os.WriteFile(filepath.Join(dir, "supabase", "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)

	got := templates.ReadSupabasePorts("")
	if got.API != 9000 {
		t.Errorf("expected API=9000, got %d", got.API)
	}
	if got.DB != 54322 || got.Studio != 54323 || got.Inbucket != 54324 {
		t.Errorf("missing sections should fall back to defaults, got %+v", got)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
