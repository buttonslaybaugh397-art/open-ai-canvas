//go:build windows

package hostupdate

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func availableDiskBytes(path string) (int64, error) {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("解析备份目录：%w", err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(directory, &available, nil, nil); err != nil {
		return 0, err
	}
	if available > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(available), nil
}
