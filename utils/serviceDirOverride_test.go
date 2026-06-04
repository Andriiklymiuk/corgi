package utils

import "testing"

func TestOverrideServiceDirs(t *testing.T) {
	wt := t.TempDir()
	mk := func() *CorgiCompose {
		return &CorgiCompose{Services: []Service{
			{ServiceName: "api", AbsolutePath: "/orig/api"},
			{ServiceName: "web", AbsolutePath: "/orig/web"},
		}}
	}

	t.Run("empty is a no-op", func(t *testing.T) {
		c := mk()
		if err := overrideServiceDirs(c, nil); err != nil {
			t.Fatal(err)
		}
		if c.Services[0].AbsolutePath != "/orig/api" {
			t.Error("AbsolutePath changed on empty input")
		}
	})

	t.Run("overrides only the named service", func(t *testing.T) {
		c := mk()
		if err := overrideServiceDirs(c, []string{"api=" + wt}); err != nil {
			t.Fatal(err)
		}
		if c.Services[0].AbsolutePath != wt {
			t.Errorf("api AbsolutePath = %q, want %q", c.Services[0].AbsolutePath, wt)
		}
		if c.Services[1].AbsolutePath != "/orig/web" {
			t.Errorf("web AbsolutePath changed: %q", c.Services[1].AbsolutePath)
		}
	})

	t.Run("unknown service errors", func(t *testing.T) {
		if err := overrideServiceDirs(mk(), []string{"nope=" + wt}); err == nil {
			t.Fatal("expected error for unknown service name")
		}
	})

	t.Run("missing directory errors", func(t *testing.T) {
		if err := overrideServiceDirs(mk(), []string{"api=/no/such/dir/zzz"}); err == nil {
			t.Fatal("expected error for missing directory")
		}
	})

	t.Run("malformed pair errors", func(t *testing.T) {
		for _, bad := range []string{"api", "=/tmp", "api="} {
			if err := overrideServiceDirs(mk(), []string{bad}); err == nil {
				t.Fatalf("expected error for malformed pair %q", bad)
			}
		}
	})
}
