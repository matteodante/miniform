package jobs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

type smtpCapture struct {
	from, message string
	to            []string
	authenticated bool
}

type smtpServerResult struct {
	capture smtpCapture
	err     error
}

func TestEmailDelivery(t *testing.T) {
	t.Run("sends a sanitized SMTP message", func(t *testing.T) {
		host, port, result := fakeSMTP(t)
		settings := &smtpConfig{
			Host: host, Port: port, Encryption: "none",
			Username: "user", Password: "secret",
			From:       "Forms <forms@example.com>",
			Recipients: []string{"owner@example.com", "Archive <archive@example.com>"},
		}
		message, err := buildSMTPMessage(outboundEmail{
			From: settings.From, To: settings.Recipients,
			ReplyTo: "Customer <customer@example.com>",
			Subject: "Hello\r\nBcc: hidden@example.com", Format: forms.EmailFormatText,
			TextBody: "line one\nline two",
		})
		require.NoError(t, err)
		require.NoError(t, sendSMTP(context.Background(), settings, message))

		server := <-result
		require.NoError(t, server.err)
		assert.True(t, server.capture.authenticated)
		assert.Contains(t, server.capture.from, "forms@example.com")
		assert.Equal(t, []string{"RCPT TO:<owner@example.com>", "RCPT TO:<archive@example.com>"}, server.capture.to)
		assert.Contains(t, server.capture.message, "Subject: Hello Bcc: hidden@example.com")
		assert.NotContains(t, server.capture.message, "\r\nBcc:")
		assert.Contains(t, server.capture.message, `Reply-To: "Customer" <customer@example.com>`)
		assert.Contains(t, server.capture.message, "line one\r\nline two")
	})

	t.Run("delivers escaped HTML and text fallback to every configured recipient", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		host, port, result := fakeSMTP(t)
		profile := &integrations.MailerProfile{
			Name: "Relay", DefaultFromName: "Miniform",
			DefaultFromEmail: "forms@example.com", SMTPHost: host, SMTPPort: port,
			SMTPUsername: "relay", SMTPPassword: "secret", SMTPEncryption: "none",
		}
		form := &forms.Form{Name: "Contact", Slug: "contact-job", AllowedOrigins: "*"}
		var event *forms.EmailEvent
		var delivery *forms.EmailDelivery
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			if err := tx.Create(profile).Error; err != nil {
				return err
			}
			if err := tx.Create(form).Error; err != nil {
				return err
			}
			profileID := profile.ID
			delivery = &forms.EmailDelivery{
				FormID: form.ID, Enabled: true, MailerProfileID: &profileID,
				Name: "Internal request", RecipientSource: forms.EmailRecipientStatic,
				Recipient:     "owner@example.com, archive@example.com",
				ReplyToSource: forms.EmailReplyToField, ReplyTo: "email",
				SubjectTemplate: "Request from {{.Fields.name}}", Format: forms.EmailFormatHTML,
				TextTemplate: "Name: {{.Fields.name}}\nMessage: {{.Fields.message}}",
				HTMLTemplate: "<p>{{.Fields.name}}</p><p>{{.Fields.message}}</p>",
			}
			if err := tx.Create(delivery).Error; err != nil {
				return err
			}
			submission := &forms.Submission{FormID: form.ID, DataJSON: `{"name":"Alice","email":"alice@example.com","message":"<script>alert('x')</script>"}`}
			if err := tx.Create(submission).Error; err != nil {
				return err
			}
			event = forms.NewEmailEvent(submission.ID, time.Now(), delivery.ID)
			return tx.Create(event).Error
		}))

		ctx := &cartridge.JobContext{
			Context: context.Background(), DB: db,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
		require.NoError(t, NewEmailDispatcher(&config.Config{}).ProcessBatch(ctx))

		var stored forms.EmailEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivered, stored.Status)
		server := <-result
		require.NoError(t, server.err)
		assert.Len(t, server.capture.to, 2)
		assert.Contains(t, server.capture.message, "Reply-To: <alice@example.com>")
		assert.Contains(t, server.capture.message, "Subject: Request from Alice")
		plain, html := multipartBodies(t, server.capture.message)
		assert.Contains(t, plain, "Alice")
		assert.Contains(t, plain, "<script>alert('x')</script>")
		assert.Contains(t, html, "Alice")
		assert.Contains(t, html, "&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;")
		assert.NotContains(t, html, "<script>alert")
	})

	t.Run("delivers two independent notifications created by one submission", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		host, port, results := fakeSMTPConnections(t, 2)
		form := setupTwoEmailForm(t, db, host, port, forms.EmailReplyToField, "email")
		logger := slog.New(slog.DiscardHandler)

		submission, err := forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{
			"name": "Ada Èlite", "email": "customer@recipient.invalid",
			"message": `<script>alert("smtp")</script> & "quotes"`,
		}, "email integration test", "", nil)
		require.NoError(t, err)
		require.NotZero(t, submission.ID)

		ctx := &cartridge.JobContext{Context: t.Context(), DB: db, Logger: logger}
		require.NoError(t, NewEmailDispatcher(&config.Config{}).ProcessBatch(ctx))

		events, err := forms.ListEmailEvents(db, form.ID, "", 10)
		require.NoError(t, err)
		require.Len(t, events, 2)
		for _, event := range events {
			assert.Equal(t, forms.WebhookStatusDelivered, event.Status)
			assert.Equal(t, 1, event.AttemptCount)
		}

		captures := receiveSMTPCaptures(t, results, 2)
		messages := make(map[string]smtpCapture, len(captures))
		for _, capture := range captures {
			message, err := mail.ReadMessage(strings.NewReader(capture.message))
			require.NoError(t, err)
			subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
			require.NoError(t, err)
			messages[subject] = capture
		}

		internal, found := messages["Internal · Ada Èlite"]
		require.True(t, found)
		assert.Equal(t, []string{
			"RCPT TO:<qa-internal@team.invalid>",
			"RCPT TO:<qa-archive@archive.invalid>",
		}, internal.to)
		assert.Contains(t, internal.message, "Reply-To: <customer@recipient.invalid>")
		plain, html := multipartBodies(t, internal.message)
		assert.Contains(t, plain, `<script>alert("smtp")</script> & "quotes"`)
		assert.Contains(t, html, "&lt;script&gt;alert(&#34;smtp&#34;)&lt;/script&gt; &amp; &#34;quotes&#34;")
		assert.NotContains(t, html, "<script>")

		customer, found := messages["Confirmation · Ada Èlite"]
		require.True(t, found)
		assert.Equal(t, []string{"RCPT TO:<customer@recipient.invalid>"}, customer.to)
		assert.Contains(t, customer.message, `Reply-To: "Support" <support@sender.invalid>`)
		message, err := mail.ReadMessage(strings.NewReader(customer.message))
		require.NoError(t, err)
		body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
		require.NoError(t, err)
		assert.Equal(t, "We received your request, Ada Èlite.", string(body))
	})

	t.Run("keeps sibling delivery independent from an invalid dynamic recipient", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		host, port, results := fakeSMTPConnections(t, 1)
		form := setupTwoEmailForm(t, db, host, port, forms.EmailReplyToStatic, "QA <qa-reply@sender.invalid>")
		logger := slog.New(slog.DiscardHandler)

		_, err := forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{
			"name": "Invalid recipient", "email": "not-an-email", "message": "internal still delivers",
		}, "email integration test", "", nil)
		require.NoError(t, err)

		ctx := &cartridge.JobContext{Context: t.Context(), DB: db, Logger: logger}
		require.NoError(t, NewEmailDispatcher(&config.Config{}).ProcessBatch(ctx))

		events, err := forms.ListEmailEvents(db, form.ID, "", 10)
		require.NoError(t, err)
		require.Len(t, events, 2)
		states := make(map[string]forms.EmailEvent, len(events))
		for _, event := range events {
			states[event.EmailDelivery.Name] = event
		}
		assert.Equal(t, forms.WebhookStatusDelivered, states["Internal notification"].Status)
		assert.Equal(t, 1, states["Internal notification"].AttemptCount)
		assert.Equal(t, forms.WebhookStatusFailed, states["Customer confirmation"].Status)
		assert.Zero(t, states["Customer confirmation"].AttemptCount)
		assert.Contains(t, states["Customer confirmation"].LastAttemptErr, "missing or invalid")

		captures := receiveSMTPCaptures(t, results, 1)
		assert.Equal(t, []string{
			"RCPT TO:<qa-internal@team.invalid>",
			"RCPT TO:<qa-archive@archive.invalid>",
		}, captures[0].to)
	})

	t.Run("rejects an unknown encryption mode", func(t *testing.T) {
		_, err := smtpSettings(&integrations.MailerProfile{
			SMTPHost: "smtp.example.com", SMTPPort: 25, SMTPEncryption: "magic",
		}, "from@example.com", []string{"to@example.com"})
		assert.ErrorContains(t, err, "encryption")
	})

	t.Run("rejects submitted header injection before SMTP", func(t *testing.T) {
		delivery := &forms.EmailDelivery{SubjectTemplate: "Request from {{.Fields.name}}"}
		delivery.Format = forms.EmailFormatText
		_, err := forms.RenderEmail(delivery, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now().UTC(),
			DataJSON: `{"name":"Alice\r\nBcc: hidden@example.com"}`,
		})
		assert.ErrorContains(t, err, "one non-empty line")

		_, err = buildSMTPMessage(outboundEmail{
			From: "forms@example.com", To: []string{"owner@example.com"},
			ReplyTo: "alice@example.com\r\nBcc: hidden@example.com",
			Subject: "Safe", Format: forms.EmailFormatText, TextBody: "Body",
		})
		assert.ErrorContains(t, err, "Reply-To")
	})

	t.Run("does not render an unused HTML template for text messages", func(t *testing.T) {
		delivery := &forms.EmailDelivery{
			SubjectTemplate: "Text only", TextTemplate: "Hello {{.Fields.name}}",
			HTMLTemplate: "<p>{{.Fields.missing}}</p>", Format: forms.EmailFormatText,
		}
		rendered, err := forms.RenderEmail(delivery, &forms.Submission{
			Form: &forms.Form{Name: "Contact"}, CreatedAt: time.Now().UTC(), DataJSON: `{"name":"Alice"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "Text only", rendered.Subject)
		assert.Equal(t, "Hello Alice", rendered.TextBody)
		assert.Empty(t, rendered.HTMLBody)
	})

	t.Run("retries SMTP failures and stops at the configured limit", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := listener.Addr().(*net.TCPAddr).Port
		require.NoError(t, listener.Close())

		profile := &integrations.MailerProfile{
			Name: "Unavailable", DefaultFromEmail: "forms@example.com",
			SMTPHost: "127.0.0.1", SMTPPort: port, SMTPEncryption: "none",
		}
		form := &forms.Form{Name: "Retry", Slug: "retry-email", AllowedOrigins: "*"}
		var event *forms.EmailEvent
		var delivery *forms.EmailDelivery
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			if err := tx.Create(profile).Error; err != nil {
				return err
			}
			if err := tx.Create(form).Error; err != nil {
				return err
			}
			profileID := profile.ID
			delivery = &forms.EmailDelivery{
				FormID: form.ID, Enabled: true, MailerProfileID: &profileID,
				Name: "Retry notification", RecipientSource: forms.EmailRecipientStatic,
				Recipient: "owner@example.com", ReplyToSource: forms.EmailReplyToNone,
				Format: forms.EmailFormatText,
			}
			if err := tx.Create(delivery).Error; err != nil {
				return err
			}
			submission := &forms.Submission{FormID: form.ID, DataJSON: `{}`}
			if err := tx.Create(submission).Error; err != nil {
				return err
			}
			event = forms.NewEmailEvent(submission.ID, time.Now(), delivery.ID)
			return tx.Create(event).Error
		}))

		cfg := &config.Config{}
		cfg.Webhook.RetryLimit = 2
		cfg.Webhook.BackoffSchedule = "1"
		ctx := &cartridge.JobContext{
			Context: context.Background(), DB: db,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
		dispatcher := NewEmailDispatcher(cfg)
		require.NoError(t, dispatcher.ProcessBatch(ctx))

		var stored forms.EmailEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusRetrying, stored.Status)
		assert.Equal(t, 1, stored.AttemptCount)
		require.NotNil(t, stored.NextAttemptAt)
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			return tx.Model(&forms.EmailEvent{}).Where("id = ?", event.ID).
				Update("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error
		}))

		require.NoError(t, dispatcher.ProcessBatch(ctx))
		stored = forms.EmailEvent{}
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusFailed, stored.Status)
		assert.Equal(t, 2, stored.AttemptCount)
		assert.Nil(t, stored.NextAttemptAt)
		assert.Contains(t, stored.LastAttemptErr, "connect SMTP")
	})

	t.Run("stops a blocked SMTP session when canceled", func(t *testing.T) {
		listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()
		connection := make(chan net.Conn, 1)
		go func() {
			accepted, acceptErr := listener.Accept()
			if acceptErr == nil {
				connection <- accepted
			}
		}()

		address := listener.Addr().(*net.TCPAddr)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			result <- sendSMTP(ctx, &smtpConfig{
				Host: "127.0.0.1", Port: address.Port, Encryption: "none",
				From: "forms@example.com", Recipients: []string{"owner@example.com"},
			}, []byte("test"))
		}()
		accepted := <-connection
		defer accepted.Close()
		cancel()

		select {
		case err := <-result:
			assert.ErrorIs(t, err, context.Canceled)
		case <-time.After(time.Second):
			t.Fatal("SMTP session did not stop after cancellation")
		}
	})

	t.Run("reports cancellation instead of connection teardown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := smtpIOError(ctx, "read SMTP greeting", net.ErrClosed)

		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, net.ErrClosed)
	})
}

func multipartBodies(t *testing.T, raw string) (string, string) {
	t.Helper()
	message, err := mail.ReadMessage(strings.NewReader(raw))
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/alternative", mediaType)
	reader := multipart.NewReader(message.Body, params["boundary"])

	bodies := make(map[string]string)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(part)
		require.NoError(t, err)
		partType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		require.NoError(t, err)
		bodies[partType] = string(body)
	}
	return bodies["text/plain"], bodies["text/html"]
}

func fakeSMTP(t *testing.T) (string, int, <-chan smtpServerResult) {
	t.Helper()
	return fakeSMTPConnections(t, 1)
}

func fakeSMTPConnections(t *testing.T, count int) (string, int, <-chan smtpServerResult) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	result := make(chan smtpServerResult, count)
	go func() {
		defer close(result)
		for range count {
			connection, err := listener.Accept()
			if err != nil {
				result <- smtpServerResult{err: err}
				return
			}
			capture, err := serveSMTP(connection)
			_ = connection.Close()
			result <- smtpServerResult{capture: capture, err: err}
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port, result
}

func setupTwoEmailForm(t *testing.T, db *gorm.DB, host string, port int, replyToSource, replyTo string) *forms.Form {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	captcha, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
		Name: "Turnstile", SiteKey: "site", SecretKey: "secret",
	})
	require.NoError(t, err)
	profile, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
		Name: "Local capture", DefaultFromName: "Miniform QA", DefaultFromEmail: "no-reply@sender.invalid",
		SMTPHost: host, SMTPPort: port, SMTPEncryption: "none",
	})
	require.NoError(t, err)
	form, err := forms.Create(logger, db, forms.CreateParams{
		Name: "Email integration", Slug: "email-integration", AllowedOrigins: "*",
		CaptchaProfileID: &captcha.ID,
		MailerProfileID:  &profile.ID, EmailEnabled: true, EmailName: "Internal notification",
		EmailRecipientType: forms.EmailRecipientStatic,
		EmailRecipient:     "qa-internal@team.invalid, qa-archive@archive.invalid",
		EmailReplyToType:   replyToSource, EmailReplyTo: replyTo,
		EmailSubject: "Internal · {{.Fields.name}}", EmailFormat: forms.EmailFormatHTML,
		EmailText: "Name: {{.Fields.name}}\nMessage: {{.Fields.message}}",
		EmailHTML: "<h1>{{.Fields.name}}</h1><p>{{.Fields.message}}</p>",
	})
	require.NoError(t, err)
	_, err = forms.CreateEmailDelivery(logger, db, forms.EmailDeliveryParams{
		FormID: form.ID, Name: "Customer confirmation", Enabled: true, MailerProfileID: &profile.ID,
		RecipientSource: forms.EmailRecipientField, Recipient: "email",
		ReplyToSource: forms.EmailReplyToStatic, ReplyTo: "Support <support@sender.invalid>",
		SubjectTemplate: "Confirmation · {{.Fields.name}}", Format: forms.EmailFormatText,
		TextTemplate: "We received your request, {{.Fields.name}}.",
	})
	require.NoError(t, err)
	form, err = forms.GetByID(db, form.ID)
	require.NoError(t, err)
	return form
}

func receiveSMTPCaptures(t *testing.T, results <-chan smtpServerResult, count int) []smtpCapture {
	t.Helper()
	captures := make([]smtpCapture, 0, count)
	for range count {
		select {
		case result, ok := <-results:
			require.True(t, ok)
			require.NoError(t, result.err)
			captures = append(captures, result.capture)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for local SMTP capture")
		}
	}
	return captures
}

func serveSMTP(connection net.Conn) (smtpCapture, error) {
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(connection)
	write := func(line string) error {
		_, err := fmt.Fprintf(connection, "%s\r\n", line)
		return err
	}
	if err := write("220 local SMTP ready"); err != nil {
		return smtpCapture{}, err
	}

	var capture smtpCapture
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return capture, err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			_, err = io.WriteString(connection, "250-localhost\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(line, "AUTH"):
			capture.authenticated = true
			err = write("235 authenticated")
		case strings.HasPrefix(line, "MAIL FROM:"):
			capture.from = line
			err = write("250 sender accepted")
		case strings.HasPrefix(line, "RCPT TO:"):
			capture.to = append(capture.to, line)
			err = write("250 recipient accepted")
		case line == "DATA":
			if err = write("354 send message"); err == nil {
				capture.message, err = readSMTPData(reader)
			}
			if err == nil {
				err = write("250 queued")
			}
		case line == "QUIT":
			if err = write("221 bye"); err == nil {
				return capture, nil
			}
		default:
			err = write("250 ok")
		}
		if err != nil {
			return capture, err
		}
	}
}

func readSMTPData(reader *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "." {
			return strings.Join(lines, "\r\n"), nil
		}
		lines = append(lines, line)
	}
}
