package forms_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/forms"
)

func TestEmailRendering(t *testing.T) {
	t.Run("preserves numeric fields in email subjects and bodies", func(t *testing.T) {
		rendered, err := forms.RenderEmail(&forms.EmailDelivery{
			SubjectTemplate: "Order {{.Fields.id}}", Format: forms.EmailFormatHTML,
			TextTemplate: "Value {{.Fields.amount}}", HTMLTemplate: "<p>{{.Fields.id}}</p>",
		}, &forms.Submission{
			Form:     &forms.Form{Name: "Contact"},
			DataJSON: `{"id":9007199254740993,"amount":0.1234567890123456789}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "Order 9007199254740993", rendered.Subject)
		assert.Equal(t, "Value 0.1234567890123456789", rendered.TextBody)
		assert.Equal(t, "<p>9007199254740993</p>", rendered.HTMLBody)
	})

	t.Run("renders the final HTML and text alternative from one submission", func(t *testing.T) {
		delivery := &forms.EmailDelivery{
			RecipientSource: forms.EmailRecipientField, Recipient: "email",
			ReplyToSource: forms.EmailReplyToField, ReplyTo: "email",
			SubjectTemplate: "Request · {{.Fields.name}}", Format: forms.EmailFormatHTML,
			TextTemplate: "Hello {{.Fields.name}}\n{{range .FieldList}}{{.Name}}={{.Value}}\n{{end}}",
			HTMLTemplate: `<h1>Hello {{.Fields.name}}</h1><p>{{.Fields.message}}</p>`,
		}
		submission := &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Date(2026, 7, 21, 12, 30, 0, 0, time.UTC),
			DataJSON: `{"name":"<Ada>","email":"ada@example.com","message":"<script>alert(1)</script>","count":2}`,
		}

		rendered, err := forms.RenderEmail(delivery, submission)
		require.NoError(t, err)
		assert.Equal(t, "Request · <Ada>", rendered.Subject)
		assert.Equal(t, forms.EmailFormatHTML, rendered.Format)
		assert.Contains(t, rendered.TextBody, "count=2\nemail=ada@example.com")
		assert.Equal(t, `<h1>Hello &lt;Ada&gt;</h1><p>&lt;script&gt;alert(1)&lt;/script&gt;</p>`, rendered.HTMLBody)

		fields := forms.EmailTemplateFields(submission.DataJSON)
		assert.Equal(t, "2", fields["count"])
		recipients, err := forms.ResolveEmailRecipients(delivery, fields)
		require.NoError(t, err)
		assert.Equal(t, []string{"ada@example.com"}, recipients)
		replyTo, err := forms.ResolveEmailReplyTo(delivery, fields)
		require.NoError(t, err)
		assert.Equal(t, "ada@example.com", replyTo)
	})

	t.Run("normalizes submitted subject text without creating headers", func(t *testing.T) {
		delivery := &forms.EmailDelivery{
			SubjectTemplate: "Request {{.Fields.name}}", Format: forms.EmailFormatText,
		}
		rendered, err := forms.RenderEmail(delivery, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now(),
			DataJSON: `{"name":"  Ada\r\nBcc: hidden@example.com\u0000\tD'Àngelo  "}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "Request Ada Bcc: hidden@example.com D'Àngelo", rendered.Subject)
		assert.NotContains(t, rendered.Subject, "\r")
		assert.NotContains(t, rendered.Subject, "\n")
		assert.NotContains(t, rendered.Subject, "\x00")
	})

	t.Run("omits unusable field-based Reply-To without losing the notification", func(t *testing.T) {
		delivery := &forms.EmailDelivery{ReplyToSource: forms.EmailReplyToField, ReplyTo: "email"}
		for name, fields := range map[string]map[string]string{
			"missing":          {},
			"empty":            {"email": ""},
			"whitespace":       {"email": " \t "},
			"invalid":          {"email": "not an email"},
			"header injection": {"email": "ada@example.com\r\nBcc: hidden@example.com"},
		} {
			t.Run(name, func(t *testing.T) {
				replyTo, err := forms.ResolveEmailReplyTo(delivery, fields)
				require.NoError(t, err)
				assert.Empty(t, replyTo)
			})
		}
	})

	t.Run("does not evaluate the unused HTML body for text email", func(t *testing.T) {
		rendered, err := forms.RenderEmail(&forms.EmailDelivery{
			SubjectTemplate: "Text only", Format: forms.EmailFormatText,
			TextTemplate: "Hello {{.Fields.name}}", HTMLTemplate: "<p>{{.Fields.missing}}</p>",
		}, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now(), DataJSON: `{"name":"Ada"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "Hello Ada", rendered.TextBody)
		assert.Empty(t, rendered.HTMLBody)
	})

	t.Run("renders absent optional fields as empty values", func(t *testing.T) {
		rendered, err := forms.RenderEmail(&forms.EmailDelivery{
			SubjectTemplate: "{{.Fields.company}}", Format: forms.EmailFormatHTML,
			TextTemplate: "Company: {{.Fields.company}}",
			HTMLTemplate: "<p>Company: {{.Fields.company}}</p>",
		}, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now(), DataJSON: `{"name":"Ada"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "New submission", rendered.Subject)
		assert.Equal(t, "Company: ", rendered.TextBody)
		assert.Equal(t, "<p>Company: </p>", rendered.HTMLBody)
	})

	t.Run("rejects a dynamic address containing control characters", func(t *testing.T) {
		delivery := &forms.EmailDelivery{RecipientSource: forms.EmailRecipientField, Recipient: "email"}
		_, err := forms.ResolveEmailRecipients(delivery, map[string]string{
			"email": "ada@example.com\r\nBcc: hidden@example.com",
		})
		assert.ErrorContains(t, err, `field "email" missing or invalid`)
	})
}
