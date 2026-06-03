package web

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"p2p-transfer"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/discovery"
	"p2p-transfer/internal/history"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/transfer"
	"p2p-transfer/internal/utils"
	"p2p-transfer/internal/websocket"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
)

// Start launched the Fiber web server on the specified port.
func Start(port int) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             50 * 1024 * 1024 * 1024, // Set max body size to 50GB for huge local browser drops if needed
	})

	// Enable CORS for development flexibility
	app.Use(cors.New())

	// Serve static files from embedded FS
	app.Use("/static", filesystem.New(filesystem.Config{
		Root:       http.FS(p2ptransfer.WebFS),
		PathPrefix: "web/static",
	}))

	// Dashboard page from embedded FS
	app.Get("/", func(c *fiber.Ctx) error {
		indexFile, err := p2ptransfer.WebFS.ReadFile("web/templates/index.html")
		if err != nil {
			logger.Error("Failed to read embedded index.html: %v", err)
			return c.Status(500).SendString("Dashboard template not found in embedded FS")
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.Send(indexFile)
	})

	// REST API Group
	api := app.Group("/api")

	// Get current settings
	api.Get("/settings", func(c *fiber.Ctx) error {
		return c.JSON(config.Get())
	})

	// Save settings
	api.Post("/settings", func(c *fiber.Ctx) error {
		var settings models.Settings
		if err := c.BodyParser(&settings); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		oldPort := config.Get().TransferPort
		oldDiscovery := config.Get().EnableDiscovery
		oldName := config.Get().DeviceName

		if err := config.Save(&settings); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		// Restart TCP receiver if port changed
		if settings.TransferPort != oldPort {
			logger.Info("Transfer port changed from %d to %d. Restarting receiver...", oldPort, settings.TransferPort)
			transfer.StopTCPReceiver()
			if err := transfer.StartTCPReceiver(settings.TransferPort); err != nil {
				logger.Error("Failed to restart TCP receiver on port %d: %v", settings.TransferPort, err)
			}
		}

		// Restart mDNS advertisement if name, port or discovery status changed
		if settings.DeviceName != oldName || settings.TransferPort != oldPort || settings.EnableDiscovery != oldDiscovery {
			logger.Info("Discovery configurations changed. Restarting discovery...")
			if err := discovery.Restart(); err != nil {
				logger.Error("Failed to restart discovery service: %v", err)
			}
		}

		websocket.Broadcast("config_update", settings)
		return c.JSON(settings)
	})

	// Get current active devices
	api.Get("/devices", func(c *fiber.Ctx) error {
		return c.JSON(discovery.GetDevices())
	})

	// Trigger manual scan
	api.Post("/scan", func(c *fiber.Ctx) error {
		logger.Info("Manual scan requested by user")
		if err := discovery.Restart(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "scanning"})
	})

	// Get active transfers
	api.Get("/transfers", func(c *fiber.Ctx) error {
		return c.JSON(transfer.GetActiveTransfers())
	})

	// Handle browser drag and drop uploads
	api.Post("/upload", func(c *fiber.Ctx) error {
		form, err := c.MultipartForm()
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		files := form.File["files"]
		if len(files) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "No files uploaded"})
		}

		home, _ := os.UserHomeDir()
		tempDir := filepath.Join(home, ".p2p-transfer", "temp")
		if err := os.MkdirAll(tempDir, 0755); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to create temp upload directory"})
		}

		var addedPaths []string
		for _, fileHeader := range files {
			tempPath := filepath.Join(tempDir, fileHeader.Filename)
			if err := c.SaveFile(fileHeader, tempPath); err != nil {
				logger.Error("Failed to save uploaded temp file: %v", err)
				return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("Failed to save file %s", fileHeader.Filename)})
			}
			addedPaths = append(addedPaths, tempPath)
		}

		added, dups := transfer.AddToQueue(addedPaths)
		return c.JSON(fiber.Map{"added": added, "duplicates": dups, "queue": transfer.GetQueue()})
	})

	// Open local folder picker
	api.Post("/select-files", func(c *fiber.Ctx) error {
		files, err := utils.SelectLocalFiles()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		
		type FileInfo struct {
			Path string `json:"path"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		var infoList []FileInfo
		for _, path := range files {
			info, err := os.Stat(path)
			if err == nil {
				infoList = append(infoList, FileInfo{
					Path: path,
					Name: filepath.Base(path),
					Size: info.Size(),
				})
			}
		}
		return c.JSON(fiber.Map{"files": infoList})
	})

	// Open local folder picker
	api.Post("/select-folder", func(c *fiber.Ctx) error {
		folder, err := utils.SelectLocalFolder()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if folder == "" {
			return c.JSON(fiber.Map{"files": []interface{}{}})
		}
		
		info, err := os.Stat(folder)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		
		return c.JSON(fiber.Map{"files": []interface{}{
			fiber.Map{
				"path": folder,
				"name": filepath.Base(folder),
				"size": info.Size(),
			},
		}})
	})

	// Clear local queue
	api.Post("/clear-queue", func(c *fiber.Ctx) error {
		transfer.ClearQueue()
		return c.JSON(fiber.Map{"status": "ok", "queue": transfer.GetQueue()})
	})

	// Remove item from local queue
	api.Post("/remove-queue", func(c *fiber.Ctx) error {
		var req struct {
			Index int `json:"index"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		transfer.RemoveFromQueue(req.Index)
		return c.JSON(fiber.Map{"status": "ok", "queue": transfer.GetQueue()})
	})

	// Get local queue
	api.Get("/queue", func(c *fiber.Ctx) error {
		return c.JSON(transfer.GetQueue())
	})

	// Initiate transfer
	api.Post("/send", func(c *fiber.Ctx) error {
		var req struct {
			DeviceIDs []string `json:"deviceIds"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		queue := transfer.GetQueue()
		if len(queue) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "No files in queue to transfer"})
		}

		devices := discovery.GetDevices()
		for _, id := range req.DeviceIDs {
			var target *models.Device
			for _, dev := range devices {
				if dev.ID == id {
					target = &dev
					break
				}
			}
			if target != nil {
				transfer.StartSendTransfer(*target, queue)
			} else {
				logger.Warn("Device with ID %s not found for transfer", id)
			}
		}

		// Clear send queue once transfer starts
		transfer.ClearQueue()
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Direct transfer bypassing queue completely
	api.Post("/send-direct", func(c *fiber.Ctx) error {
		var req struct {
			DeviceIDs []string `json:"deviceIds"`
			Paths     []string `json:"paths"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		if len(req.Paths) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "No files specified"})
		}

		var items []models.TransferItem
		for _, path := range req.Paths {
			info, err := os.Stat(path)
			if err != nil {
				logger.Error("Direct send file stats failed: %v", err)
				continue
			}
			itemType := "file"
			if info.IsDir() {
				itemType = "folder"
			}
			items = append(items, models.TransferItem{
				Path: path,
				Name: filepath.Base(path),
				Size: info.Size(),
				Type: itemType,
			})
		}

		if len(items) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "No valid files found to transfer"})
		}

		devices := discovery.GetDevices()
		for _, id := range req.DeviceIDs {
			var target *models.Device
			for _, dev := range devices {
				if dev.ID == id {
					target = &dev
					break
				}
			}
			if target != nil {
				transfer.StartSendTransfer(*target, items)
			}
		}

		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Accept a pending transfer
	api.Post("/accept-transfer", func(c *fiber.Ctx) error {
		var req struct {
			TransferID string `json:"transferId"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		transfer.ResolvePendingAccept(req.TransferID, true)
		return c.JSON(fiber.Map{"status": "accepted"})
	})

	// Reject a pending transfer
	api.Post("/reject-transfer", func(c *fiber.Ctx) error {
		var req struct {
			TransferID string `json:"transferId"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		transfer.ResolvePendingAccept(req.TransferID, false)
		return c.JSON(fiber.Map{"status": "rejected"})
	})

	// Cancel an active transfer
	api.Post("/cancel-transfer", func(c *fiber.Ctx) error {
		var req struct {
			TransferID string `json:"transferId"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		transfer.CancelTransfer(req.TransferID)
		return c.JSON(fiber.Map{"status": "cancelled"})
	})

	// Open download folder in explorer
	api.Post("/open-folder", func(c *fiber.Ctx) error {
		cfg := config.Get()
		err := exec.Command("explorer", cfg.DownloadDir).Start()
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "opened"})
	})

	// Get history
	api.Get("/history", func(c *fiber.Ctx) error {
		return c.JSON(history.GetEntries())
	})

	// Clear history
	api.Post("/clear-history", func(c *fiber.Ctx) error {
		if err := history.ClearEntries(); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "cleared"})
	})

	// Browse local files and drives
	api.Get("/browse", func(c *fiber.Ctx) error {
		path := c.Query("path")
		
		type BrowseItem struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"isDir"`
			Size  int64  `json:"size"`
		}
		
		if path == "" || path == "drives" {
			var items []BrowseItem
			for _, d := range []string{"C:", "D:", "E:", "F:", "G:", "H:", "I:", "J:", "K:"} {
				drivePath := d + "\\"
				if _, err := os.Stat(drivePath); err == nil {
					items = append(items, BrowseItem{
						Name:  d,
						Path:  drivePath,
						IsDir: true,
						Size:  0,
					})
				}
			}
			return c.JSON(fiber.Map{
				"currentPath": "drives",
				"parentPath":  "",
				"items":       items,
			})
		}

		path = filepath.Clean(path)
		dirEntries, err := os.ReadDir(path)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}

		var items []BrowseItem
		for _, entry := range dirEntries {
			if strings.HasPrefix(entry.Name(), "$") || entry.Name() == "System Volume Information" {
				continue
			}
			
			size := int64(0)
			if !entry.IsDir() {
				if info, err := entry.Info(); err == nil {
					size = info.Size()
				}
			}

			items = append(items, BrowseItem{
				Name:  entry.Name(),
				Path:  filepath.Join(path, entry.Name()),
				IsDir: entry.IsDir(),
				Size:  size,
			})
		}

		parent := filepath.Dir(path)
		// On Windows, filepath.Dir("C:\") returns "C:\"
		if parent == path || (len(path) == 3 && path[1:] == ":\\") {
			parent = "drives"
		}

		return c.JSON(fiber.Map{
			"currentPath": path,
			"parentPath":  parent,
			"items":       items,
		})
	})

	// Add files or folders directly to send queue by absolute path
	api.Post("/add-to-queue", func(c *fiber.Ctx) error {
		var req struct {
			Paths []string `json:"paths"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		added, dups := transfer.AddToQueue(req.Paths)
		return c.JSON(fiber.Map{"added": added, "duplicates": dups, "queue": transfer.GetQueue()})
	})

	addr := fmt.Sprintf(":%d", port)
	logger.Info("Fiber web server starting on http://localhost:%d", port)
	go func() {
		if err := app.Listen(addr); err != nil {
			logger.Error("Fiber server error: %v", err)
		}
	}()
}
