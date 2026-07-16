package http

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
)

// DemoContactForm renders the local form submission test page.
func DemoContactForm(ctx *cartridge.Context) error {
	db := ctx.DB()

	slug := strings.TrimSpace(ctx.Query("slug"))
	if slug == "" {
		slug = "contact"
	}

	form, err := forms.GetBySlug(db, slug)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ctx.Status(fiber.StatusNotFound).SendString("Demo endpoint not found. Run 'make demo' or pass an existing slug with ?slug=your-slug.")
		}
		return fiber.ErrInternalServerError
	}

	return ctx.Render("demo", fiber.Map{
		"FormName":  form.Name,
		"FormSlug":  form.Slug,
		"FormToken": form.Token,
	}, "")
}
