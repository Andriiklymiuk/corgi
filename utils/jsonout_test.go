package utils

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPrintJSONTo(t *testing.T) {
	var buf bytes.Buffer
	PrintJSONTo(&buf, map[string]int{"a": 1})
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if got["a"] != 1 {
		t.Errorf("got %v, want a=1", got)
	}
}

func TestJSONErrorShape(t *testing.T) {
	var buf bytes.Buffer
	WriteJSONError(&buf, "PORT_BUSY", "port 5432 in use")
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Error.Code != "PORT_BUSY" || got.Error.Message != "port 5432 in use" {
		t.Errorf("got %+v", got.Error)
	}
}

func TestPrintJSON_WritesStdout(t *testing.T) {
	out := captureStdout(t, func() { PrintJSON(map[string]int{"n": 7}) })
	if !strings.Contains(out, `"n": 7`) {
		t.Errorf("PrintJSON stdout = %q", out)
	}
}

func TestJSONError_WritesStdout(t *testing.T) {
	out := captureStdout(t, func() { JSONError("BOOM", "kaboom") })
	if !strings.Contains(out, "BOOM") || !strings.Contains(out, "kaboom") {
		t.Errorf("JSONError stdout = %q", out)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
