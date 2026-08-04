//go:build windows

package storage

import "golang.org/x/sys/windows"

// diskUsage reports the total, free, and used bytes on the filesystem that
// contains the storage root.
func diskUsage(root string) (total, free, used int64) {
	var available, capacity, unused uint64
	if err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(root), &available, &capacity, &unused); err != nil {
		return 0, 0, 0
	}
	return int64(capacity), int64(available), int64(capacity - unused)
}
