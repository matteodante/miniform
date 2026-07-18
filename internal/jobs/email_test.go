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
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			if err := tx.Create(profile).Error; err != nil {
				return err
			}
			if err := tx.Create(form).Error; err != nil {
				return err
			}
			profileID := profile.ID
			if err := tx.Create(&forms.EmailDelivery{
				FormID: form.ID, Enabled: true, MailerProfileID: &profileID,
				Recipient: "owner@example.com, archive@example.com", Format: forms.EmailFormatHTML,
			}).Error; err != nil {
				return err
			}
			submission := &forms.Submission{FormID: form.ID, DataJSON: `{"name":"Alice","message":"<script>alert('x')</script>"}`}
			if err := tx.Create(submission).Error; err != nil {
				return err
			}
			event = forms.NewEmailEvent(submission.ID, time.Now())
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
		plain, html := multipartBodies(t, server.capture.message)
		assert.Contains(t, plain, "Alice")
		assert.Contains(t, plain, "<script>alert('x')</script>")
		assert.Contains(t, html, "Alice")
		assert.Contains(t, html, "&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;")
		assert.NotContains(t, html, "<script>alert")
	})

	t.Run("rejects an unknown encryption mode", func(t *testing.T) {
		_, err := smtpSettings(&integrations.MailerProfile{
			SMTPHost: "smtp.example.com", SMTPPort: 25, SMTPEncryption: "magic",
		}, "from@example.com", []string{"to@example.com"})
		assert.ErrorContains(t, err, "encryption")
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
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			if err := tx.Create(profile).Error; err != nil {
				return err
			}
			if err := tx.Create(form).Error; err != nil {
				return err
			}
			profileID := profile.ID
			if err := tx.Create(&forms.EmailDelivery{
				FormID: form.ID, Enabled: true, MailerProfileID: &profileID,
				Recipient: "owner@example.com", Format: forms.EmailFormatText,
			}).Error; err != nil {
				return err
			}
			submission := &forms.Submission{FormID: form.ID, DataJSON: `{}`}
			if err := tx.Create(submission).Error; err != nil {
				return err
			}
			event = forms.NewEmailEvent(submission.ID, time.Now())
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
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	result := make(chan smtpServerResult, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			result <- smtpServerResult{err: err}
			return
		}
		defer connection.Close()
		capture, err := serveSMTP(connection)
		result <- smtpServerResult{capture: capture, err: err}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port, result
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
