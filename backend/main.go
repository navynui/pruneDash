package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
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
			w.WriteHeader(http.StatusNoContent)
			return
		}

		ScansMu.Lock()
		resChan, resOk := ActiveScans[scanID]
		logChan, logOk := ActiveLogs[scanID]
		ScansMu.Unlock()

		// Return 204 so the browser EventSource permanently stops retrying
		if !resOk || !logOk {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Content-Encoding", "none")

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
				// Send a lightweight done signal — client will fetch results via GET /api/scan/result
				fmt.Fprintf(w, "event: done\ndata: ok\n\n")
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


	// API: Scan Result (fetched by client after receiving 'done' SSE signal)
	app.Get("/api/scan/result", func(c *fiber.Ctx) error {
		return c.SendString(formatScanResultsHTML(LastScanResult))
	})

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

		// Return initial Loading UI with native EventSource (no htmx SSE extension)
		return c.SendString(fmt.Sprintf(`
			<div id="scan-console-wrapper" class="mt-4 animate-in fade-in slide-in-from-top-4 duration-500">

				<!-- Activity Console -->
				<div class="mb-4 bg-black/40 rounded-xl border border-white/5 overflow-hidden shadow-inner">
					<div class="flex items-center justify-between px-3 py-2 border-b border-white/5">
						<div class="flex items-center space-x-2">
							<span id="log-status-dot" class="flex h-1.5 w-1.5 rounded-full bg-brand-500 animate-pulse"></span>
							<span id="log-status-label" class="text-[9px] font-bold uppercase tracking-widest text-slate-500">Live Activity Monitor</span>
						</div>
						<button id="log-toggle-btn"
							onclick="(function(){
								var b = document.getElementById('activity-log-body');
								var btn = document.getElementById('log-toggle-btn');
								if (b.style.display === 'none') { b.style.display = ''; btn.textContent = 'Hide'; }
								else { b.style.display = 'none'; btn.textContent = 'View Logs'; }
							})()"
							class="text-[9px] font-bold text-brand-400 hover:text-brand-300 transition uppercase tracking-widest">Hide</button>
					</div>
					<div id="activity-log-body" class="p-3">
						<div id="activity-log" class="h-48 overflow-y-auto scrollbar-hide flex flex-col-reverse italic">
							<div class="text-[10px] text-slate-500 opacity-50">Handshaking with system probes...</div>
						</div>
					</div>
				</div>

				<!-- Intel Skeleton (updated via OOB from /api/scan/result) -->
				<div id="intel-section" class="mb-6 bg-slate-800/20 rounded-2xl p-5 border border-white/5 border-dashed animate-pulse text-center">
					<p class="text-[10px] text-slate-500 uppercase tracking-widest font-bold">Waiting for System Intel...</p>
				</div>
			</div>
			<script>
			(function() {
				var es = new EventSource('/api/scan/logs?id=%s');

				es.addEventListener('log', function(e) {
					var log = document.getElementById('activity-log');
					if (log) { log.insertAdjacentHTML('afterbegin', e.data); }
				});

				es.addEventListener('done', function(e) {
					es.close(); // Explicitly close — no reconnects ever

					// Update status indicator
					var dot = document.getElementById('log-status-dot');
					var label = document.getElementById('log-status-label');
					if (dot) { dot.className = 'flex h-1.5 w-1.5 rounded-full bg-green-500'; }
					if (label) { label.className = 'text-[9px] font-bold uppercase tracking-widest text-green-500'; label.textContent = 'Scan Complete'; }

					// Collapse log panel
					var body = document.getElementById('activity-log-body');
					var btn = document.getElementById('log-toggle-btn');
					if (body) body.style.display = 'none';
					if (btn) btn.textContent = 'View Logs';

					// Fetch scan results via standard HTMX request
					htmx.ajax('GET', '/api/scan/result', {
						target: '#results-skeleton',
						swap: 'outerHTML'
					});
				});

				es.onerror = function() { es.close(); };
			})();
			</script>
		`, scanID))
	})

	// API: Prune Prepare (Show Confirm Button)
	app.Post("/api/prune/prepare", func(c *fiber.Ctx) error {
		id := c.Query("id")
		aType := c.Query("type")
		return c.SendString(fmt.Sprintf(`
			<button hx-post="/api/prune/confirm?id=%s&type=%s" 
					hx-swap="none"
					class="bg-red-500 hover:bg-red-600 text-white text-[10px] font-black px-3 py-1 rounded-lg transition-all shadow-lg shadow-red-500/20">
				Confirm
			</button>
		`, id, aType))
	})

	// API: Prune Confirm (Actually Move to Bin)
	app.Post("/api/prune/confirm", func(c *fiber.Ctx) error {
		id := c.Query("id")
		aType := c.Query("type")
		
		found := false
		for _, a := range LastScanResult.PrunableAssets {
			if a.Name+":"+a.Type == id+":"+aType {
				found = true
				break
			}
		}

		// Handle System Items (Special Case)
		if !found {
			if id == "Package Cache" || id == "System Journals" || id == "User Cache" {
				found = true
				// For system items, we execute the purge immediately as before
				// but we don't move them to "bin" as they're not restorable in the same way
				// (or we could, but let's keep it simple for now as they're one-off commands)
				if id == "Package Cache" { system.PurgeCachesSelective(true, false) }
				if id == "System Journals" { system.PurgeCachesSelective(false, true) }
				if id == "User Cache" { system.ClearUserCache() }
				
				return c.SendString(fmt.Sprintf(`
					<div hx-swap-oob="outerHTML:#row-%s"></div>
					<div hx-swap-oob="innerHTML:#bin-section">%s</div>
				`, id, formatBinSection()))
			}
			return c.Status(404).SendString("Asset not found")
		}

		_, _, err := system.MoveToPruneBin([]string{id + ":" + aType}, LastScanResult.PrunableAssets, DryRunMode)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		// Update LastScanResult and recalculate totals
		newPrunable := []system.ProtectedAsset{}
		categoryTotal := int64(0)
		globalTotal := int64(0)
		for _, a := range LastScanResult.PrunableAssets {
			if a.Name+":"+a.Type != id+":"+aType {
				newPrunable = append(newPrunable, a)
				globalTotal += a.Size
				if a.Type == aType {
					categoryTotal += a.Size
				}
			}
		}
		LastScanResult.PrunableAssets = newPrunable
		LastScanResult.PrunableSize = globalTotal

		return c.SendString(fmt.Sprintf(`
			<div hx-swap-oob="outerHTML:#row-%s"></div>
			<div hx-swap-oob="innerHTML:#bin-content">%s</div>
			<div hx-swap-oob="innerHTML:#category-total-%s">%s</div>
			<div hx-swap-oob="innerHTML:#total-reclaimable">%s</div>
		`, id, formatBinSection(), aType, system.FormatSize(categoryTotal), system.FormatSize(globalTotal)))
	})

	// API: Bin Restore
	app.Post("/api/bin/restore", func(c *fiber.Ctx) error {
		binDir := c.Query("binDir")
		name, err := system.RestoreItem(binDir, DryRunMode)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		
		system.LogActivity("Restored " + name)
		
		// Ideally we would re-scan or add back to LastScanResult
		// For now, let's just update the bin UI.
		return c.SendString(fmt.Sprintf(`
			<div hx-swap-oob="innerHTML:#bin-content">%s</div>
			<div class="fixed bottom-4 right-4 p-4 bg-green-500 text-white rounded-xl shadow-2xl animate-in slide-in-from-right-full">
				Restored %s
			</div>
		`, formatBinSection(), name))
	})

	// API: Bin Confirm (Permanent Delete)
	app.Post("/api/bin/confirm", func(c *fiber.Ctx) error {
		binDir := c.Query("binDir")
		err := system.ConfirmItem(binDir)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		
		return c.SendString(fmt.Sprintf(`
			<div hx-swap-oob="innerHTML:#bin-content">%s</div>
		`, formatBinSection()))
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

	return fmt.Sprintf(`
		<!-- OOB: stat card updates -->
		<div id="active-wm" hx-swap-oob="innerHTML">%s</div>
		<div id="total-protected" hx-swap-oob="innerHTML">%d</div>
		<div id="protected-assets-count" hx-swap-oob="innerHTML">%d</div>
		<div id="total-reclaimable" hx-swap-oob="innerHTML">%s</div>

		<!-- OOB: env intel cards -->
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

		<!-- Main content: replaces #results-skeleton via outerHTML (htmx.ajax target) -->
		<div id="prune-results-container" class="animate-in fade-in slide-in-from-bottom-4 duration-700">
			
			<div class="space-y-6">
				<!-- Prune Bin Section (New) -->
				<div id="bin-section" class="mb-8">
					<div id="bin-content">%s</div>
				</div>

				<!-- Prunable Section -->
				<div class="space-y-4">
					<h4 class="text-[10px] font-bold text-slate-500 uppercase tracking-widest px-1">Prunable Items</h4>
					%s
					
					<!-- System Items -->
					<div class="grid grid-cols-1 md:grid-cols-3 gap-3">
						%s
					</div>
				</div>

				<!-- Protected Section -->
				<div class="pt-4 border-t border-white/5">
					<h4 class="text-[10px] font-bold text-brand-400 uppercase tracking-widest px-1 mb-4 flex items-center">
						<svg class="w-3 h-3 mr-2 text-brand-400" fill="currentColor" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M5 9V7a5 5 0 0110 0v2a2 2 0 012 2v5a2 2 0 01-2 2H5a2 2 0 01-2-2v-5a2 2 0 012-2zm8-2v2H7V7a3 3 0 016 0z" clip-rule="evenodd"/></path></svg>
						Currently Active (Protected Assets)
					</h4>
					<div class="bg-slate-900/40 rounded-2xl border border-white/5 p-4 text-left">
						<div class="grid grid-cols-1 md:grid-cols-2 gap-2">
							%s
						</div>
					</div>
				</div>
			</div>
		</div>
	`,
		res.Env.WM, len(res.Assets), len(res.Assets), totalSizeStr,
		res.Env.WM, res.Env.DM, res.Env.Bootloader,
		formatBinSection(),
		formatCategorySections(themes, icons, fonts),
		formatOtherSizes(res),
		formatProtectedList(res.Assets))
}

func formatBreakdown(res system.ScanResult, themes, icons, fonts []system.ProtectedAsset) string {
	var t, i, f int64
	for _, a := range themes { t += a.Size }
	for _, a := range icons { i += a.Size }
	for _, a := range fonts { f += a.Size }
	s := res.PacmanMetrics.Reclaim + res.JournalMetrics.Reclaim + res.UserCacheSize
	
	return fmt.Sprintf("Themes: %s • Icons: %s • Fonts: %s • System: %s", 
		system.FormatSize(t), system.FormatSize(i), system.FormatSize(f), system.FormatSize(s))
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

	// Sort assets by descending size
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Size > assets[j].Size
	})

	rows := ""
	for _, a := range assets {
		// Build optional tags
		tags := ""
		if a.Subtype != "" {
			tags += fmt.Sprintf(` <span class="text-[9px] font-black text-brand-400/60 ml-2 uppercase tracking-widest">[%s]</span>`, a.Subtype)
		}
		if a.Source != "" {
			tags += fmt.Sprintf(` <span class="text-[9px] font-black text-slate-500/60 ml-1 uppercase tracking-widest">[%s]</span>`, sourceToLabel(a.Source))
		}

		rows += fmt.Sprintf(`
			<div id="row-%s" class="flex items-center justify-between p-3 border-b border-white/5 hover:bg-white/5 transition group">
				<div class="flex items-center space-x-3 overflow-hidden">
					<span class="text-xs font-medium text-slate-300 truncate">%s</span>
					%s
				</div>
				<div class="flex items-center space-x-4">
					<span class="text-[10px] font-mono text-slate-500">%s</span>
					<button hx-post="/api/prune/prepare?id=%s&type=%s" 
							hx-swap="outerHTML"
							class="text-[9px] font-black text-brand-400 hover:text-white border border-brand-500/30 px-3 py-1 rounded-lg transition-all uppercase tracking-widest bg-brand-500/5">
						Prune
					</button>
				</div>
			</div>
		`, a.Name, a.Name, tags, a.FormattedSize, a.Name, a.Type)
	}

	return fmt.Sprintf(`
		<div class="bg-slate-800/30 rounded-2xl border border-white/5 overflow-hidden text-left">
			<div class="flex items-center justify-between bg-white/5 px-4 py-3">
				<div class="flex items-center space-x-3">
					<span class="text-sm">%s</span>
					<span class="text-[10px] font-black uppercase tracking-[0.3em] text-slate-500">%s</span>
				</div>
				<span id="category-total-%s" class="text-[10px] font-black text-brand-400 uppercase tracking-widest">%s</span>
			</div>
			<div class="max-h-64 overflow-y-auto custom-scrollbar">
				%s
			</div>
		</div>
	`, emoji, title, strings.ToLower(title[:len(title)-1]), system.FormatSize(totalSize), rows)
}

func formatBinSection() string {
	assets, _ := system.GetBinAssets()
	if len(assets) == 0 {
		return ""
	}

	rows := ""
	var totalSize int64
	for binDir, entry := range assets {
		totalSize += entry.Size
		rows += fmt.Sprintf(`
			<div class="flex items-center justify-between p-3 border-b border-white/5 hover:bg-white/5 transition group">
				<div class="flex items-center space-x-3 overflow-hidden">
					<span class="text-[10px] font-bold text-slate-400 uppercase tracking-widest">[%s]</span>
					<span class="text-xs font-medium text-slate-200 truncate">%s</span>
				</div>
				<div class="flex items-center space-x-4">
					<span class="text-[10px] font-mono text-slate-500">%s</span>
					<div class="flex items-center space-x-2">
						<button hx-post="/api/bin/restore?binDir=%s" 
								hx-swap="none"
								class="text-[9px] font-bold text-green-400 hover:text-green-300 transition-all uppercase tracking-widest">
							Undo
						</button>
						<span class="text-slate-700 font-black">/</span>
						<button hx-post="/api/bin/confirm?binDir=%s" 
								hx-swap="none"
								class="text-[9px] font-bold text-slate-500 hover:text-white transition-all uppercase tracking-widest">
							Confirm
						</button>
					</div>
				</div>
			</div>
		`, entry.Type, entry.Name, system.FormatSize(entry.Size), binDir, binDir)
	}

	return fmt.Sprintf(`
		<div class="bg-brand-500/5 rounded-3xl border border-brand-500/20 overflow-hidden animate-in zoom-in duration-500 text-left">
			<div class="flex items-center justify-between bg-brand-500/10 px-5 py-3 border-b border-brand-500/10">
				<div class="flex items-center space-x-3">
					<span class="w-2 h-2 rounded-full bg-brand-500 animate-pulse"></span>
					<span class="text-[10px] font-black uppercase tracking-[0.4em] text-brand-400">Prune Bin (Staged for removal)</span>
				</div>
				<span class="text-[10px] font-black text-brand-400 uppercase tracking-widest underline decoration-brand-500/30 underline-offset-4">%s RECLAIMABLE</span>
			</div>
			<div class="max-h-64 overflow-y-auto custom-scrollbar">
				%s
			</div>
		</div>
	`, system.FormatSize(totalSize), rows)
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
				<div id="row-%s" class="flex items-center justify-between p-4 bg-slate-800/30 rounded-2xl border border-white/5 text-left">
					<div class="flex items-center space-x-3">
						<span class="text-xl">%s</span>
						<span class="text-xs font-bold text-slate-300 uppercase tracking-widest">%s</span>
					</div>
					<div class="flex items-center space-x-4">
						<span class="text-[10px] font-mono text-brand-400 font-bold">%s</span>
						<button hx-post="/api/prune/prepare?id=%s&type=system" 
								hx-swap="outerHTML"
								class="text-[9px] font-black text-brand-400 hover:text-white border border-brand-500/30 px-3 py-1 rounded-lg transition-all uppercase tracking-widest bg-brand-500/5">
							Prune
						</button>
					</div>
				</div>
			`, item.name, item.icon, item.name, system.FormatSize(item.size), item.name)
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
			sourceTag := ""
			if a.Type == "theme" && a.Source != "" {
				sourceTag = fmt.Sprintf(` <span class="text-[8px] font-bold text-slate-600 ml-1 uppercase">[%s]</span>`, sourceToLabel(a.Source))
			}
			html += fmt.Sprintf(`
				<div class="flex items-center space-x-2 text-[10px] px-2 py-1 rounded-lg hover:bg-white/5 transition group">
					<span class="font-medium text-slate-300 truncate">%s%s</span>
				</div>
			`, a.Name, sourceTag)
		}
		
		html += `</div></div>`
	}
	
	return html
}

// sourceToLabel converts a Source field into a short display label for themes.
func sourceToLabel(source string) string {
	switch source {
	case "SDDM Config":
		return "sddm"
	case "GTK Settings":
		return "gtk"
	case "Kitty Theme":
		return "kitty"
	case "Rofi Theme":
		return "rofi"
	case "Wofi Style":
		return "wofi"
	case "Hyprland Config":
		return "hyprland"
	default:
		return source
	}
}
