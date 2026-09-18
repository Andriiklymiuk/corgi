package cmd

import "testing"

func TestMaskPulseURL(t *testing.T) {
	if got := maskPulseURL("https://hc-ping.com/0123456789abcdef"); got != "https://hc-ping.com/0123…cdef" {
		t.Fatal(got)
	}
	if got := maskPulseURL("https://x.io/ab"); got != "https://x.io/ab" {
		t.Fatal(got)
	}
	if got := maskPulseURL("::nope"); got != "***" {
		t.Fatal(got)
	}
}
