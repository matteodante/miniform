package jobs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	from, to, message string
	authenticated     bool
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
			From: "Forms <forms@example.com>", To: "owner@example.com",
		}
		message := buildSMTPMessage(settings.From, settings.To, "Hello\r\nBcc: hidden@example.com", "line one\nline two")
		require.NoError(t, sendSMTP(context.Background(), settings, message))

		server := <-result
		require.NoError(t, server.err)
		assert.True(t, server.capture.authenticated)
		assert.Contains(t, server.capture.from, "forms@example.com")
		assert.Contains(t, server.capture.to, "owner@example.com")
		assert.Contains(t, server.capture.message, "Subject: Hello Bcc: hidden@example.com")
		assert.NotContains(t, server.capture.message, "\r\nBcc:")
		assert.Contains(t, server.capture.message, "line one\r\nline two")
	})

	t.Run("delivers a queued submission through SMTP", func(t *testing.T) {
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
				OverridesJSON: `{"to":"owner@example.com"}`,
			}).Error; err != nil {
				return err
			}
			submission := &forms.Submission{FormID: form.ID, DataJSON: `{"name":"Alice"}`}
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
		assert.Contains(t, server.capture.message, "Alice")
	})

	t.Run("rejects an unknown encryption mode", func(t *testing.T) {
		_, err := smtpSettings(&integrations.MailerProfile{
			SMTPHost: "smtp.example.com", SMTPPort: 25, SMTPEncryption: "magic",
		}, "from@example.com", "to@example.com")
		assert.ErrorContains(t, err, "encryption")
	})
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
			capture.to = line
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
