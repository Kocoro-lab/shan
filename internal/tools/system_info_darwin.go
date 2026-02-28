package tools

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func getMemoryInfo() string {
	// macOS: use sysctl for total memory
	out, err := exec.Command("sysctl", "-n", "hw.memsize").CombinedOutput()
	if err != nil {
		return ""
	}
	totalBytes, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return ""
	}

	// Get page size and free pages from vm_stat
	pageSize := uint64(syscall.Getpagesize())
	vmOut, err := exec.Command("vm_stat").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Memory Total: %s\n", formatBytes(totalBytes))
	}

	freePages := uint64(0)
	for _, line := range strings.Split(string(vmOut), "\n") {
		if strings.HasPrefix(line, "Pages free:") {
			val := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "Pages free:"), "."))
			if n, err := strconv.ParseUint(val, 10, 64); err == nil {
				freePages = n
			}
		}
	}
	freeBytes := freePages * pageSize

	return fmt.Sprintf("Memory Total: %s\nMemory Free: %s\n", formatBytes(totalBytes), formatBytes(freeBytes))
}

func getDiskInfo() string {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return ""
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	return fmt.Sprintf("Disk Total: %s\nDisk Free: %s\n", formatBytes(total), formatBytes(free))
}

func formatBytes(b uint64) string {
	const (
		MB = 1024 * 1024
		GB = 1024 * MB
	)
	if b >= GB {
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	}
	return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
}
