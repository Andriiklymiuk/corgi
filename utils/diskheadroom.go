package utils

import "fmt"

// Rough per-item cost of booting a stack, used only to turn "some disk free"
// into a yes/no. A database is an image plus its data directory; a service is
// its dependency tree. Both are estimates, and the check says so.
const (
	diskBaseBytes     = 2 << 30 // toolchains, corgi itself, log and cache dirs
	diskPerDatabase   = 3 << 30
	diskPerService    = 1 << 30
	bytesInGigabyte   = 1 << 30
	diskUnknownIsFine = true
)

// DiskHeadroom compares free space at path with a rough estimate of what
// booting this compose needs.
//
// Running out mid-boot is the least recognisable CI failure there is: the
// symptom is a random service failing to build, not a disk message. Hosted
// runners are provisioned tighter than a full stack needs, so the answer is
// worth two seconds up front.
func DiskHeadroom(corgi *CorgiCompose, path string) (need uint64, free uint64, ok bool, known bool) {
	need = diskBaseBytes +
		uint64(len(corgi.DatabaseServices))*diskPerDatabase +
		uint64(len(corgi.Services))*diskPerService

	free, known = FreeDiskBytes(path)
	if !known {
		// A platform that cannot answer must not fail the run over a number
		// corgi does not have.
		return need, 0, true, false
	}
	return need, free, free >= need, true
}

// FormatGigabytes renders a byte count the way the check reports it.
func FormatGigabytes(b uint64) string {
	return fmt.Sprintf("%.1fG", float64(b)/float64(bytesInGigabyte))
}
