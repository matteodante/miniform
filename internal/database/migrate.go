package database

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(Models()...); err != nil {
		return fmt.Errorf("migrate application schema: %w", err)
	}
	return nil
}

func Models() []any {
	return []any{
		&accounts.User{}, &accounts.Settings{},
		&integrations.MailerProfile{}, &integrations.CaptchaProfile{},
		&forms.Form{}, &forms.EmailDelivery{}, &forms.WebhookDelivery{},
		&forms.Submission{}, &forms.SubmissionFile{},
		&forms.EmailEvent{}, &forms.WebhookEvent{},
	}
}
