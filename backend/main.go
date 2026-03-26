package main

import (
	"log"

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

	// API: Scan (Dry Run Placeholder)
	app.Post("/api/scan", func(c *fiber.Ctx) error {
		// Simulate a bit of processing
		return c.SendString(`
			<div class="mt-4 p-4 rounded-xl bg-slate-900/50 border border-brand-500/20 animate-in fade-in slide-in-from-top-4 duration-500">
				<div class="flex items-center justify-between mb-4">
					<h3 class="font-bold text-brand-400">Scan Results</h3>
					<span class="text-xs bg-brand-500/10 text-brand-400 px-2 py-1 rounded">Dry Run</span>
				</div>
				<ul class="space-y-2 text-sm">
					<li class="flex justify-between"><span>Pacman Cache</span> <span class="text-green-400">842 MB</span></li>
					<li class="flex justify-between"><span>Systemd Journals</span> <span class="text-green-400">120 MB</span></li>
					<li class="flex justify-between"><span>Thumbnail Cache</span> <span class="text-green-400">45 MB</span></li>
				</ul>
				<div class="mt-4 pt-4 border-t border-slate-700 flex justify-between items-center">
					<span class="font-bold">Total Reclaimable</span>
					<span class="text-xl font-black text-brand-500">1.0 GB</span>
				</div>
			</div>
		`)
	})

	log.Fatal(app.Listen(":3333"))
}
