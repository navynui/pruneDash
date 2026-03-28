package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync" // Added sync for parallel tasks
	"time"
)

// LogActivity is now internal to each scan via channels

type ScanResult struct {
	Env            CoreEnvironment
	Assets         []ProtectedAsset
	PrunableAssets []ProtectedAsset
	PrunableSize   int64
	PacmanMetrics  struct{ Total, Reclaim int64 }
	JournalMetrics struct{ Total, Reclaim int64 }
	UserCacheSize  int64
	CompletedAt    time.Time
}

func LogToChan(ch chan string, msg string) {
	fmt.Printf("[AUDIT] %s\n", msg)
	if ch != nil {
		select {
		case ch <- fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg):
		default:
		}
	}
}

func LogActivity(msg string) {
	LogToChan(nil, msg)
}

func LogError(msg string) {
	LogToChan(nil, "<span class='text-red-500 font-bold'>ERROR: "+msg+"</span>")
}

func LogErrorToChan(ch chan string, msg string) {
	LogToChan(ch, fmt.Sprintf("<span class='text-red-500 font-bold'>ERROR: %s</span>", msg))
}

// PerformAsyncScan runs all detection and audit tasks in parallel where possible.
func PerformAsyncScan(logChan chan string) ScanResult {
	var res ScanResult
	var wg sync.WaitGroup
	var mu sync.Mutex // Protect res fields during parallel writes

	LogToChan(logChan, "--- Starting Parallel System Audit ---")
	startTime := time.Now()

	// 1. Environment Detection (Serial - first)
	LogToChan(logChan, "Detecting System Environment...")
	env := DetectEnvironment(logChan)
	res.Env = env

	// 2. Pacman Metrics (Parallel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		LogToChan(logChan, "Auditing Package Cache (paccache)...")
		total, reclaim, err := GetPacmanMetrics()
		if err != nil {
			LogErrorToChan(logChan, "Pacman audit failed: " + err.Error())
		}
		mu.Lock()
		res.PacmanMetrics.Total = total
		res.PacmanMetrics.Reclaim = reclaim
		mu.Unlock()
	}()

	// 3. Journal Metrics (Parallel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		LogToChan(logChan, "Auditing Systemd Journals (journalctl)...")
		total, reclaim, err := GetJournalMetrics()
		if err != nil {
			LogErrorToChan(logChan, "Journal audit failed: " + err.Error())
		}
		mu.Lock()
		res.JournalMetrics.Total = total
		res.JournalMetrics.Reclaim = reclaim
		mu.Unlock()
	}()

	// 4. User Cache (Parallel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		LogToChan(logChan, "Scanning User Cache (~/.cache)...")
		size, err := GetUserCacheSize()
		if err != nil {
			LogErrorToChan(logChan, "User cache scan failed: " + err.Error())
		}
		mu.Lock()
		res.UserCacheSize = size
		mu.Unlock()
	}()

	// 5. Protected Assets (Parallel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		assets := GetProtectedAssets(logChan, env)
		mu.Lock()
		res.Assets = assets
		mu.Unlock()

		// 5.1 Prunable Assets
		prunable, size := GetPrunableAssets(logChan, assets)
		mu.Lock()
		res.PrunableAssets = prunable
		res.PrunableSize = size
		mu.Unlock()
	}()

	wg.Wait()
	res.CompletedAt = time.Now()
	LogToChan(logChan, fmt.Sprintf("--- Audit complete in %.2fs ---", time.Since(startTime).Seconds()))
	
	return res
}

type CoreEnvironment struct {
	WM         string   `json:"wm"`
	DM         string   `json:"dm"`
	Bootloader string   `json:"bootloader"`
	Apps       []string `json:"apps"`
}

type ProtectedAsset struct {
	Name          string `json:"name"`
	Type          string `json:"type"`          // "theme", "icon", "font", "cursor"
	Path          string `json:"path"`          // Absolute path on host
	Source        string `json:"source"`        // Config file where found
	Priority      int    `json:"priority"`      // 1=core, 2=app, 3=global
	Size          int64  `json:"size"`          // Size in bytes
	FormattedSize string `json:"formattedSize"` // Readable size (e.g., "1.2 MB")
}

// DetectEnvironment returns the detected core environment of the host system.
func DetectEnvironment(logChan chan string) CoreEnvironment {
	env := CoreEnvironment{
		WM:         DetectWM(logChan),
		DM:         DetectDM(logChan),
		Bootloader: DetectBootloader(logChan),
		Apps:       DetectApps(logChan),
	}
	
	if env.WM == "unknown" {
		LogErrorToChan(logChan, "Failed to identify active Window Manager.")
	}
	
	LogToChan(logChan, fmt.Sprintf("Environment: WM=%s, DM=%s, Bootloader=%s, Apps=%d found", env.WM, env.DM, env.Bootloader, len(env.Apps)))
	return env
}

// DetectWM identifies the active Window Manager or Desktop Environment.
func DetectWM(logChan chan string) string {
	LogToChan(logChan, "Checking environment variables (XDG_CURRENT_DESKTOP)...")
	// 1. Check environment variables
	env, _ := RunHostCommand("printenv XDG_CURRENT_DESKTOP")
	env = strings.TrimSpace(env)
	if env != "" {
		LogToChan(logChan, "Found active session: " + env)
		return strings.ToLower(env)
	}

	LogToChan(logChan, "Environment variables ambiguous. Searching for config files...")
	// 2. Check for config files if env is ambiguous
	configs := map[string]string{
		"hyprland": "/host/home/nui/.config/hypr/hyprland.conf",
		"niri":     "/host/home/nui/.config/niri/config.kdl",
		"sway":     "/host/home/nui/.config/sway/config",
	}

	for wm, path := range configs {
		LogToChan(logChan, fmt.Sprintf("Probing for %s config at %s", wm, path))
		if _, err := os.Stat(path); err == nil {
			LogToChan(logChan, fmt.Sprintf("Detected %s via configuration match.", wm))
			return wm
		}
	}

	return "unknown"
}

// DetectDM identifies the active Display Manager.
func DetectDM(logChan chan string) string {
	LogToChan(logChan, "Querying systemd for active display-manager service...")
	// Check active display-manager service
	out, err := RunHostCommand("systemctl list-units --type=service --state=active | grep -E 'sddm|gdm|lightdm|ly'")
	if err == nil && out != "" {
		LogToChan(logChan, "Parsing active service units...")
		if strings.Contains(out, "sddm") { return "sddm" }
		if strings.Contains(out, "gdm") { return "gdm" }
		if strings.Contains(out, "lightdm") { return "lightdm" }
		if strings.Contains(out, "ly") { return "ly" }
	}

	LogToChan(logChan, "Active service unit not found. Checking unit symlinks...")
	// Fallback: check unit symlink
	out, err = RunHostCommand("ls -l /etc/systemd/system/display-manager.service")
	if err == nil {
		LogToChan(logChan, "Resolving display-manager.service symlink...")
		if strings.Contains(out, "sddm") { return "sddm" }
		if strings.Contains(out, "gdm") { return "gdm" }
		if strings.Contains(out, "lightdm") { return "lightdm" }
		if strings.Contains(out, "ly") { return "ly" }
	}

	return "none"
}

// DetectBootloader identifies the active Bootloader.
func DetectBootloader(logChan chan string) string {
	LogToChan(logChan, "Probing for active bootloader...")
	
	// Check for GRUB
	if _, err := os.Stat("/host/boot/grub/grub.cfg"); err == nil {
		return "grub"
	}

	// Check for rEFInd - recursive search on host via nsenter
	// Search in /boot and /efi up to 3 levels deep
	out, _ := RunHostCommand("find /boot /efi /boot/efi -maxdepth 4 -name refind.conf 2>/dev/null")
	if strings.TrimSpace(out) != "" {
		return "refind"
	}

	// Check for systemd-boot
	if _, err := os.Stat("/host/boot/loader/loader.conf"); err == nil {
		return "systemd-boot"
	}

	return "unknown"
}

// GetProtectedAssets scans for active system assets.
func GetProtectedAssets(logChan chan string, env CoreEnvironment) []ProtectedAsset {
	var assets []ProtectedAsset

	LogToChan(logChan, "Starting Core Asset Detection...")

	// 1. Parse GTK Settings
	LogToChan(logChan, "Detecting GTK Settings...")
	assets = append(assets, ParseGTKSettings("/host/home/nui/.config/gtk-3.0/settings.ini")...)
	assets = append(assets, ParseGTKSettings("/host/home/nui/.config/gtk-4.0/settings.ini")...)

	// 2. Parse WM Config (using pre-detected WM)
	if strings.Contains(strings.ToLower(env.WM), "hyprland") {
		LogToChan(logChan, "Detecting Hyprland Configurations...")
		hyprAssets := ParseHyprlandConfig("/host/home/nui/.config/hypr/hyprland.conf")
		assets = append(assets, hyprAssets...)
	}

	assets = deduplicateAssets(assets)

	// 3. Resolve Paths and Sizes
	LogToChan(logChan, "Resolving Asset Paths & Sizes on host...")
	for i := range assets {
		assets[i].Path = ResolveAssetPath(assets[i].Name, assets[i].Type)
		if assets[i].Path != "unknown" && assets[i].Path != "" {
			size, _ := DirSize(assets[i].Path)
			assets[i].Size = size
			assets[i].FormattedSize = FormatSize(size)
		}
	}

	LogToChan(logChan, fmt.Sprintf("Protection complete: %d assets locked.", len(assets)))
	return assets
}

func ResolveAssetPath(name string, assetType string) string {
	if name == "" {
		return ""
	}

	var searchPaths []string
	switch assetType {
	case "theme":
		searchPaths = []string{"/host/usr/share/themes", "/host/home/nui/.local/share/themes", "/host/home/nui/.themes"}
	case "icon", "cursor":
		searchPaths = []string{"/host/usr/share/icons", "/host/home/nui/.local/share/icons", "/host/home/nui/.icons"}
	case "font":
		searchPaths = []string{"/host/usr/share/fonts", "/host/home/nui/.local/share/fonts", "/host/home/nui/.fonts"}
	}

	for _, base := range searchPaths {
		path := filepath.Join(base, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "unknown"
}

func ParseGTKSettings(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil {
		return assets
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gtk-theme-name") {
			name := extractValue(line)
			if name != "" {
				assets = append(assets, ProtectedAsset{Name: name, Type: "theme", Source: "GTK Settings", Priority: 1})
			}
		}
		if strings.HasPrefix(line, "gtk-icon-theme-name") {
			name := extractValue(line)
			if name != "" {
				assets = append(assets, ProtectedAsset{Name: name, Type: "icon", Source: "GTK Settings", Priority: 1})
			}
		}
		if strings.HasPrefix(line, "gtk-font-name") {
			name := extractValue(line)
			if name != "" {
				assets = append(assets, ProtectedAsset{Name: name, Type: "font", Source: "GTK Settings", Priority: 1})
			}
		}
		if strings.HasPrefix(line, "gtk-cursor-theme-name") {
			name := extractValue(line)
			if name != "" {
				assets = append(assets, ProtectedAsset{Name: name, Type: "cursor", Source: "GTK Settings", Priority: 1})
			}
		}
	}
	return assets
}

func ParseHyprlandConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil {
		return assets
	}

	// Simple regex for fonts in hyprland or executive themes
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Example: exec = hyprctl setcursor Bibata-Modern-Ice 24
		if strings.Contains(line, "setcursor") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "setcursor" && i+1 < len(parts) {
					assets = append(assets, ProtectedAsset{Name: parts[i+1], Type: "cursor", Source: "Hyprland Config", Priority: 1})
				}
			}
		}
	}
	return assets
}

// DetectApps identifies frequently used and startup apps.
func DetectApps(logChan chan string) []string {
	LogToChan(logChan, "Scanning for Startup and Common Apps...")
	var apps []string

	// 1. Startup Apps
	files, _ := os.ReadDir("/host/home/nui/.config/autostart")
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".desktop") {
			apps = append(apps, strings.TrimSuffix(f.Name(), ".desktop"))
		}
	}

	// 2. Common Rice Tools/Apps
	common := []string{"alacritty", "kitty", "foot", "waybar", "polybar", "rofi", "wofi", "firefox", "thunar", "dolphin"}
	for _, app := range common {
		if _, err := RunHostCommand("command -v " + app); err == nil {
			apps = append(apps, app)
		}
	}

	return deduplicateStrings(apps)
}

// GetInstalledAssets lists all theme, icon, and font assets found on the host.
func GetInstalledAssets(logChan chan string) []ProtectedAsset {
	LogToChan(logChan, "Scanning host for all installed aesthetic assets...")
	var assets []ProtectedAsset

	types := map[string][]string{
		"theme": {"/host/usr/share/themes", "/host/home/nui/.local/share/themes", "/host/home/nui/.themes"},
		"icon":  {"/host/usr/share/icons", "/host/home/nui/.local/share/icons", "/host/home/nui/.icons"},
		"font":  {"/host/usr/share/fonts", "/host/home/nui/.local/share/fonts", "/host/home/nui/.fonts"},
	}

	for aType, paths := range types {
		for _, path := range paths {
			files, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || aType == "font" {
					fullPath := filepath.Join(path, f.Name())
					size, _ := DirSize(fullPath)
					assets = append(assets, ProtectedAsset{
						Name:          f.Name(),
						Type:          aType,
						Path:          fullPath,
						Size:          size,
						FormattedSize: FormatSize(size),
					})
				}
			}
		}
	}
	return assets
}

// GetPrunableAssets identifies assets that are installed but not currently protected/used.
func GetPrunableAssets(logChan chan string, protected []ProtectedAsset) ([]ProtectedAsset, int64) {
	LogToChan(logChan, "Calculating prunable asset delta...")
	installed := GetInstalledAssets(logChan)
	
	protectedMap := make(map[string]bool)
	for _, p := range protected {
		protectedMap[p.Name+p.Type] = true
	}

	var prunable []ProtectedAsset
	var totalSize int64

	for _, ins := range installed {
		if !protectedMap[ins.Name+ins.Type] {
			prunable = append(prunable, ins)
			size, _ := DirSize(ins.Path)
			totalSize += size
		}
	}

	LogToChan(logChan, fmt.Sprintf("Audit complete: Found %d prunable assets (%s reclaimable).", len(prunable), FormatSize(totalSize)))
	return prunable, totalSize
}

func extractValue(line string) string {
	parts := strings.Split(line, "=")
	if len(parts) < 2 {
		return ""
	}
	val := strings.TrimSpace(parts[1])
	// Remove quotes if present
	val = strings.Trim(val, "\"'")
	return val
}

func deduplicateAssets(assets []ProtectedAsset) []ProtectedAsset {
	seen := make(map[string]bool)
	var j []ProtectedAsset
	for _, a := range assets {
		if _, ok := seen[a.Name+a.Type]; !ok {
			seen[a.Name+a.Type] = true
			j = append(j, a)
		}
	}
	return j
}

func deduplicateStrings(input []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, s := range input {
		if !unique[s] {
			unique[s] = true
			result = append(result, s)
		}
	}
	return result
}

