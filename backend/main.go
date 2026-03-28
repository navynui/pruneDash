package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"pruneDash/system"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/template/html/v2"
)

var (
	LastScanResult system.ScanResult
	DryRunMode     = false

	// Per-scan channels to avoid races
	ActiveScans     = make(map[string]chan system.ScanResult)
	ActiveLogs      = make(map[string]chan string)
	ScansMu         sync.Mutex
)

func main() {
	// Initialize standard Go html template engine
	engine := html.New("./templates", ".html")

	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Serve static files (if any)
	app.Static("/static", "./static")

	// Main Dashboard Route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{
			"Title": "PruneDash",
		})
	})

	// API: Health Check
	app.Get("/api/health", func(c *fiber.Ctx) error {
		return c.SendString(`
			<span class="h-2 w-2 rounded-full bg-green-500 shadow-[0_0_10px_rgba(34,197,94,0.5)]"></span>
			<span class="text-xs font-bold text-slate-300 uppercase tracking-widest">System Online</span>
		`)
	})

	// API: Scan Logs (SSE)
	app.Get("/api/scan/logs", adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scanID := r.URL.Query().Get("id")
		if scanID == "" {
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Content-Encoding", "none")

		ScansMu.Lock()
		resChan, resOk := ActiveScans[scanID]
		logChan, logOk := ActiveLogs[scanID]
		ScansMu.Unlock()

		if !resOk || !logOk {
			return
		}

		// Cleanup channels after we're done
		defer func() {
			ScansMu.Lock()
			delete(ActiveScans, scanID)
			delete(ActiveLogs, scanID)
			ScansMu.Unlock()
		}()

		for {
			select {
			case logMsg := <-logChan:
				fmt.Fprintf(w, "event: log\ndata: <div class='text-blue-400 font-mono text-[10px] py-0.5 border-b border-white/5 opacity-80 animate-in fade-in slide-in-from-left-1'>%s</div>\n\n", logMsg)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case res := <-resChan:
				// Drain any remaining logs
				for {
					select {
					case logMsg := <-logChan:
						fmt.Fprintf(w, "event: log\ndata: <div class='text-blue-400 font-mono text-[10px] py-0.5 border-b border-white/5 opacity-80 animate-in fade-in slide-in-from-left-1'>%s</div>\n\n", logMsg)
						if f, ok := w.(http.Flusher); ok {
							f.Flush()
						}
					default:
						goto sendResult
					}
				}
			sendResult:
				LastScanResult = res
				html := formatScanResultsHTML(res)
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", strings.ReplaceAll(html, "\n", ""))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				return
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Second):
				// Heartbeat
				fmt.Fprintf(w, ": heartbeat\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}))



	// API: Scan (Async trigger)
	app.Post("/api/scan", func(c *fiber.Ctx) error {
		scanID := fmt.Sprintf("%d", time.Now().UnixNano())
		
		resChan := make(chan system.ScanResult, 1)
		logChan := make(chan string, 100)

		ScansMu.Lock()
		ActiveScans[scanID] = resChan
		ActiveLogs[scanID] = logChan
		ScansMu.Unlock()

		// Start scan in background
		go func() {
			res := system.PerformAsyncScan(logChan)
			resChan <- res
		}()

		// Return initial Loading UI with SSE console targeting the scanID
		return c.SendString(fmt.Sprintf(`
			<div class="mt-4 p-4 rounded-xl bg-slate-900/50 border border-brand-500/20 animate-in fade-in slide-in-from-top-4 duration-500">
				
				<!-- Activity Console (SSE Listener) -->
				<div class="mb-6 bg-black/40 rounded-xl border border-white/5 p-3 overflow-hidden shadow-inner">
					<div class="flex items-center justify-between mb-2">
						<span class="text-[9px] font-bold uppercase tracking-widest text-slate-500">Live Activity Monitor</span>
						<span class="flex h-1.5 w-1.5 rounded-full bg-brand-500 animate-pulse"></span>
					</div>
					<div hx-ext="sse" sse-connect="/api/scan/logs?id=%s" class="flex flex-col-reverse">
						<div id="activity-log" sse-swap="log" hx-swap="beforeend" 
							 class="h-48 overflow-y-auto scrollbar-hide flex flex-col-reverse italic">
							<div class="text-[10px] text-slate-500 opacity-50">Handshaking with system probes...</div>
						</div>
						<!-- Final Result Listener -->
						<div sse-swap="result" hx-swap="innerHTML" hx-target="#results-skeleton" class="hidden"></div>
					</div>
				</div>

				<!-- Intel Skeleton (to be swapped OOB) -->
				<div id="intel-section" class="mb-6 bg-slate-800/20 rounded-2xl p-5 border border-white/5 border-dashed animate-pulse text-center">
					<p class="text-[10px] text-slate-500 uppercase tracking-widest font-bold">Waiting for System Intel...</p>
				</div>
                
                <div id="results-skeleton" class="space-y-4 animate-pulse">
                    <div class="h-4 bg-white/5 rounded w-3/4"></div>
                    <div class="h-4 bg-white/5 rounded w-1/2"></div>
                </div>
			</div>
		`, scanID))
	})

	// API: Prune (Execute Cleanup)
	app.Post("/api/prune", func(c *fiber.Ctx) error {
		// Get all selected asset identifiers (Name:Type)
		var assetIds []string
		c.Context().PostArgs().VisitAll(func(key, value []byte) {
			if string(key) == "assets" {
				assetIds = append(assetIds, string(value))
			}
		})

		if len(assetIds) == 0 {
			// Check if any assets were even available
			if len(LastScanResult.PrunableAssets) > 0 {
				return c.SendString("<div class='p-4 bg-amber-500/10 border border-amber-500/20 text-amber-500 rounded-xl'>No assets selected for cleanup.</div>")
			}
		}

		// 1. Move to Prune Bin
		moved, size, err := system.MoveToPruneBin(assetIds, LastScanResult.PrunableAssets, DryRunMode)
		if err != nil {
			return c.SendString(fmt.Sprintf("<div class='text-red-500'>Error moving to bin: %v</div>", err))
		}

		// 2. Clear Caches & Logs
		purgeLogs, _ := system.PurgeCaches()
		system.ClearUserCache()

		prefix := ""
		if DryRunMode {
			prefix = "[DRY RUN] "
		}

		return c.SendString(fmt.Sprintf(`
			<div class="mt-4 p-6 rounded-3xl bg-green-500/10 border border-green-500/20 text-green-100 animate-in fade-in zoom-in duration-500">
				<div class="flex items-center space-x-4 mb-4">
					<div class="w-12 h-12 rounded-2xl bg-green-500/20 flex items-center justify-center text-2xl font-bold">✨</div>
					<div>
						<h3 class="font-bold text-lg leading-tight">%sCleanup Successful</h3>
						<p class="text-xs text-green-400 font-semibold uppercase tracking-wider">%d Assets Staged / %s reclaimed</p>
					</div>
				</div>
				<ul class="text-[11px] space-y-1.5 opacity-80 font-medium px-2">
					%s
					<li>User cache cleared successfully.</li>
				</ul>
				
				<div class="mt-6 flex items-center space-x-3">
					<button hx-post="/api/restore" hx-target="#prune-status" hx-swap="outerHTML" 
							class="text-[11px] font-bold bg-white/10 hover:bg-white/20 px-4 py-2 rounded-xl transition-all border border-white/10">
						Undo Last Action
					</button>
                    <button onclick="window.location.reload()" class="text-[11px] font-bold text-slate-500 hover:text-white px-2">Dismiss</button>
				</div>
			</div>
		`, prefix, moved, system.FormatSize(size), formatLogs(purgeLogs)))
	})

	// API: Restore (Undo)
	app.Post("/api/restore", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html")
		restored, err := system.RestoreFromBin(DryRunMode)
		if err != nil {
			return c.SendString(fmt.Sprintf(`
				<div class="p-4 rounded-xl bg-slate-900 border border-amber-500/20 text-amber-500 animate-in fade-in slide-in-from-top-4">
					<div class="flex items-center space-x-3">
						<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>
						<span class="text-xs font-bold uppercase tracking-wider">%s</span>
					</div>
				</div>
			`, err.Error()))
		}

		statusPrefix := ""
		if DryRunMode {
			statusPrefix = "[DRY RUN] "
		}

		return c.SendString(fmt.Sprintf(`
			<div class="p-4 rounded-xl bg-slate-900 border border-green-500/20 text-green-500 animate-in fade-in slide-in-from-top-4">
				<div class="flex items-center space-x-3">
					<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
					<span class="text-xs font-bold uppercase tracking-wider">%sRestore Successful: %d assets returned to original paths</span>
				</div>
			</div>
		`, statusPrefix, restored))
	})

	log.Fatal(app.Listen(":3333"))
}

func formatScanResultsHTML(res system.ScanResult) string {
	// Group assets by category
	themes := filterAssets(res.PrunableAssets, "theme")
	icons := filterAssets(res.PrunableAssets, "icon")
	fonts := filterAssets(res.PrunableAssets, "font")

	totalSizeStr := system.FormatSize(res.PrunableSize)
	disabledAttr := ""
	if res.PrunableSize == 0 {
		disabledAttr = "disabled"
	}

	return fmt.Sprintf(`
		<!-- Swap Env Intel -->
		<div id="active-wm" hx-swap-oob="innerHTML">%s</div>
		<div id="total-protected" hx-swap-oob="innerHTML">%d</div>
		<div id="total-reclaimable" hx-swap-oob="innerHTML">%s</div>

		<div id="intel-section" hx-swap-oob="innerHTML">
			<div class="grid grid-cols-1 md:grid-cols-3 gap-3 mb-6">
                <!-- Environment Insight -->
				<div class="bg-brand-500/10 border border-brand-500/20 p-4 rounded-2xl">
					<span class="text-[9px] font-bold text-brand-400 uppercase tracking-widest block mb-1">Window Manager</span>
					<span class="font-bold text-slate-100">%s</span>
				</div>
                <div class="bg-slate-800/20 border border-white/5 p-4 rounded-2xl">
					<span class="text-[9px] font-bold text-slate-500 uppercase tracking-widest block mb-1">Display Manager</span>
					<span class="font-bold text-slate-100">%s</span>
				</div>
                <div class="bg-slate-800/20 border border-white/5 p-4 rounded-2xl">
					<span class="text-[9px] font-bold text-slate-500 uppercase tracking-widest block mb-1">Boot Loader</span>
					<span class="font-bold text-slate-100">%s</span>
				</div>
			</div>
		</div>

		<div id="results-skeleton" hx-swap-oob="outerHTML">
            <div id="prune-results-container" class="animate-in fade-in slide-in-from-bottom-4 duration-700">
                <form id="prune-form" hx-post="/api/prune" hx-target="#prune-status" hx-indicator="#prune-spinner">
                    
                    <!-- Selection Summary -->
                    <div class="bg-slate-800/50 border border-brand-500/30 rounded-3xl p-6 mb-6 shadow-2xl overflow-hidden relative group">
                         <div class="absolute top-0 right-0 p-8 opacity-10 group-hover:opacity-20 transition-opacity">
                             <div class="text-7xl font-black italic select-none">RECLAIM</div>
                         </div>
                         <div class="relative z-10">
                            <h3 id="current-selection-size" class="text-5xl font-black tabular-nums transition-all">%s</h3>
                            <p class="text-xs font-bold text-brand-400 uppercase tracking-[0.3em]">Total Prunable Assets</p>
                         </div>
                    </div>

                    <!-- Protected & Prunable Toggle Tabs? No, just list both -->
                    <div class="space-y-6">
                        <!-- Prunable Section -->
                        <div class="space-y-4">
                            <h4 class="text-[10px] font-bold text-slate-500 uppercase tracking-widest px-1">Prunable Items</h4>
                            %s
                            %s
                        </div>

                        <!-- Protected Section -->
                        <div class="pt-4 border-t border-white/5">
                            <h4 class="text-[10px] font-bold text-brand-400 uppercase tracking-widest px-1 mb-4 flex items-center">
                                <svg class="w-3 h-3 mr-2 text-brand-400" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M5 9V7a5 5 0 0110 0v2a2 2 0 012 2v5a2 2 0 01-2 2H5a2 2 0 01-2-2v-5a2 2 0 012-2zm8-2v2H7V7a3 3 0 016 0z" clip-rule="evenodd"/></path></svg>
                                Currently Active (Protected Assets)
                            </h4>
                            <div class="bg-slate-900/40 rounded-2xl border border-white/5 p-4">
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-2">
                                    %s
                                </div>
                            </div>
                        </div>
                    </div>

                    <div id="prune-status" class="mt-8">
                         <button type="submit" %s class="w-full bg-brand-500 hover:bg-brand-600 disabled:opacity-50 disabled:cursor-not-allowed text-white font-black py-5 rounded-2xl transition-all shadow-xl shadow-brand-500/30 flex items-center justify-center space-x-3 group">
                            <span>Execute Cleanup</span>
                            <svg id="prune-spinner" class="htmx-indicator animate-spin h-5 w-5 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                            </svg>
                         </button>
                    </div>
                </form>
            </div>
		</div>
	`, 
		res.Env.WM, len(res.Assets), totalSizeStr, 
		res.Env.WM, res.Env.DM, res.Env.Bootloader, 
		totalSizeStr, 
		formatCategorySections(themes, icons, fonts),
		formatOtherSizes(res),
		formatProtectedList(res.Assets),
		disabledAttr)
}

func filterAssets(assets []system.ProtectedAsset, aType string) []system.ProtectedAsset {
	var filtered []system.ProtectedAsset
	for _, a := range assets {
		if a.Type == aType {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func formatCategorySections(themes, icons, fonts []system.ProtectedAsset) string {
	res := ""
	if len(themes) > 0 {
		res += renderCategory("Themes", themes, "🎨")
	}
	if len(icons) > 0 {
		res += renderCategory("Icons", icons, "🖼️")
	}
	if len(fonts) > 0 {
		res += renderCategory("Fonts", fonts, "🔤")
	}
	return res
}

func renderCategory(title string, assets []system.ProtectedAsset, emoji string) string {
	var totalSize int64
	for _, a := range assets {
		totalSize += a.Size
	}

	rows := ""
	for _, a := range assets {
		// Identifier format: Name:Type
		id := fmt.Sprintf("%s:%s", a.Name, a.Type)
		rows += fmt.Sprintf(`
			<div class="flex items-center justify-between p-3 border-b border-white/5 hover:bg-white/5 transition group">
				<div class="flex items-center space-x-3">
					<input type="checkbox" name="assets" value="%s" checked class="w-4 h-4 rounded bg-slate-900 border-white/10 text-brand-500 focus:ring-brand-500">
					<span class="text-xs font-medium text-slate-300">%s</span>
				</div>
				<span class="text-[10px] font-mono text-slate-500">%s</span>
			</div>
		`, id, a.Name, a.FormattedSize)
	}

	return fmt.Sprintf(`
		<div class="bg-slate-800/30 rounded-2xl border border-white/5 overflow-hidden">
			<div class="flex items-center justify-between bg-white/5 px-4 py-3">
				<div class="flex items-center space-x-2">
					<span class="text-sm">%s</span>
					<span class="text-xs font-bold uppercase tracking-widest text-slate-400">%s</span>
				</div>
				<span class="text-[10px] font-bold text-brand-400 opacity-60">%s</span>
			</div>
			<div class="max-h-48 overflow-y-auto custom-scrollbar">
				%s
			</div>
		</div>
	`, emoji, title, system.FormatSize(totalSize), rows)
}

func formatLogs(logs []string) string {
	res := ""
	for _, log := range logs {
		res += fmt.Sprintf("<li>%s</li>", log)
	}
	return res
}

func formatOtherSizes(res system.ScanResult) string {
	items := []struct {
		name string
		size int64
		icon string
	}{
		{"Package Cache", res.PacmanMetrics.Reclaim, "📦"},
		{"System Journals", res.JournalMetrics.Reclaim, "📝"},
		{"User Cache", res.UserCacheSize, "📂"},
	}

	html := ""
	for _, item := range items {
		if item.size > 0 {
			html += fmt.Sprintf(`
				<div class="flex items-center justify-between p-4 bg-slate-800/30 rounded-2xl border border-white/5">
					<div class="flex items-center space-x-3">
						<span class="text-xl">%s</span>
						<span class="text-xs font-bold text-slate-300 uppercase tracking-widest">%s</span>
					</div>
					<span class="text-xs font-mono text-brand-400 font-bold">%s</span>
				</div>
			`, item.icon, item.name, system.FormatSize(item.size))
		}
	}
	return html
}

func formatProtectedList(assets []system.ProtectedAsset) string {
	if len(assets) == 0 {
		return "<div class='text-[10px] text-slate-600 italic px-2'>No active themes detected.</div>"
	}

	// Categorize
	categorized := make(map[string][]system.ProtectedAsset)
	for _, a := range assets {
		categorized[a.Type] = append(categorized[a.Type], a)
	}

	html := ""
	order := []string{"theme", "icon", "font", "cursor"}
	names := map[string]string{"theme": "Themes", "icon": "Icons", "font": "Fonts", "cursor": "Cursors"}
	emojis := map[string]string{"theme": "🎨", "icon": "🖼️", "font": "🔤", "cursor": "🖱️"}

	for _, t := range order {
		list := categorized[t]
		if len(list) == 0 {
			continue
		}
		
		html += fmt.Sprintf(`
			<div class="space-y-1">
				<p class="text-[9px] font-bold text-slate-500 uppercase flex items-center mb-1">
					<span class="mr-1">%s</span> %s
				</p>
				<div class="grid grid-cols-1 gap-1">
		`, emojis[t], names[t])
		
		for _, a := range list {
			html += fmt.Sprintf(`
				<div class="flex items-center space-x-2 text-[10px] px-2 py-1 rounded-lg hover:bg-white/5 transition group">
					<span class="font-medium text-slate-300 truncate">%s</span>
					<span class="text-[8px] text-slate-600 opacity-0 group-hover:opacity-100 transition-opacity truncate">%s</span>
				</div>
			`, a.Name, a.Source)
		}
		
		html += `</div></div>`
	}
	
	return html
}
