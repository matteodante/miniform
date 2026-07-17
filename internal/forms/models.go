package forms

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/integrations"
)

const (
	WebhookStatusPending    = "pending"
	WebhookStatusDelivering = "delivering"
	WebhookStatusDelivered  = "delivered"
	WebhookStatusRetrying   = "retrying"
	WebhookStatusFailed     = "failed"
	DefaultRetryLimit       = 3
)

type Form struct {
	ID               uint                         `gorm:"primaryKey"`
	PublicID         string                       `gorm:"size:20;uniqueIndex;not null"`
	Name             string                       `gorm:"size:255;not null"`
	Slug             string                       `gorm:"size:255;uniqueIndex;not null"`
	Token            string                       `gorm:"size:64;uniqueIndex;not null"`
	AllowedOrigins   string                       `gorm:"type:text"`
	UseSDK           bool                         `gorm:"not null;default:false"`
	GeneratedHTML    string                       `gorm:"type:text"`
	CaptchaProfileID *uint                        `gorm:"index"`
	CaptchaProfile   *integrations.CaptchaProfile `gorm:"constraint:OnDelete:RESTRICT"`
	CreatedAt        time.Time
	UpdatedAt        time.Time

	Submissions     []Submission
	EmailDelivery   *EmailDelivery   `gorm:"constraint:OnDelete:CASCADE"`
	WebhookDelivery *WebhookDelivery `gorm:"constraint:OnDelete:CASCADE"`
}

type EmailDelivery struct {
	ID              uint                        `gorm:"primaryKey"`
	FormID          uint                        `gorm:"uniqueIndex;not null"`
	Form            *Form                       `gorm:"constraint:OnDelete:CASCADE"`
	Enabled         bool                        `gorm:"not null;default:false"`
	MailerProfileID *uint                       `gorm:"index"`
	MailerProfile   *integrations.MailerProfile `gorm:"constraint:OnDelete:RESTRICT"`
	Recipient       string                      `gorm:"size:320"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type WebhookDelivery struct {
	ID          uint   `gorm:"primaryKey"`
	FormID      uint   `gorm:"uniqueIndex;not null"`
	Form        *Form  `gorm:"constraint:OnDelete:CASCADE"`
	Enabled     bool   `gorm:"not null;default:false"`
	URL         string `gorm:"type:text"`
	Secret      string `gorm:"size:255"`
	HeadersJSON string `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Submission struct {
	ID        uint      `gorm:"primaryKey"`
	FormID    uint      `gorm:"not null;index:idx_submissions_form_created,priority:1"`
	Form      *Form     `gorm:"constraint:OnDelete:CASCADE"`
	DataJSON  string    `gorm:"type:text;not null"`
	UserAgent string    `gorm:"type:text"`
	IsSpam    bool      `gorm:"index"`
	CreatedAt time.Time `gorm:"index:idx_submissions_created_at;index:idx_submissions_form_created,priority:2"`
	UpdatedAt time.Time

	Files         []*SubmissionFile
	EmailEvents   []EmailEvent
	WebhookEvents []WebhookEvent
}

type SubmissionFile struct {
	ID           uint        `gorm:"primaryKey"`
	SubmissionID uint        `gorm:"index;not null"`
	Submission   *Submission `gorm:"constraint:OnDelete:CASCADE"`
	FieldName    string      `gorm:"size:255;not null"`
	Filename     string      `gorm:"size:255;not null"`
	ContentType  string      `gorm:"size:100"`
	Size         int64       `gorm:"not null"`
	StoragePath  string      `gorm:"size:500;not null"`
	CreatedAt    time.Time
}

type WebhookEvent struct {
	ID             uint        `gorm:"primaryKey"`
	SubmissionID   uint        `gorm:"not null;index:idx_webhook_events_submission_created,priority:1"`
	Submission     *Submission `gorm:"constraint:OnDelete:CASCADE"`
	Status         string      `gorm:"size:32;index;not null"`
	AttemptCount   int         `gorm:"not null;default:0"`
	LastAttemptErr string      `gorm:"type:text"`
	NextAttemptAt  *time.Time  `gorm:"index:idx_webhook_events_queue,priority:1"`
	LastAttemptAt  *time.Time
	CreatedAt      time.Time `gorm:"index:idx_webhook_events_submission_created,priority:2;index:idx_webhook_events_queue,priority:2"`
	UpdatedAt      time.Time
}

type EmailEvent struct {
	ID             uint        `gorm:"primaryKey"`
	SubmissionID   uint        `gorm:"not null;index:idx_email_events_submission_created,priority:1"`
	Submission     *Submission `gorm:"constraint:OnDelete:CASCADE"`
	Status         string      `gorm:"size:32;index;not null"`
	AttemptCount   int         `gorm:"not null;default:0"`
	LastAttemptErr string      `gorm:"type:text"`
	NextAttemptAt  *time.Time  `gorm:"index:idx_email_events_queue,priority:1"`
	LastAttemptAt  *time.Time
	CreatedAt      time.Time `gorm:"index:idx_email_events_submission_created,priority:2;index:idx_email_events_queue,priority:2"`
	UpdatedAt      time.Time
}

func (form *Form) BeforeCreate(_ *gorm.DB) error {
	if form.PublicID == "" {
		publicID, err := GeneratePublicID()
		if err != nil {
			return fmt.Errorf("generate form public ID: %w", err)
		}
		form.PublicID = publicID
	}
	if form.Token == "" {
		token, err := generateToken(24)
		if err != nil {
			return fmt.Errorf("generate form token: %w", err)
		}
		form.Token = token
	}
	if form.Slug == "" {
		slug, err := Slugify(form.Name)
		if err != nil {
			return fmt.Errorf("generate form slug: %w", err)
		}
		form.Slug = slug
	}
	return nil
}

func GeneratePublicID() (string, error) {
	return randomHex(rand.Reader, 10)
}

func generateToken(byteCount int) (string, error) {
	return randomHex(rand.Reader, byteCount)
}

func randomHex(source io.Reader, byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := io.ReadFull(source, buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func Slugify(value string) (string, error) {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return GeneratePublicID()
	}
	return strings.Join(parts, "-"), nil
}

func NewWebhookEvent(submissionID uint, scheduledAt time.Time) *WebhookEvent {
	nextAttempt := scheduledAt.UTC()
	return &WebhookEvent{SubmissionID: submissionID, Status: WebhookStatusPending, NextAttemptAt: &nextAttempt}
}

func NewEmailEvent(submissionID uint, scheduledAt time.Time) *EmailEvent {
	nextAttempt := scheduledAt.UTC()
	return &EmailEvent{SubmissionID: submissionID, Status: WebhookStatusPending, NextAttemptAt: &nextAttempt}
}
