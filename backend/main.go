package main

import (
	"fmt"
	"log"

	"pruneDash/system"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
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
		return c.JSON(fiber.Map{
			"status": "online",
			"system": "CachyOS (Arch)",
		})
	})

	// API: Scan (Dry Run placeholder replaced with real audit)
	app.Post("/api/scan", func(c *fiber.Ctx) error {
		pacTotal, pacReclaim, _ := system.GetPacmanMetrics()
		jrnlTotal, jrnlReclaim, _ := system.GetJournalMetrics()
		userReclaim, _ := system.GetUserCacheSize()

		totalReclaim := pacReclaim + jrnlReclaim + userReclaim
		totalProtected := (pacTotal - pacReclaim) + (jrnlTotal - jrnlReclaim)

		return c.SendString(fmt.Sprintf(`
			<div class="mt-4 p-4 rounded-xl bg-slate-900/50 border border-brand-500/20 animate-in fade-in slide-in-from-top-4 duration-500">
				<div class="flex items-center justify-between mb-4">
					<h3 class="font-bold text-brand-400">Analysis Results</h3>
					<span class="text-xs bg-brand-500/10 text-brand-400 px-2 py-1 rounded">Live Audit</span>
				</div>
				<ul class="space-y-3 text-sm">
					<li class="flex flex-col">
						<div class="flex justify-between">
							<span>Package Cache</span>
							<span class="text-green-400 font-bold">%s</span>
						</div>
						<span class="text-[10px] opacity-40">Keeping 2 versions for system stability (%s protected)</span>
					</li>
					<li class="flex flex-col">
						<div class="flex justify-between">
							<span>Systemd Journals</span>
							<span class="text-green-400 font-bold">%s</span>
						</div>
						<span class="text-[10px] opacity-40">Vacuum limit set to 50MB for log history (%s protected)</span>
					</li>
					<li class="flex justify-between">
						<span>User Cache</span>
						<span class="text-green-400 font-bold">%s</span>
					</li>
				</ul>
				<div class="mt-4 pt-4 border-t border-slate-700 flex justify-between items-center">
					<span class="font-bold text-slate-300">Prunable Storage</span>
					<span class="text-xl font-black text-brand-500" hx-swap-oob="innerHTML:#total-reclaimable">%7s</span>
					<span id="total-protected-oob" hx-swap-oob="innerHTML:#total-protected" class="hidden">%8s</span>
				</div>
				<p class="mt-3 text-[10px] text-slate-500 italic bg-black/20 p-2 rounded">
					Note: We protect %s of data from being pruned to ensure you can roll back packages or check recent logs if needed.
				</p>
				<button hx-post="/api/prune" 
						hx-target="#scan-results" 
						hx-swap="beforeend"
						class="mt-6 w-full bg-red-500/10 hover:bg-red-500/20 text-red-500 font-bold py-3 px-6 rounded-xl border border-red-500/20 transition-all active:scale-[0.98]">
					Prune Now
				</button>
			</div>
		`, 
		system.FormatSize(pacReclaim), system.FormatSize(pacTotal-pacReclaim),
		system.FormatSize(jrnlReclaim), system.FormatSize(jrnlTotal-jrnlReclaim),
		system.FormatSize(userReclaim),
		system.FormatSize(totalReclaim),
		system.FormatSize(totalReclaim),
		system.FormatSize(totalProtected),
		system.FormatSize(totalProtected)))
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

func formatLogs(logs []string) string {
	res := ""
	for _, log := range logs {
		res += fmt.Sprintf("<li>%s</li>", log)
	}
	return res
}
