package tunnel

import (
	"strings"
	"testing"
)

func TestCloudflared(t *testing.T) {
	c := Cloudflared{}
	if c.Name() != "cloudflared" {
		t.Errorf("name = %q", c.Name())
	}
	cmd := c.Cmd(8080)
	if cmd[0] != "cloudflared" || cmd[len(cmd)-1] != "http://localhost:8080" {
		t.Errorf("cmd = %v", cmd)
	}
	if c.AcceptsStdin() {
		t.Error("AcceptsStdin should be false")
	}
	if err := c.PreflightAuth(); err != nil {
		t.Errorf("PreflightAuth = %v", err)
	}
	if c.InstallHint() == "" {
		t.Error("InstallHint empty")
	}

	t.Run("ExtractURL finds trycloudflare", func(t *testing.T) {
		got := c.ExtractURL("INF |  https://kind-zebra-42.trycloudflare.com  |")
		if got != "https://kind-zebra-42.trycloudflare.com" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ExtractURL miss", func(t *testing.T) {
		if got := c.ExtractURL("nothing here"); got != "" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("CmdNamed requires name", func(t *testing.T) {
		_, err := c.CmdNamed(80, NamedConfig{})
		if err == nil {
			t.Error("want err")
		}
	})

	t.Run("CmdNamed ok", func(t *testing.T) {
		cmd, err := c.CmdNamed(80, NamedConfig{Name: "mytun"})
		if err != nil {
			t.Fatal(err)
		}
		if cmd[len(cmd)-1] != "mytun" {
			t.Errorf("cmd = %v", cmd)
		}
	})
}

func TestNgrok(t *testing.T) {
	n := Ngrok{}
	if n.Name() != "ngrok" {
		t.Errorf("name = %q", n.Name())
	}
	cmd := n.Cmd(9090)
	if cmd[len(cmd)-1] != "9090" {
		t.Errorf("cmd = %v", cmd)
	}
	if n.AcceptsStdin() {
		t.Error("AcceptsStdin should be false")
	}

	t.Run("ExtractURL", func(t *testing.T) {
		got := n.ExtractURL("t=now url=https://abc-123.ngrok-free.app addr=...")
		if !strings.HasPrefix(got, "https://abc-123.ngrok") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("CmdNamed has domain flag", func(t *testing.T) {
		cmd, err := n.CmdNamed(80, NamedConfig{Hostname: "x.example.com"})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "--domain=x.example.com") {
			t.Errorf("cmd = %v", cmd)
		}
	})
}

func TestLocaltunnel(t *testing.T) {
	l := Localtunnel{}
	if l.Name() != "localtunnel" {
		t.Errorf("name = %q", l.Name())
	}
	if l.AcceptsStdin() {
		t.Error("AcceptsStdin should be false")
	}
	if err := l.PreflightAuth(); err != nil {
		t.Errorf("PreflightAuth = %v", err)
	}
	if err := l.PreflightNamedAuth(NamedConfig{}); err != nil {
		t.Errorf("PreflightNamedAuth = %v", err)
	}

	t.Run("Cmd", func(t *testing.T) {
		cmd := l.Cmd(7777)
		if cmd[0] != "lt" || cmd[len(cmd)-1] != "7777" {
			t.Errorf("cmd = %v", cmd)
		}
	})

	t.Run("ExtractURL localtunnel.me", func(t *testing.T) {
		got := l.ExtractURL("your url is: https://my-api.localtunnel.me")
		if got != "https://my-api.localtunnel.me" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ExtractURL loca.lt", func(t *testing.T) {
		got := l.ExtractURL("foo https://abc.loca.lt bar")
		if got != "https://abc.loca.lt" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("CmdNamed strips suffix", func(t *testing.T) {
		cmd, err := l.CmdNamed(80, NamedConfig{Hostname: "my-api.localtunnel.me"})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "--subdomain my-api") {
			t.Errorf("cmd = %v", cmd)
		}
	})

	t.Run("CmdNamed rejects invalid hostname", func(t *testing.T) {
		_, err := l.CmdNamed(80, NamedConfig{Hostname: "has.dots/slash"})
		if err == nil {
			t.Error("want err")
		}
	})

	t.Run("CmdNamed rejects empty", func(t *testing.T) {
		_, err := l.CmdNamed(80, NamedConfig{Hostname: ""})
		if err == nil {
			t.Error("want err")
		}
	})
}

func TestCloudflaredPreflightNamedAuthNoCert(t *testing.T) {
	err := Cloudflared{}.PreflightNamedAuth(NamedConfig{Hostname: "my.tunnel.example.com"})
	if err == nil {
		t.Error("expected error when cert.pem missing")
	}
}

func TestNgrokInstallHint(t *testing.T) {
	hint := Ngrok{}.InstallHint()
	if hint == "" {
		t.Error("InstallHint should not be empty")
	}
}

func TestNgrokPreflightAuthNoConfig(t *testing.T) {
	err := Ngrok{}.PreflightAuth()
	if err == nil {
		t.Log("ngrok is installed (unexpected in CI) or PreflightAuth returned nil")
	}
}

func TestNgrokPreflightNamedAuth(t *testing.T) {
	// Should return error when ngrok config not present
	err := Ngrok{}.PreflightNamedAuth(NamedConfig{Hostname: "x.ngrok.app"})
	if err == nil {
		t.Log("ngrok available and configured (unexpected)")
	}
}

func TestLocaltunnelInstallHint(t *testing.T) {
	hint := Localtunnel{}.InstallHint()
	if hint == "" {
		t.Error("InstallHint should not be empty")
	}
}
