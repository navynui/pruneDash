package system

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Subtype       string `json:"subtype"`       // "GTK3", "GTK4", etc.
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

	// App-specific configs
	LogToChan(logChan, "Parsing Configurations for frequently used apps...")
	for _, app := range env.Apps {
		if app == "alacritty" {
			assets = append(assets, ParseAlacrittyConfig("/host/home/nui/.config/alacritty/alacritty.toml")...)
		} else if app == "kitty" {
			assets = append(assets, ParseKittyConfig("/host/home/nui/.config/kitty/kitty.conf")...)
		} else if app == "rofi" {
			assets = append(assets, ParseRofiConfig("/host/home/nui/.config/rofi/config.rasi")...)
		} else if app == "waybar" {
			assets = append(assets, ParseWaybarConfig("/host/home/nui/.config/waybar/style.css")...)
		} else if app == "wofi" {
			assets = append(assets, ParseWofiConfig("/host/home/nui/.config/wofi/config")...)
		} else if app == "code" || app == "vscode" || app == "vscodium" {
			assets = append(assets, ParseVSCodeConfig("/host/home/nui/.config/Code/User/settings.json")...)
			assets = append(assets, ParseVSCodeConfig("/host/home/nui/.config/VSCodium/User/settings.json")...)
			assets = append(assets, ParseVSCodeConfig("/host/home/nui/.config/Code - OSS/User/settings.json")...)
		} else if app == "zed" {
			assets = append(assets, ParseZedConfig("/host/home/nui/.config/zed/settings.json")...)
		} else if app == "firefox" {
			assets = append(assets, ParseFirefoxConfig()...)
		} else if app == "chromium" || app == "brave" || app == "google-chrome" {
			assets = append(assets, ParseChromiumConfig()...)
		} else if app == "dolphin" || env.WM == "plasma" {
			assets = append(assets, ParseKDEConfig("/host/home/nui/.config/kdeglobals")...)
		}
	}

	// 3. Parse DM (SDDM) Config
	if env.DM == "sddm" {
		LogToChan(logChan, "Detecting SDDM Login Theme...")
		theme := ParseSDDMConfig(logChan)
		if theme != "" {
			assets = append(assets, ProtectedAsset{Name: theme, Type: "theme", Source: "SDDM Config", Priority: 1})
		}
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
			if assets[i].Type == "theme" {
				assets[i].Subtype = detectThemeType(assets[i].Path)
			}
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
		searchPaths = []string{
			"/host/usr/share/themes", 
			"/host/home/nui/.local/share/themes", 
			"/host/home/nui/.themes",
			"/host/usr/share/sddm/themes",
		}
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

// Detects assets from Alacritty config
func ParseAlacrittyConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil { return assets }
	
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "family =") || strings.HasPrefix(line, "family:") {
			name := extractValue(line)
			if name != "" {
				assets = append(assets, ProtectedAsset{Name: name, Type: "font", Source: "Alacritty Config", Priority: 2})
			}
		}
	}
	return assets
}

// Detects assets from Kitty config
func ParseKittyConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil { return assets }

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "font_family") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				name := strings.Join(parts[1:], " ")
				assets = append(assets, ProtectedAsset{Name: name, Type: "font", Source: "Kitty Config", Priority: 2})
			}
		}
		if strings.HasPrefix(line, "include") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				themeName := filepath.Base(parts[1])
				themeName = strings.TrimSuffix(themeName, filepath.Ext(themeName))
				assets = append(assets, ProtectedAsset{Name: themeName, Type: "theme", Source: "Kitty Theme", Priority: 2})
			}
		}
	}
	return assets
}

// Detects assets from Rofi config
func ParseRofiConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil { return assets }

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "font:") {
			val := extractValue(strings.TrimSuffix(line, ";"))
			if val != "" {
				// Remove font size
				words := strings.Fields(val)
				if len(words) > 1 {
					val = strings.Join(words[:len(words)-1], " ")
				}
				assets = append(assets, ProtectedAsset{Name: val, Type: "font", Source: "Rofi Config", Priority: 2})
			}
		}
		if strings.HasPrefix(line, "@theme") || strings.HasPrefix(line, "@import") {
			val := extractValue(strings.TrimSuffix(line, ";"))
			if val != "" {
				themeName := filepath.Base(val)
				themeName = strings.TrimSuffix(themeName, filepath.Ext(themeName))
				assets = append(assets, ProtectedAsset{Name: themeName, Type: "theme", Source: "Rofi Theme", Priority: 2})
			}
		}
	}
	return assets
}

// Detects assets from Waybar config
func ParseWaybarConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil { return assets }

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "font-family:") {
			val := extractValue(strings.TrimSuffix(line, ";"))
			if val != "" {
				// Clean up font names from CSS
				val = strings.Trim(val, `"'`)
				val = strings.Split(val, ",")[0]
				assets = append(assets, ProtectedAsset{Name: val, Type: "font", Source: "Waybar CSS", Priority: 2})
			}
		}
	}
	return assets
}

// Detects assets from Wofi config
func ParseWofiConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, err := os.ReadFile(path)
	if err != nil { return assets }

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "style=") {
			val := extractValue(line)
			if val != "" {
				themeName := filepath.Base(val)
				themeName = strings.TrimSuffix(themeName, filepath.Ext(themeName))
				assets = append(assets, ProtectedAsset{Name: themeName, Type: "theme", Source: "Wofi Style", Priority: 2})
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
	LogToChan(logChan, "Scanning for Startup, Common, and Keybind Apps...")
	var apps []string

	// 1. Startup Apps
	files, _ := os.ReadDir("/host/home/nui/.config/autostart")
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".desktop") {
			apps = append(apps, strings.TrimSuffix(f.Name(), ".desktop"))
		}
	}

	// 2. WM Keybinds (Hyprland & Niri)
	hyprContent, _ := os.ReadFile("/host/home/nui/.config/hypr/hyprland.conf")
	for _, line := range strings.Split(string(hyprContent), "\n") {
		if strings.Contains(line, "exec,") {
			parts := strings.Split(line, "exec,")
			if len(parts) > 1 {
				fields := strings.Fields(parts[1])
				if len(fields) > 0 {
					apps = append(apps, fields[0])
				}
			}
		}
	}
	niriContent, _ := os.ReadFile("/host/home/nui/.config/niri/config.kdl")
	for _, line := range strings.Split(string(niriContent), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "spawn ") || strings.HasPrefix(line, "command ") {
			parts := strings.SplitN(line, "\"", 3)
			if len(parts) >= 2 {
				fields := strings.Fields(parts[1])
				if len(fields) > 0 {
					apps = append(apps, fields[0])
				}
			}
		}
	}

	// 3. Recent Apps
	recentContent, _ := os.ReadFile("/host/home/nui/.local/share/recently-used.xbel")
	for _, line := range strings.Split(string(recentContent), "\n") {
		if strings.Contains(line, "exec=&apos;") {
			parts := strings.Split(line, "exec=&apos;")
			if len(parts) > 1 {
				cmd := strings.Split(parts[1], " ")[0]
				cmd = strings.TrimSuffix(cmd, "&apos;")
				apps = append(apps, cmd)
			}
		}
	}

	// 4. Common Rice Tools/Apps
	common := []string{"alacritty", "kitty", "foot", "waybar", "polybar", "rofi", "wofi", "firefox", "chromium", "zed", "code", "nautilus", "dolphin", "thunar", "brave", "vlc", "mpv"}
	for _, app := range common {
		if _, err := RunHostCommand("command -v " + app); err == nil {
			apps = append(apps, app)
		}
		// Also check if running
		if _, err := RunHostCommand("pgrep -u nui " + app); err == nil {
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
		"theme": {
			"/host/usr/share/themes", 
			"/host/home/nui/.local/share/themes", 
			"/host/home/nui/.themes",
			"/host/usr/share/sddm/themes",
		},
		"icon":  {"/host/usr/share/icons", "/host/home/nui/.local/share/icons", "/host/home/nui/.icons"},
	}

	fontPaths := []string{"/host/usr/share/fonts", "/host/home/nui/.local/share/fonts", "/host/home/nui/.fonts"}

	for aType, paths := range types {
		for _, path := range paths {
			files, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() {
					fullPath := filepath.Join(path, f.Name())
					size, _ := DirSize(fullPath)
					subtype := ""
					if aType == "theme" {
						subtype = detectThemeType(fullPath)
					}
					assets = append(assets, ProtectedAsset{
						Name:          f.Name(),
						Type:          aType,
						Subtype:       subtype,
						Path:          fullPath,
						Size:          size,
						FormattedSize: FormatSize(size),
					})
				}
			}
		}
	}

	// Fonts: flat scan across all font dirs, grouped by filename family prefix
	assets = append(assets, scanFontsFlat(fontPaths)...)

	return assets
}

// scanFontsFlat walks all font paths and groups font files by the part of their
// filename before the first dash (e.g. "FiraSans" from "FiraSans-Bold.ttf").
// Files with no dash use the full stem as the family name.
// Grouping is entirely name-based — folder structure is ignored.
func scanFontsFlat(roots []string) []ProtectedAsset {
	// family -> list of absolute file paths
	groups := make(map[string][]string)

	for _, root := range roots {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".ttf" && ext != ".otf" && ext != ".woff" && ext != ".woff2" {
				return nil
			}
			base := filepath.Base(path)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			// Family = part before first dash
			family := stem
			if idx := strings.Index(stem, "-"); idx > 0 {
				family = stem[:idx]
			}
			groups[family] = append(groups[family], path)
			return nil
		})
	}

	var assets []ProtectedAsset
	for family, files := range groups {
		var totalSize int64
		for _, f := range files {
			info, err := os.Stat(f)
			if err == nil {
				totalSize += info.Size()
			}
		}
		name := family
		if lang := detectLang(family); lang != "" {
			name = fmt.Sprintf("%s (%s)", family, lang)
		}
		assets = append(assets, ProtectedAsset{
			Name:          name,
			Type:          "font",
			Path:          strings.Join(files, "|"),
			Size:          totalSize,
			FormattedSize: FormatSize(totalSize),
			Source:        "System Font",
		})
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
		// Alias cursor as icon since Linux cursors are stored inside system icon directories
		if p.Type == "cursor" {
			protectedMap[p.Name+"icon"] = true
		}
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

var (
	reThai  = regexp.MustCompile(`(?i)\b(TH|THAI)\b|^TH\s`)
	reKorea = regexp.MustCompile(`(?i)\b(KOR|KR|KOREAN)\b`)
	reJapan = regexp.MustCompile(`(?i)\b(JPN|JP|JAPANESE)\b`)
	reChina = regexp.MustCompile(`(?i)\b(CHI|CN|SC|CHINESE|TC|HK|TW)\b`)
	reCJK   = regexp.MustCompile(`(?i)\bCJK\b`)
	reArab  = regexp.MustCompile(`(?i)\b(AR|ARAB|ARABIC)\b`)
	reThaiN = regexp.MustCompile(`(?i)\bTH\b`)
)

func detectLang(input string) string {
	if reThai.MatchString(input) {
		return "Thai"
	}
	if reJapan.MatchString(input) {
		return "Japanese"
	}
	if reKorea.MatchString(input) {
		return "Korean"
	}
	if reChina.MatchString(input) {
		return "Chinese"
	}
	if reCJK.MatchString(input) {
		return "East Asian"
	}
	if reArab.MatchString(input) {
		return "Arabic"
	}
	
	// Fallback for common patterns
	u := strings.ToUpper(input)
	if strings.Contains(u, "THAI") { return "Thai" }
	if strings.Contains(u, "ARABIC") { return "Arabic" }
	if strings.Contains(u, "CJK") { return "East Asian" }
	
	return ""
}

func detectThemeType(path string) string {
	if _, err := os.Stat(filepath.Join(path, "gtk-4.0")); err == nil {
		return "GTK4"
	}
	if _, err := os.Stat(filepath.Join(path, "gtk-3.0")); err == nil {
		return "GTK3"
	}
	if _, err := os.Stat(filepath.Join(path, "gtk-2.0")); err == nil {
		return "GTK2"
	}
	if _, err := os.Stat(filepath.Join(path, "gnome-shell")); err == nil {
		return "Gnome Shell"
	}
	if _, err := os.Stat(filepath.Join(path, "xfwm4")); err == nil {
		return "XFWM4"
	}
	if _, err := os.Stat(filepath.Join(path, "openbox-3")); err == nil {
		return "Openbox"
	}
	if _, err := os.Stat(filepath.Join(path, "metadata.desktop")); err == nil {
		return "SDDM"
	}
	return ""
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
	var merged []ProtectedAsset
	seen := make(map[string]int) // Name+Type -> index in merged
	for _, a := range assets {
		if a.Name == "" || a.Name == "unknown" { continue }
		key := a.Name + ":" + a.Type
		if idx, ok := seen[key]; ok {
			// Asset already exists. Append source if not already present.
			if !strings.Contains(merged[idx].Source, a.Source) {
				if merged[idx].Source == "" {
					merged[idx].Source = a.Source
				} else {
					merged[idx].Source += ", " + a.Source
				}
			}
			// Keep highest priority
			if a.Priority < merged[idx].Priority {
				merged[idx].Priority = a.Priority
			}
		} else {
			seen[key] = len(merged)
			merged = append(merged, a)
		}
	}
	return merged
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

func ParseSDDMConfig(logChan chan string) string {
	// SDDM may have config in /etc/sddm.conf or /etc/sddm.conf.d/*.conf
	paths := []string{"/host/etc/sddm.conf"}
	
	files, _ := os.ReadDir("/host/etc/sddm.conf.d")
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".conf") {
			paths = append(paths, filepath.Join("/host/etc/sddm.conf.d", f.Name()))
		}
	}

	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Current=") || strings.HasPrefix(line, "Theme=") {
				parts := strings.Split(line, "=")
				if len(parts) >= 2 {
					theme := strings.TrimSpace(parts[1])
					if theme != "" {
						return theme
					}
				}
			}
		}
	}
	return ""
}

func ParseVSCodeConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, _ := os.ReadFile(path)
	if len(content) == 0 { return assets }
	
	re := regexp.MustCompile(`"editor.fontFamily"\s*:\s*"([^"]+)"`)
	match := re.FindStringSubmatch(string(content))
	if len(match) > 1 {
		for _, f := range strings.Split(match[1], ",") {
			f = strings.Trim(strings.TrimSpace(f), "\"'")
			if f != "" {
				assets = append(assets, ProtectedAsset{Name: f, Type: "font", Source: "VSCode", Priority: 2})
			}
		}
	}
	return assets
}

func ParseZedConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, _ := os.ReadFile(path)
	if len(content) == 0 { return assets }
	
	fields := []string{"ui_font_family", "buffer_font_family", "terminal.font_family"}
	for _, field := range fields {
		re := regexp.MustCompile(fmt.Sprintf(`"%s"\s*:\s*"([^"]+)"`, field))
		match := re.FindStringSubmatch(string(content))
		if len(match) > 1 {
			assets = append(assets, ProtectedAsset{Name: match[1], Type: "font", Source: "Zed", Priority: 2})
		}
	}
	return assets
}

func ParseFirefoxConfig() []ProtectedAsset {
	var assets []ProtectedAsset
	matches, _ := filepath.Glob("/host/home/nui/.mozilla/firefox/*/prefs.js")
	for _, p := range matches {
		content, _ := os.ReadFile(p)
		re := regexp.MustCompile(`user_pref\("font\.name\.[^"\)]+",\s*"([^"]+)"\)`)
		found := re.FindAllStringSubmatch(string(content), -1)
		for _, m := range found {
			assets = append(assets, ProtectedAsset{Name: m[1], Type: "font", Source: "Firefox", Priority: 2})
		}
	}
	return assets
}

func ParseChromiumConfig() []ProtectedAsset {
	var assets []ProtectedAsset
	paths := []string{
		"/host/home/nui/.config/chromium/Default/Preferences",
		"/host/home/nui/.config/google-chrome/Default/Preferences",
		"/host/home/nui/.config/BraveSoftware/Brave-Browser/Default/Preferences",
	}
	for _, p := range paths {
		content, _ := os.ReadFile(p)
		if len(content) == 0 { continue }
		re := regexp.MustCompile(`"fonts":\{"standard":\{"[^"]+":"([^"]+)"\}`)
		match := re.FindStringSubmatch(string(content))
		if len(match) > 1 {
			assets = append(assets, ProtectedAsset{Name: match[1], Type: "font", Source: "Chromium", Priority: 2})
		}
	}
	return assets
}

func ParseKDEConfig(path string) []ProtectedAsset {
	var assets []ProtectedAsset
	content, _ := os.ReadFile(path)
	if len(content) == 0 { return assets }
	
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "font=") || strings.HasPrefix(line, "fixed=") {
			parts := strings.Split(line, "=")
			if len(parts) >= 2 {
				fontInfo := strings.Split(parts[1], ",")
				if len(fontInfo) > 0 {
					assets = append(assets, ProtectedAsset{Name: fontInfo[0], Type: "font", Source: "KDE", Priority: 1})
				}
			}
		}
	}
	return assets
}

