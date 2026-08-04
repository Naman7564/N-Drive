//go:build !windows

package storage

import "golang.org/x/sys/unix"

// diskUsage reports the total, free, and used bytes on the filesystem that
// contains the storage root.
func diskUsage(root string) (total, free, used int64) {
	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		return 0, 0, 0
	}
	total = int64(stat.Blocks) * int64(stat.Bsize)
	free = int64(stat.Bavail) * int64(stat.Bsize)
	used = (int64(stat.Blocks) - int64(stat.Bfree)) * int64(stat.Bsize)
	return total, free, used
}
