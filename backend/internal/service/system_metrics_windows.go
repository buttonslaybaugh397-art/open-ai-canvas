//go:build windows

package service

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func platformMemory() (uint64, uint64, bool)  { return 0, 0, false }
func platformLoadAverage() ([3]float64, bool) { return [3]float64{}, false }

func collectSystemDisk(path string) SystemPerformanceDisk {
	if path == "" {
		path = "."
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return SystemPerformanceDisk{}
	}
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return SystemPerformanceDisk{}
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(pathPointer, &available, &total, &free); err != nil || total == 0 {
		return SystemPerformanceDisk{}
	}
	writable := false
	if file, err := os.CreateTemp(path, ".system-performance-write-check-"); err == nil {
		writable = true
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
	}
	used := total - free
	return SystemPerformanceDisk{
		Available:    true,
		Writable:     writable,
		TotalBytes:   total,
		UsedBytes:    used,
		FreeBytes:    available,
		UsagePercent: float64(used) / float64(total) * 100,
	}
}
