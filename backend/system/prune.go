package system

import (
	"os"
)

// PurgeCaches executes paccache and journalctl vacuum
func PurgeCaches() ([]string, error) {
	var logs []string

	// 1. Pacman Cache (Keep 2)
	out1, err := RunHostCommand("paccache -r -k 2")
	if err == nil {
		logs = append(logs, "Pacman Cache: "+out1)
	}

	// 2. Pacman Uninstalled (Remove all)
	out2, err := RunHostCommand("paccache -ruk 0")
	if err == nil {
		logs = append(logs, "Pacman Uninstalled: "+out2)
	}

	// 3. Journal Vacuum (50M)
	out3, err := RunHostCommand("journalctl --vacuum-size=50M")
	if err == nil {
		logs = append(logs, "Journal Vacuum: "+out3)
	}

	return logs, nil
}

// ClearUserCache deletes thumbnails and shader caches
func ClearUserCache() error {
	paths := []string{
		"/host/home/nui/.cache/thumbnails",
		"/host/home/nui/.cache/mesa_shader_cache",
	}

	for _, path := range paths {
		// Use os.RemoveAll directly since we have RW mount
		os.RemoveAll(path)
		// Re-create the directory to keep it clean but present
		os.MkdirAll(path, 0755)
	}

	return nil
}
