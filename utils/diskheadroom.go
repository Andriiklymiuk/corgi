package utils

import "fmt"

// Rough per-item cost of a boot, only precise enough for a yes/no.
const (
	diskBaseBytes     = 2 << 30 // toolchains, corgi itself, log and cache dirs
	diskPerDatabase   = 3 << 30
	diskPerService    = 1 << 30
	bytesInGigabyte   = 1 << 30
	diskUnknownIsFine = true
)

// DiskHeadroom compares free space with an estimate of what this compose needs.
// Running out mid-boot reads as a random service failing to build, never as a
// disk message.
func DiskHeadroom(corgi *CorgiCompose, path string) (need uint64, free uint64, ok bool, known bool) {
	need = diskBaseBytes +
		uint64(len(corgi.DatabaseServices))*diskPerDatabase +
		uint64(len(corgi.Services))*diskPerService

	free, known = FreeDiskBytes(path)
	if !known {
		// Do not fail a run over a number corgi does not have.
		return need, 0, true, false
	}
	return need, free, free >= need, true
}

// FormatGigabytes renders a byte count for the check's message.
func FormatGigabytes(b uint64) string {
	return fmt.Sprintf("%.1fG", float64(b)/float64(bytesInGigabyte))
}
