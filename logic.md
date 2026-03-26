# Theme Detection Logic for PruneDash

## Overview
This document outlines the logic for detecting which themes, icons, fonts, and cursors the current user is actively using on their Linux desktop, to mark them as "Protected Assets" and prevent accidental deletion during cleanup.

The detection follows a **prioritized approach**:
1. First detect what the user actually uses (WM, DM, bootloader)
2. Then detect apps the user frequently uses (from keybinds + recent files)
3. Finally parse themes/configs only for those relevant apps

---

## 1. Phase 0: Detect User's Core Environment

Before parsing any theme configs, first detect what infrastructure the user uses:

### 1.1 Window Manager / Desktop Environment
| WM/DE | Detection Method | Config File |
|-------|------------------|-------------|
| Hyprland | Check for `~/.config/hypr/hyprland.conf` | `hyprland.conf` |
| Niri | Check for `~/.config/niri/config.kdl` | `config.kdl` |
| Sway | Check for `~/.config/sway/config` | `config` |
| Awesome | Check for `~/.config/awesome/rc.lua` | `rc.lua` |
| Qtile | Check for `~/.config/qtile/config.py` | `config.py` |
| GNOME | Check `DESKTOP_SESSION` env or `~/.config GNOME` | gsettings |
| KDE | Check for `~/.config/kwinrc`, `~/.config/kdeglobals` | plasma |

### 1.2 Display Manager (Login Screen)
| DM | Detection Method | Config Locations |
|----|-------------------|------------------|
| SDDM | Check `/etc/sddm.conf.d/` or `~/.config/sddm/` | `/usr/share/sddm/themes/`, `~/.local/share/sddm/themes/` |
| GDM | Check for `/etc/gdm3/` or `/etc/gdm/` | `/usr/share/gnome-shell/themes/`, `/usr/share/gdm/themes/` |
| LightDM | Check `/etc/lightdm/` | `/usr/share/themes/` (lightdm-gtk-greeter) |
| Ly | Check `~/.config/ly/` | `~/.config/ly/` |

**Detection Logic**:
```bash
# Detect active display manager
systemctl list-units --type=service | grep -E 'sddm|gdm|lightdm|ly'
# Or check symlink
ls -la /etc/systemd/system/display-manager.service
```

### 1.3 Boot Loader
| Bootloader | Detection Method | Theme Locations |
|-------------|-------------------|-----------------|
| GRUB | Check `/boot/grub/grub.cfg` | `/boot/grub/themes/`, `/usr/share/grub/themes/` |
| rEFInd | Check `/boot/EFI/*/refind.conf` or `/efi/EFI/*/refind.conf` | `/boot/EFI/*/themes/`, `/efi/EFI/*/themes/` |
| systemd-boot | Check `/boot/loader/entries/` | No theme support typically |
| Clover | Check `/boot/EFI/CLOVER/` | `/boot/EFI/CLOVER/themes/` |

**Detection Logic**:
```bash
# GRUB
ls /boot/grub/grub.cfg 2>/dev/null && echo "grub"
# rEFInd
ls /boot/EFI/*/refind.conf 2>/dev/null || ls /efi/EFI/*/refind.conf 2>/dev/null && echo "refind"
```

---

## 2. Phase 1: Detect User's Frequently Used Apps

Instead of parsing every app config, detect what the user actually uses:

### 2.1 From WM Keybinds
Parse WM config to extract app executables from keybinds:
```bash
# Hyprland example binds
bind = SUPER, Return, exec, alacritty
bind = SUPER, D, exec, rofi -show drun
bind = SUPER, B, exec, firefox
bind = SUPER, E, exec, thunar
```

Extract all `exec, <app>` patterns to build a usage list.

### 2.2 From Recent Files
- **Recent files**: `~/.local/share/recently-used.xbel` (GTK)
- **Recent docs**: `~/.local/share/documents.lrf` (KDE)
- **Desktop entries**: Parse `.desktop` files in `~/.config/autostart/` for launched apps

### 2.3 From Running Processes
Check for commonly used apps currently running or in autostart:
```bash
# Common app categories to check
terminals: alacritty, kitty, foot, gnome-terminal, konsole
file-managers: thunar, nautilus, dolphin, pcmanfm, ranger
browsers: firefox, chrome, chromium, librewolf
launchers: rofi, wofi, dmenu, bemenu, j4-dmenu-desktop
status-bars: waybar, polybar, i3blocks, yabar
```

### 2.4 Prioritized App List
Build a deduplicated list sorted by likelihood:
```
Priority 1 (WM core): terminal, file-manager, browser, launcher, status-bar
Priority 2 (frequent): text-editor, image-viewer, music-player, video-player
Priority 3 (occasional): office, email, chat, development-tools
```

---

## 3. Phase 2: Parse Themes for Detected Apps

Now parse only the configs for apps detected in Phase 1:

### 3.1 App-Specific Theme Configs

| App Category | Config File(s) | Theme Keys |
|--------------|----------------|------------|
| Terminal (Alacritty) | `~/.config/alacritty/alacritty.toml` | `window.opacity`, colors |
| Terminal (Kitty) | `~/.config/kitty/kitty.conf` | `include`, `background_opacity` |
| Terminal (Foot) | `~/.config/foot/foot.ini` | `theme` |
| File Manager (Thunar) | `~/.config/Thunar/uca.xml` | theming via GTK |
| File Manager (Dolphin) | `~/.config/dolphinrc` | `KDE-Theme` |
| Launcher (Rofi) | `~/.config/rofi/config.rasi` | `theme` |
| Launcher (Wofi) | `~/.config/wofi/config` | `style` |
| Status Bar (Waybar) | `~/.config/waybar/config` | `style`, css |
| Status Bar (Polybar) | `~/.config/polybar/config.ini` | `theme` |
| Image Viewer (imv) | `~/.config/imv/config` | `style` |
| Video Player (mpv) | `~/.config/mpv/mpv.conf` | `osc`, `sls` |

### 3.2 GTK/Qt Application Themes
These apps use the system GTK/Qt theme settings:
- GTK3: `~/.config/gtk-3.0/settings.ini` → `gtk-theme`, `gtk-icon-theme`, `gtk-font-name`, `gtk-cursor-theme-name`
- GTK4: `~/.config/gtk-4.0/settings.ini` → same keys
- Qt5: `~/.config/Trolltech.conf` or `~/.config/qt5ct/qt5ct.conf`
- Qt6: `~/.config/qt6ct/qt6ct.conf`

---

## 4. Phase 3: Global Theme Assets

Parse system-wide theme configs (applies to all GTK/Qt apps):

### 4.1 GTK Themes
- **Config**: `gtk-theme` in GTK3/4 settings
- **Locations**: `/usr/share/themes/`, `~/.local/share/themes/`, `~/.themes/`

### 4.2 Icon Themes
- **Config**: `gtk-icon-theme` in GTK3/4, plus WM-specific
- **Locations**: `/usr/share/icons/`, `~/.local/share/icons/`, `~/.icons/`

### 4.3 Font Families
- **Config**: GTK `gtk-font-name`, WM `font-family`, terminal configs
- **Locations**: `/usr/share/fonts/`, `~/.local/share/fonts/`, `~/.fonts/`

### 4.4 Cursor Themes
- **Config**: `gtk-cursor-theme-name`
- **Locations**: `/usr/share/icons/`, `~/.local/share/icons/`

---

## 5. Phase 4: DM/Bootloader Themes

### 5.1 SDDM Themes
- **Theme Config**: `/etc/sddm.conf.d/` or `~/.config/sddm.conf`
- **Current Theme**: `Theme=` in `[Theme]` section
- **Locations**: `/usr/share/sddm/themes/`, `~/.local/share/sddm/themes/`

### 5.2 GDM Themes
- **Theme Config**: `org.gnome.shell.looking-for` gsettings key
- **Shell Theme**: `org.gnome.desktop.interface.gtk-theme`
- **Locations**: `/usr/share/gnome-shell/themes/`, `/usr/share/themes/`

### 5.3 GRUB Themes
- **Config**: `/etc/default/grub` → `GRUB_THEME=`
- **Locations**: `/boot/grub/themes/`, `/usr/share/grub/themes/`

### 5.4 rEFInd Themes
- **Config**: `refind.conf` → `theme` directive
- **Locations**: `/boot/EFI/*/themes/`, `/efi/EFI/*/themes/`

---

## 6. Detection Logic Summary

```
┌─────────────────────────────────────────────────────────────┐
│ Phase 0: Detect Core Environment                            │
│  ├── Detect WM/DE (Hyprland, Niri, GNOME, KDE, etc.)        │
│  ├── Detect Display Manager (SDDM, GDM, LightDM, Ly)        │
│  └── Detect Boot Loader (GRUB, rEFInd, systemd-boot)       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Phase 1: Detect Frequently Used Apps                        │
│  ├── Parse WM keybinds → extract exec commands              │
│  ├── Check recent files → recent apps                      │
│  ├── Check autostart → .desktop files                       │
│  └── Build prioritized app list                            │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Phase 2: Parse App-Specific Themes                          │
│  ├── Terminal themes (Alacritty, Kitty, Foot)               │
│  ├── Launcher themes (Rofi, Wofi)                           │
│  ├── Status bar themes (Waybar, Polybar)                    │
│  └── File manager, image viewer, video player configs       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Phase 3: Parse Global Theme Assets                          │
│  ├── GTK themes + icon + fonts + cursors                    │
│  └── Qt themes (if Qt apps detected)                       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Phase 4: Parse DM/Bootloader Themes                         │
│  ├── SDDM/GDM/LightDM themes                                │
│  └── GRUB/rEFInd themes                                     │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ Output: Protected Assets Set                                │
│  └── Deduplicated list of theme/icon/font paths            │
└─────────────────────────────────────────────────────────────┘
```

---

## 7. Implementation Plan

### Step 0: Core Environment Detection (`backend/system/rice.go`)
- `DetectWM() string` → returns "hyprland", "niri", "sway", "gnome", etc.
- `DetectDM() string` → returns "sddm", "gdm", "lightdm", "ly"
- `DetectBootloader() string` → returns "grub", "refind", "systemd-boot"

### Step 1: App Detection (`backend/system/rice.go`)
- `GetWMKeybindApps(path string) []string` → extract exec commands
- `GetRecentApps() []string` → parse recently-used.xbel
- `GetAutostartApps() []string` → parse .desktop files
- `MergePrioritizedApps([]string...) []string` → deduplicate + sort

### Step 2: Config Parser Module
- `ParseHyprlandConfig(path string) map[string]string`
- `ParseNiriConfig(path string) map[string]string`
- `ParseGTKSettings(path string) map[string]string`
- `ParseAlacrittyConfig(path string) map[string]string`
- `ParseKittyConfig(path string) map[string]string`
- `ParseRofiConfig(path string) map[string]string`
- `ParseWaybarConfig(path string) map[string]string`
- `ParseSDDMConfig(path string) string`
- `ParseGRUBTheme(path string) string`
- `ParserEFIndConfig(path string) string`

### Step 3: Asset Resolver
- `ResolveThemePath(name string) string`
- `ResolveIconPath(name string) string`
- `ResolveFontPath(name string) string`
- `ResolveCursorPath(name string) string`
- `ResolveDMThemePath(name string) string`
- `ResolveBootloaderThemePath(name string) string`

### Step 4: Protected Assets API
- `GetCoreEnvironment() (WM, DM, Bootloader string)`
- `GetFrequentApps() []string`
- `GetProtectedAssets() ([]ProtectedAsset, error)`

---

## 8. Output Structure

```go
type CoreEnvironment struct {
    WM          string // "hyprland", "niri", "sway", "gnome", "kde"
    DM          string // "sddm", "gdm", "lightdm", "ly"
    Bootloader  string // "grub", "refind", "systemd-boot"
}

type FrequentApp struct {
    Name        string // "alacritty", "rofi", "firefox"
    Category    string // "terminal", "launcher", "browser"
    Source      string // "keybind", "recent", "autostart"
}

type ProtectedAsset struct {
    Name        string // e.g., "Adwaita-dark"
    Type        string // "theme" | "icon" | "font" | "cursor" | "dm" | "bootloader"
    Path        string // full path to asset
    Source      string // config file where detected
    Priority    int    // 1=core, 2=frequent-app, 3=global
}
```

---

## 9. Edge Cases & Notes

1. **Case Sensitivity**: Theme names may be case-insensitive; normalize to lowercase
2. **Symbolic Links**: Resolve symlinks when checking if asset exists
3. **Missing Configs**: Gracefully handle missing files (don't crash)
4. **Theme Variants**: "Theme-dark" should protect "Theme" base folder
5. **Variable Expansion**: Expand `$VAR`, `~`, `~/.config` before matching
6. **WM Fallbacks**: If WM not detected, assume GTK defaults (Adwaita)
7. **Multiple WMs**: User may have multiple WMs installed; use currently active one
8. **No DM**: For tty-only or minimal setups, skip DM theme detection

---

## 10. Tech Stack & Tools

### 10.1 Go Standard Library
For efficient file parsing and pattern matching:

| Package | Purpose |
|---------|---------|
| `os` / `io/ioutil` | File existence check, reading config files |
| `path/filepath` | Path manipulation, glob patterns |
| `regexp` | Key-value extraction from config files |
| `encoding/xml` | Parsing `.xbel` (recent files), `.rasi` (rofi) |
| `encoding/json` | Parsing `.toml` (alacritty), `.json` configs |
| `text/template` | Parsing bash-style configs with template placeholders |

### 10.2 External Libraries (Go)
| Library | Purpose |
|---------|---------|
| `github.com/BurntSushi/toml` | Parse Alacritty, Cargo, Go workspace configs |
| `github.com/mitchellh/go-homedir` | Expand `~` to actual home path |
| `github.com/mitchellh/mapstructure` | Map parsed config values to structs |

### 10.3 Shell Commands (via `os/exec`)
For system-level detection:

```bash
# Detect active display manager
systemctl list-units --type=service | grep -E 'sddm|gdm|lightdm|ly'
# Or: ls -la /etc/systemd/system/display-manager.service

# Detect active WM from session
loginctl show-session $(loginctl | grep $(whoami) | awk '{print $1}') -p Display

# List running processes for app detection
ps aux | grep -E 'alacritty|kitty|firefox|thunar'

# Get user environment variables
printenv | grep -E 'XDG_CURRENT_DESKTOP|DESKTOP_SESSION|WAYLAND_DISPLAY'
```

### 10.4 Parsing Libraries by Config Type

| Config Type | Recommended Parser |
|-------------|-------------------|
| `.ini` (GTK3/4, XDG) | `goconfig` or custom regex |
| `.toml` (Alacritty, Starship) | `BurntSushi/toml` |
| `.kdl` (Niri) | Custom parser or `tantivy` for KDL |
| `.rasi` (Rofi) | Custom regex (simple syntax) |
| `.json` (Waybar, VSCode) | `encoding/json` |
| `.xml` (Thunar, recent-xbel) | `encoding/xml` |
| `.conf` (Hyprland, systemd) | Custom regex parser |
| `.lua` (Awesome, nvim) | Custom parser or `gopher-lua` |
| `.yaml`/.yml (Ansible, docker) | `go-yaml` / `yaml.v3` |

### 10.5 Performance Optimization

1. **Parallel File Scanning**: Use goroutines for independent config reads
2. **Lazy Loading**: Only load configs when associated WM/DM/app is detected
3. **Caching**: In-memory cache with TTL for parsed configs (refresh every scan)
4. **Incremental Updates**: Store last known state, only re-parse changed files
5. **Bloom Filter**: Quick negative lookups for asset existence checks

### 10.6 Efficiency Patterns

```go
// Example: Parallel detection with early exit
func DetectCoreEnvironment() CoreEnvironment {
    wm, dm, bl := "", "", ""
    wg := sync.WaitGroup{}
    
    wg.Add(3)
    go func() { wm = DetectWM(); wg.Done() }()
    go func() { dm = DetectDM(); wg.Done() }()
    go func() { bl = DetectBootloader(); wg.Done() }()
    
    wg.Wait()
    return CoreEnvironment{WM: wm, DM: dm, Bootloader: bl}
}
```

### 10.7 File System Considerations
- Use `os.ReadDir` instead of `filepath.Walk` when only checking existence
- Set reasonable timeouts for network-adjacent paths (e.g., `/boot`, `/efi`)
- Handle permission errors gracefully; some configs may be root-only
- Watch for symlink loops when resolving paths

---

## 11. Future Extensions

- Add support for more WMs: Qtile, Awesome, bspwm
- Detect wallpaper images for protection
- Add user whitelist config for custom protected paths
- Cache parsed configs to avoid repeated disk reads
- Support for more DMs: LXDM, SDDM (KDE), entrance