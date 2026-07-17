package database

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

func Seed(db *gorm.DB) error {
	logger := slog.Default()
	if _, err := accounts.EnsureAdmin(logger, db, "miniform", true); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	createdForms := 0
	createdEntries := 0
	err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := ensureSeedProfiles(tx); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&forms.Form{}).Count(&count).Error; err != nil {
			return fmt.Errorf("count demo forms: %w", err)
		}
		if count != 0 {
			return nil
		}
		var err error
		createdForms, createdEntries, err = createDemoInbox(tx, time.Now().UTC())
		return err
	})
	if err != nil {
		return err
	}
	fmt.Printf("✓ Development data ready (%d forms, %d submissions created)\n", createdForms, createdEntries)
	return nil
}

func ensureSeedProfiles(tx *gorm.DB) error {
	mailer := &integrations.MailerProfile{
		Name: "default", Provider: "mailgun", DefaultFromName: "Miniform", DefaultFromEmail: "no-reply@example.com",
	}
	if err := tx.Where("name = ?", mailer.Name).FirstOrCreate(mailer).Error; err != nil {
		return fmt.Errorf("seed mailer profile: %w", err)
	}
	captcha := &integrations.CaptchaProfile{
		Name: "default", Provider: "turnstile",
		SiteKeysJSON: `[{"host_pattern":"*","site_key":""}]`,
		PolicyJSON:   `{"required":false,"action":"submit","widget":"managed"}`,
	}
	if err := tx.Where("name = ?", captcha.Name).FirstOrCreate(captcha).Error; err != nil {
		return fmt.Errorf("seed captcha profile: %w", err)
	}
	return nil
}

func createDemoInbox(tx *gorm.DB, now time.Time) (int, int, error) {
	demoForms := []*forms.Form{
		{Name: "Contact Form", Slug: "contact", AllowedOrigins: "*"},
		{Name: "Newsletter Signup", Slug: "newsletter", AllowedOrigins: "*"},
		{Name: "Feedback Form", Slug: "feedback", AllowedOrigins: "*"},
	}
	for _, form := range demoForms {
		if err := tx.Create(form).Error; err != nil {
			return 0, 0, fmt.Errorf("seed form %q: %w", form.Slug, err)
		}
	}

	entries := []struct {
		form   *forms.Form
		age    time.Duration
		fields map[string]any
	}{
		{demoForms[0], 2 * time.Hour, map[string]any{"name": "Ada", "email": "ada@example.com", "message": "Could you share deployment guidance?"}},
		{demoForms[0], 26 * time.Hour, map[string]any{"name": "Linus", "email": "linus@example.com", "company": "Kernel Works", "message": "Webhook delivery looks great."}},
		{demoForms[0], 50 * time.Hour, map[string]any{"name": "Grace", "email": "grace@example.com", "message": "Can uploads be retained for 30 days?"}},
		{demoForms[1], 30 * time.Minute, map[string]any{"email": "reader@example.com"}},
		{demoForms[2], 4 * time.Hour, map[string]any{"rating": 5, "comment": "Simple and fast."}},
	}
	for index, entry := range entries {
		payload, err := json.Marshal(entry.fields)
		if err != nil {
			return 0, 0, fmt.Errorf("encode demo submission: %w", err)
		}
		submission := &forms.Submission{
			FormID: entry.form.ID, DataJSON: string(payload), IPHash: fmt.Sprintf("demo-%d", index),
			UserAgent: "Miniform demo seed", CreatedAt: now.Add(-entry.age),
		}
		if err := tx.Create(submission).Error; err != nil {
			return 0, 0, fmt.Errorf("seed submission: %w", err)
		}
	}
	return len(demoForms), len(entries), nil
}
