package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
)

func DemoContactForm(ctx *cartridge.Context) error {
	slug := strings.TrimSpace(ctx.Query("slug", "contact"))
	form, err := forms.GetBySlug(ctx.DB(), slug)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ctx.Status(fiber.StatusNotFound).SendString("Demo endpoint not found. Run 'make demo' or pass an existing slug with ?slug=your-slug.")
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return ctx.Render("demo", fiber.Map{"FormName": form.Name, "FormSlug": form.Slug, "FormToken": form.Token}, "")
}
