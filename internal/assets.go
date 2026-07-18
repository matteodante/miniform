package internal

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/web"
)

func mountAssets(server *cartridge.Server, cfg *config.Config) {
	app := server.App()

	if cfg.IsDevelopment() {
		app.Static("/assets", cfg.GetPublicDirectory(), fiber.Static{
			ByteRange: true, Browse: false,
		})
		return
	}
	app.Use("/assets", filesystem.New(filesystem.Config{
		Root: http.FS(web.Static), Browse: false,
		MaxAge: int((365 * 24 * time.Hour).Seconds()),
	}))
}
