# PruneDash: System Maintenance Dashboard
A lightweight, containerized, and intelligently "safe" system cleaning tool for Linux power users.

---

## 🚀 1. Overview
PruneDash is a modern maintenance dashboard designed to keep Linux systems (initially **CachyOS/Arch**) lean and performant. It evolves the traditional "blind" bash script philosophy into an intelligent, safety-first orchestration that prunes system bloat while protecting active "Rice" and configurations.

**Core Philosophy:** ⚡ *Spin up. Clean. Spin down.* ⚡

---

## 🛠️ 2. Core Features

### 🔍 Intelligent Storage Audit
- **Deep Scan**: Analyzes `pacman` levels, `systemd` journals, user cache (thumbnails, shaders), and AUR build fragments.
- **Config-Aware Protection**: Parses `hyprland.conf`, `niri/config.kdl`, and GTK settings to identify currently used themes, icons, and fonts.
- **Visual Status**: Displays "Active" assets with a [Locked 🔒] status in the UI to prevent accidental deletion of your desktop "Rice."

### 🧹 The Cleanup Engine
- **Package Management**:
    - Keep only $N$ versions of installed packages.
    - Purge uninstalled package cache.
- **Journal Vacuuming**: Hard cap on system logs to prevent runaway storage consumption.
- **Cache Purge**: Targeted clearing of `~/.cache/` (thumbnails, mesa_shader_cache) and AUR clones.
- **Theme Pruning**: One-click removal of unused fonts, themes, and icon sets.

### 🛡️ Safety & Undo System (The "Prune Bin")
- **Non-Destructive Cleanup**: Instead of `direct deletion`, files are staged in `/home/nui/.prune/bin/`.
- **Universal Restore**: If the system feels unstable, a universal "Undo" button restores the last prune session from the metadata map.
- **Dry Run Mode [Dev Only]**: Backend-only flag to simulate operations without affecting the filesystem.

### 📊 Premium UI Experience
- **Real-Time progress**: HTMX or WebSockets powered logs streaming bash output to a terminal-style component.
- **Rich Visuals**: Vibrant HSL-tailored color palettes with dark/light mode support.
- **Interactive Metrics**: Before/After delta charts showing exactly how much space was reclaimed.

---

## 🏗️ 3. Technical Stack

| Component | Technology | Rationale |
| :--- | :--- | :--- |
| **Backend** | **Go (Golang)** | Tiny binary footprint, high performance for file operations. |
| **Frontend** | **HTMX + Tailwind CSS** | Hypermedia-driven UI with premium styling and zero JS overhead. |
| **Logic** | **Bash + Systemd** | For direct interaction with system-specific tools (paccache, journalctl). |
| **Deployment** | **Docker (Privileged)** | Isolation while maintaining the ability to clean the host via nsenter. |

---

## 🗺️ 4. Development Roadmap

### Phase 1: The Foundation [DONE]
- [x] Initialize Go backend with Fiber.
- [x] Scaffold HTMX-powered "Premium" dashboard.
- [x] Docker Compose orchestration with basic networking.

### Phase 2: System Audit Integration [DONE]
- [x] Implement Go logic to fetch real data (Pacman, Journals, Caches).
- [x] Configure Docker volume mounts for host system visibility.
- [x] Implement real-time audit via HTMX.

### Phase 3: The "Prune Bin" & Safety [DONE]
- [x] Implement the staging directory logic (Move instead of Delete).
- [x] Build the "Execute Prune" API endpoint.
- [x] Create the "Undo" universal restore mechanism.
- [x] UI: Add "Prune Now" and "Undo" controls.
- [x] UI: Implement "Prunable Storage" vs "Protected Assets" breakdown for complete transparency.

### Phase 4: Configuration Intel (The "Rice" Protector) [DONE]
- [x] Build the Config Parser (Hyprland, Niri, GTK).
- [x] Identify active fonts, themes, and icons to mark as [Locked 🔒].
- [x] Implement selective pruning with category-based size summaries.

### Phase 5: Expansion & Polish [/]
- [x] Refactor to asynchronous Parallel System Audit.
- [x] Implement per-scan SSE logging for real-time console feedback.
- [x] Refine UI with single-column layout and improved Regex font language detection.
- [ ] Add support for Fedora (DNF) and Ubuntu/Debian (APT) caching logic.
- [ ] Refine micro-animations and HSL transitions for a premium feel.

---

## 📦 5. Deployment Recommendation
Run via Docker to keep the tool itself from polluting your system state:

```bash
docker compose up -d
```
Accessible at `http://localhost:3333` by default.
