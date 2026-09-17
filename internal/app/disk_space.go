package app

import (
	"fmt"
	"os"
	"path/filepath"
)

const minFreeDiskSpaceBytes = 1024 * 1024 * 1024 // 1 GB minimum required safety buffer

// ensureDiskSpace verifies that the target directory has at least 1 GB of free space.
// If disk space is critically low, it returns an error to prevent writing corrupt/partial files.
func ensureDiskSpace(path string) error {
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	free, err := checkFreeSpace(dir)
	if err != nil {
		return nil // don't block if OS call fails (e.g. exotic network mount)
	}
	if free < minFreeDiskSpaceBytes {
		return fmt.Errorf("磁盘可用空间过低（仅剩 %.1f MB，低于安全阈值 1GB），已暂停写入以保护系统", float64(free)/1024/1024)
	}
	return nil
}
