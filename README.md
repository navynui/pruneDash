# PruneDash 🧹
A premium, lightweight, and intelligently "safe" system maintenance dashboard for Linux power users.

PruneDash is designed to help you reclaim storage with surgical precision by identifying unnecessary caches, logs, and unused theme assets while protecting your active desktop configuration (Hyprland, Niri, GTK).

---

## ✨ Features
- **🔍 Intelligent Storage Audit**: Deep scan of `pacman` caches, `systemd` journals, and user caches, distinctly separating **Prunable Storage** from **Protected Assets** (like kept package versions for stability).
- **🛡️ Safe Staging (Self-Managing Prune Bin)**: Files are staged in a temporary area instead of being deleted, with a intelligent UI that only appears when items are staged, ensuring a clean dashboard at all times. Supports instant "Undo."
- **🔒 Protected Assets**: Automatically identifies and locks active themes/icons/fonts by parsing configs for Hyprland, Niri, and GTK. 
- **⚡ Asynchronous Parallel Scanning**: Ultra-fast audit engine leveraging Goroutines to complete multi-GB system scans in milliseconds.
- **📊 Real-Time Feedback**: Premium, HTMX-powered dashboard with live SSE (Server-Sent Events) log streaming and a responsive single-column layout.
- **🐳 Containerized**: Spin up in seconds using Docker without polluting your host system.

---

## 🚀 Quick Start
To launch the PruneDash dashboard:

```bash
docker compose up -d --build
```
Once the container is running, access the dashboard at:
👉 **[http://localhost:3333](http://localhost:3333)**

---

## 🛠️ Tech Stack
- **Backend**: Go (Fiber)
- **Frontend**: Go Templates + HTMX + Tailwind CSS
- **Orchestration**: Docker

---

## 🏗️ Technical Plan
The detailed project specification and development roadmap can be found in the [pruneDash.md](pruneDash.md) file (ignored by git).
