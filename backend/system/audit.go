package system

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirSize returns the size of a directory in bytes
func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// FormatSize converts bytes to a human-readable string
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetPacmanCacheSize returns the size of the mounted pacman cache
func GetPacmanCacheSize() (int64, error) {
	return DirSize("/host/var/cache/pacman/pkg")
}

// GetJournalUsage returns the size of the mounted systemd journals
func GetJournalUsage() (int64, error) {
	return DirSize("/host/var/log/journal")
}

// GetUserCacheSize returns the size of specific user caches (thumbnails, etc.)
func GetUserCacheSize() (int64, error) {
	thumbSize, _ := DirSize("/host/home/nui/.cache/thumbnails")
	mesaSize, _ := DirSize("/host/home/nui/.cache/mesa_shader_cache")
	return thumbSize + mesaSize, nil
}
