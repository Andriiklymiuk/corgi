// Package atomicfile replaces a file's contents in one step, so a crash
// mid-write cannot leave a half-written file behind.
package atomicfile

import "os"

// Write puts data in a sibling ".tmp" file and renames it over path. The temp
// file is removed when the rename fails, which would otherwise leave it behind
// for good — nothing else ever cleans it up.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
