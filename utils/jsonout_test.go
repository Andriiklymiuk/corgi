package utils

import (
	"bytes"
	"encoding/json"
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
