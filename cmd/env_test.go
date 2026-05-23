package cmd

import "testing"

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
