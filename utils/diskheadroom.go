package utils

import "fmt"

const (
	diskBaseBytes     = 2 << 30
	diskPerDatabase   = 3 << 30
	diskPerService    = 1 << 30
	bytesInGigabyte   = 1 << 30
	diskUnknownIsFine = true
)

func DiskHeadroom(corgi *CorgiCompose, path string) (need uint64, free uint64, ok bool, known bool) {
	need = diskBaseBytes +
		uint64(len(corgi.DatabaseServices))*diskPerDatabase +
		uint64(len(corgi.Services))*diskPerService

	free, known = FreeDiskBytes(path)
	if !known {
		return need, 0, true, false
	}
	return need, free, free >= need, true
}

func FormatGigabytes(b uint64) string {
	return fmt.Sprintf("%.1fG", float64(b)/float64(bytesInGigabyte))
}
