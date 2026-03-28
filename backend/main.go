package main

import (
	"fmt"
	"log"

	"pruneDash/system"

	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/template/html/v2"
)

var (
	ScanResults = make(chan system.ScanResult, 1) // Signal scan completion
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
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Transfer-Encoding", "chunked")

		rc := make(chan system.ScanResult, 1)
		// We use a select to listen for either a log or the final result
		go func() {
			res := <-ScanResults
			rc <- res
		}()

		for {
			select {
			case logMsg := <-system.ScanLogs:
				fmt.Fprintf(w, "event: log\ndata: <div class='text-blue-400 font-mono text-[10px] py-0.5 border-b border-white/5 opacity-80 animate-in fade-in slide-in-from-left-1'>%s</div>\n\n", logMsg)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case res := <-rc:
				// Send final result as OOB swap
				html := formatScanResultsHTML(res)
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", html)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				return // End SSE stream
			case <-r.Context().Done():
				return
			}
		}
	}))

	// API: Scan (Async trigger)
	app.Post("/api/scan", func(c *fiber.Ctx) error {
		// Start scan in background
		go func() {
			res := system.PerformAsyncScan()
			ScanResults <- res
		}()

		// Return initial Loading UI with SSE console
		return c.SendString(`
			<div class="mt-4 p-4 rounded-xl bg-slate-900/50 border border-brand-500/20 animate-in fade-in slide-in-from-top-4 duration-500">
				
				<!-- Activity Console (SSE Listener) -->
				<div class="mb-6 bg-black/40 rounded-xl border border-white/5 p-3 overflow-hidden shadow-inner">
					<div class="flex items-center justify-between mb-2">
						<span class="text-[9px] font-bold uppercase tracking-widest text-slate-500">Live Activity Monitor</span>
						<span class="flex h-1.5 w-1.5 rounded-full bg-brand-500 animate-pulse"></span>
					</div>
					<div id="activity-log" hx-ext="sse" sse-connect="/api/scan/logs" sse-swap="log" hx-swap="beforeend" 
						 class="h-48 overflow-y-auto scrollbar-hide flex flex-col-reverse italic">
						<div class="text-[10px] text-slate-500 opacity-50">Handshaking with system probes...</div>
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
		`)
	})

	// API: Prune (Execute Cleanup)
	app.Post("/api/prune", func(c *fiber.Ctx) error {
		// 1. Purge Caches
		purgeLogs, _ := system.PurgeCaches()

		// 2. Clear User Cache
		system.ClearUserCache()

		return c.SendString(fmt.Sprintf(`
			<div class="mt-4 p-4 rounded-xl bg-green-500/10 border border-green-500/20 text-green-400 animate-in fade-in zoom-in duration-300">
				<h3 class="font-bold mb-2">Cleanup Successful</h3>
				<ul class="text-xs space-y-1 opacity-80">
					%s
					<li>User cache cleared successfully.</li>
				</ul>
				<button hx-post="/api/scan" hx-trigger="load" hx-swap="none" class="hidden"></button>
			</div>
		`, formatLogs(purgeLogs)))
	})

	log.Fatal(app.Listen(":3333"))
}

func formatScanResultsHTML(res system.ScanResult) string {
	// TODO: Implement the full summary UI here
	return fmt.Sprintf(`
		<div id="intel-section" hx-swap-oob="innerHTML">
			<div class="mb-4 bg-slate-800/30 rounded-2xl p-4 border border-white/5">
				<p class="text-xs text-brand-400 font-bold uppercase tracking-widest">Environment: %s</p>
			</div>
		</div>
		<div id="results-skeleton" hx-swap-oob="innerHTML">
			<div class="mt-4 p-4 rounded-xl bg-slate-900/50 border border-brand-400/20">
				<p class="text-sm font-bold text-brand-400">Scan Complete / Results Pending Detail Render</p>
			</div>
		</div>
	`, res.Env.WM)
}

func formatLogs(logs []string) string {
	res := ""
	for _, log := range logs {
		res += fmt.Sprintf("<li>%s</li>", log)
	}
	return res
}
