//go:build !darwin

package tools

func getMemoryInfo() string {
	return ""
}

func getDiskInfo() string {
	return ""
}
