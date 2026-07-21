package forms_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/forms"
)

func TestEmailRendering(t *testing.T) {
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

	t.Run("rejects rendered header injection", func(t *testing.T) {
		delivery := &forms.EmailDelivery{
			SubjectTemplate: "Request {{.Fields.name}}", Format: forms.EmailFormatText,
		}
		_, err := forms.RenderEmail(delivery, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now(),
			DataJSON: `{"name":"Ada\r\nBcc: hidden@example.com"}`,
		})
		assert.ErrorContains(t, err, "one non-empty line")
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

	t.Run("rejects a dynamic address containing control characters", func(t *testing.T) {
		delivery := &forms.EmailDelivery{RecipientSource: forms.EmailRecipientField, Recipient: "email"}
		_, err := forms.ResolveEmailRecipients(delivery, map[string]string{
			"email": "ada@example.com\r\nBcc: hidden@example.com",
		})
		assert.ErrorContains(t, err, `field "email" missing or invalid`)
	})
}
