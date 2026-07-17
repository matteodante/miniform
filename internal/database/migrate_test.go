package database

import (
	"testing"
	"time"

	"github.com/matteodante/miniform/internal/forms"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrate(t *testing.T) {
	t.Run("creates indexes for chronological queries", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			NowFunc: func() time.Time { return time.Now().UTC() },
		})
		require.NoError(t, err)
		require.NoError(t, Migrate(db))

		indexes := []struct {
			model any
			name  string
		}{
			{&forms.Submission{}, "idx_submissions_created_at"},
			{&forms.Submission{}, "idx_submissions_form_created"},
			{&forms.WebhookEvent{}, "idx_webhook_events_submission_created"},
			{&forms.WebhookEvent{}, "idx_webhook_events_queue"},
			{&forms.WebhookEvent{}, "idx_webhook_events_status"},
			{&forms.EmailEvent{}, "idx_email_events_submission_created"},
			{&forms.EmailEvent{}, "idx_email_events_queue"},
			{&forms.EmailEvent{}, "idx_email_events_status"},
		}

		for _, index := range indexes {
			t.Run(index.name, func(t *testing.T) {
				require.True(t, db.Migrator().HasIndex(index.model, index.name))
			})
		}

		redundantIndexes := []struct {
			model any
			name  string
		}{
			{&forms.Submission{}, "idx_submissions_form_id"},
			{&forms.WebhookEvent{}, "idx_webhook_events_submission_id"},
			{&forms.EmailEvent{}, "idx_email_events_submission_id"},
		}

		for _, index := range redundantIndexes {
			t.Run("omits "+index.name, func(t *testing.T) {
				require.False(t, db.Migrator().HasIndex(index.model, index.name))
			})
		}

		t.Run("omits removed generic fields and tables", func(t *testing.T) {
			require.True(t, db.Migrator().HasColumn(&forms.EmailDelivery{}, "recipient"))
			require.False(t, db.Migrator().HasColumn(&forms.EmailDelivery{}, "overrides_json"))
			require.False(t, db.Migrator().HasColumn(&forms.Form{}, "captcha_overrides_json"))
			require.False(t, db.Migrator().HasColumn(&forms.Submission{}, "ip_hash"))
			require.False(t, db.Migrator().HasTable("settings"))
		})
	})
}
