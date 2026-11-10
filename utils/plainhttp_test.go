package utils

import "testing"

func TestPlainHTTPURL(t *testing.T) {
	cases := map[[2]string]string{
		{"127.0.0.1:7777", ""}:                        "http://127.0.0.1:7777",
		{"localhost:4566", "/000000000000/orders"}:    "http://localhost:4566/000000000000/orders",
		{"captive.apple.com", "/hotspot-detect.html"}: "http://captive.apple.com/hotspot-detect.html",
		{"[::1]:80", "/app"}:                          "http://[::1]:80/app",
	}
	for in, want := range cases {
		if got := PlainHTTPURL(in[0], in[1]); got != want {
			t.Errorf("PlainHTTPURL(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
