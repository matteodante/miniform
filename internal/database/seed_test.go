package database

import (
	"testing"
	"time"

	"github.com/matteodante/miniform/internal/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSeed(t *testing.T) {
	t.Run("creates complete demo forms", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, Migrate(db))

		formCount, submissionCount, err := createDemoInbox(db, time.Now().UTC())
		require.NoError(t, err)
		assert.Equal(t, 3, formCount)
		assert.Equal(t, 5, submissionCount)

		var formsWithEmail, formsWithWebhook int64
		require.NoError(t, db.Model(&forms.EmailDelivery{}).Count(&formsWithEmail).Error)
		require.NoError(t, db.Model(&forms.WebhookDelivery{}).Count(&formsWithWebhook).Error)
		assert.Equal(t, int64(formCount), formsWithEmail)
		assert.Equal(t, int64(formCount), formsWithWebhook)
	})
}
