package usage

import (
	"os"
	"path/filepath"
	"testing"
)

// The daemon reads a transcript from where it stopped: the rows since,
// never the whole file again, and a row still being written waits.
func TestSumFromReadsOnlyWhatIsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	row := `{"type":"assistant","message":{"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(path, []byte(row+row), 0o600); err != nil {
		t.Fatal(err)
	}
	got, off := SumFrom(path, 0)
	if got.Total() != 230 || got.Turns != 2 || off != int64(2*len(row)) {
		t.Fatalf("two rows: %+v at %d", got, off)
	}
	// One more row, and half of another still being written.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(row + row[:20])
	f.Close()
	got, off2 := SumFrom(path, off)
	if got.Total() != 115 || off2 != off+int64(len(row)) {
		t.Fatalf("the new row only, the half one not yet: %+v at %d", got, off2)
	}
	got, off3 := SumFrom(path, off2)
	if got.Total() != 0 || off3 != off2 {
		t.Fatalf("nothing new: %+v at %d", got, off3)
	}
}
