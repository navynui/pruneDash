package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// RunHostCommand (moved from prune.go for shared use)
func RunHostCommand(command string) (string, error) {
	// Use privileged execution via nsenter
	cmd := exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "-i", "bash", "-c", command)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

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

// GetPacmanMetrics returns (total, reclaimable)
func GetPacmanMetrics() (int64, int64, error) {
	total, _ := DirSize("/host/var/cache/pacman/pkg")
	
	out, err := RunHostCommand("paccache -d -k 2")
	if err != nil {
		return total, 0, err
	}

	reclaimable := parsePaccacheOutput(out)
	return total, reclaimable, nil
}

// GetJournalMetrics returns (total, reclaimable)
func GetJournalMetrics() (int64, int64, error) {
	total, err := DirSize("/host/var/log/journal")
	if err != nil {
		return 0, 0, err
	}
	
	limit := int64(50 * 1024 * 1024) // 50MB
	if total > limit {
		return total, total - limit, nil
	}
	return total, 0, nil
}

func parsePaccacheOutput(out string) int64 {
	re := regexp.MustCompile(`Disk space saved: ([\d\.]+)\s+(\w+)`)
	matches := re.FindStringSubmatch(out)
	if len(matches) < 3 {
		return 0
	}

	val, _ := strconv.ParseFloat(matches[1], 64)
	unit := matches[2]
	
	var bytes int64
	switch unit {
	case "KiB": bytes = int64(val * 1024)
	case "MiB": bytes = int64(val * 1024 * 1024)
	case "GiB": bytes = int64(val * 1024 * 1024 * 1024)
	default: bytes = int64(val)
	}
	return bytes
}

// GetUserCacheSize returns the size of specific user caches (thumbnails, etc.)
func GetUserCacheSize() (int64, error) {
	thumbSize, _ := DirSize("/host/home/nui/.cache/thumbnails")
	mesaSize, _ := DirSize("/host/home/nui/.cache/mesa_shader_cache")
	return thumbSize + mesaSize, nil
}
