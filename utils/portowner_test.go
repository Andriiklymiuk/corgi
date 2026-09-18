package utils

import (
	"reflect"
	"testing"
)

const procNetTCPSample = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 41234 1 0000000000000000 100 0 0 10 0
   1: 00000000:0BB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 41999 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F90 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 42000 1 0000000000000000 20 4 30 10 -1
`

func TestParseProcNetTCPFindsListeningInodesOnly(t *testing.T) {
	if got := parseProcNetTCP(procNetTCPSample, 8080); !reflect.DeepEqual(got, []uint64{41234}) {
		t.Fatalf("8080: got %v", got)
	}
	if got := parseProcNetTCP(procNetTCPSample, 3000); !reflect.DeepEqual(got, []uint64{41999}) {
		t.Fatalf("3000: got %v", got)
	}
	if got := parseProcNetTCP(procNetTCPSample, 5432); got != nil {
		t.Fatalf("5432: got %v", got)
	}
}
