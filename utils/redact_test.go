package utils

import (
	"strings"
	"testing"
)

func TestRedactLineMasksCredentials(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		leaked  string
		wantSub string
	}{
		{
			name:    "assignment with secret-named key",
			in:      "POSTGRES_PASSWORD=sup3rs3cretvalue",
			leaked:  "sup3rs3cretvalue",
			wantSub: "POSTGRES_PASSWORD=****",
		},
		{
			name:    "colon form",
			in:      "  api_key: abcd1234efgh5678",
			leaked:  "abcd1234efgh5678",
			wantSub: "api_key: ****",
		},
		{
			name:    "service role key",
			in:      "SERVICE_ROLE_KEY=zzzzzzzzzzzz",
			leaked:  "zzzzzzzzzzzz",
			wantSub: "****",
		},
		{
			name:    "connection string password",
			in:      "connecting to postgres://corgi:hunter2pass@localhost:5432/api",
			leaked:  "hunter2pass",
			wantSub: "postgres://corgi:****@localhost:5432/api",
		},
		{
			name:    "aws access key id",
			in:      "using AKIAIOSFODNN7EXAMPLE for s3",
			leaked:  "AKIAIOSFODNN7EXAMPLE",
			wantSub: "using **** for s3",
		},
		{
			name:    "jwt",
			in:      "anon key eyJhbGciOiJub25lIn0.eyJzdWIiOiJmYWtlIn0.AAAAAAAAAAAAAAAAAAAAAA",
			leaked:  "AAAAAAAAAAAAAAAAAAAAAA",
			wantSub: "anon key ****",
		},
		{
			name:    "pem block",
			in:      "-----BEGIN RSA PRIVATE KEY-----MIIEowIBAAKCAQEA",
			leaked:  "MIIEowIBAAKCAQEA",
			wantSub: "****",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactLine(tc.in)
			if strings.Contains(got, tc.leaked) {
				t.Fatalf("secret %q survived redaction: %q", tc.leaked, got)
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("want %q in %q", tc.wantSub, got)
			}
		})
	}
}

func TestRedactLineLeavesOrdinaryLinesAlone(t *testing.T) {
	for _, in := range []string{
		"api listening on http://localhost:3000",
		"✗ [E_DANGLING_DEP] service \"web\" depends on unknown service \"api\"",
		"port 3000 busy (services.web) — held by node(pid=4242)",
		"feature: api -> feature/add-metrics",
		"GET /health 200 in 4ms",
	} {
		if got := RedactLine(in); got != in {
			t.Fatalf("ordinary line rewritten:\n in: %q\nout: %q", in, got)
		}
	}
}

func TestRedactLineMaskIsFixedWidth(t *testing.T) {
	short := RedactLine("PASSWORD=abcd")
	long := RedactLine("PASSWORD=abcdefghijklmnopqrstuvwxyz0123456789")
	if short != long {
		t.Fatalf("mask leaks secret length: %q vs %q", short, long)
	}
}
