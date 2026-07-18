package jobs

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const smtpTimeout = 15 * time.Second

type smtpConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	From       string
	To         string
}

func sendSMTP(ctx context.Context, config *smtpConfig, message []byte) error {
	client, stopCancel, err := openSMTP(ctx, config)
	if err != nil {
		return err
	}
	defer stopCancel()
	defer func() { _ = client.Close() }()

	if config.Encryption == "starttls" {
		if err := client.StartTLS(&tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpIOError(ctx, "start SMTP TLS", err)
		}
	}
	if config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
			return smtpIOError(ctx, "authenticate SMTP", err)
		}
	}

	from, err := envelopeAddress(config.From)
	if err != nil {
		return fmt.Errorf("invalid sender: %w", err)
	}
	to, err := envelopeAddress(config.To)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return smtpIOError(ctx, "set SMTP sender", err)
	}
	if err := client.Rcpt(to); err != nil {
		return smtpIOError(ctx, "set SMTP recipient", err)
	}

	data, err := client.Data()
	if err != nil {
		return smtpIOError(ctx, "start SMTP data", err)
	}
	if _, err := data.Write(message); err != nil {
		_ = data.Close()
		return smtpIOError(ctx, "write SMTP data", err)
	}
	if err := data.Close(); err != nil {
		return smtpIOError(ctx, "finish SMTP data", err)
	}
	if err := client.Quit(); err != nil {
		return smtpIOError(ctx, "close SMTP session", err)
	}
	return nil
}

func openSMTP(ctx context.Context, config *smtpConfig) (*smtp.Client, func() bool, error) {
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	connection, err := (&net.Dialer{Timeout: smtpTimeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, smtpIOError(ctx, "connect SMTP", err)
	}
	rawConnection := connection
	stopCancel := context.AfterFunc(ctx, func() { _ = rawConnection.Close() })
	closeConnection := func() {
		stopCancel()
		_ = rawConnection.Close()
	}

	deadline := time.Now().Add(smtpTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		closeConnection()
		return nil, nil, smtpIOError(ctx, "set SMTP deadline", err)
	}

	if config.Encryption == "tls" {
		secure := tls.Client(connection, &tls.Config{ServerName: config.Host, MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			closeConnection()
			return nil, nil, smtpIOError(ctx, "negotiate SMTP TLS", err)
		}
		connection = secure
	}

	client, err := smtp.NewClient(connection, config.Host)
	if err != nil {
		closeConnection()
		return nil, nil, smtpIOError(ctx, "initialize SMTP", err)
	}
	return client, stopCancel, nil
}

func smtpIOError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func envelopeAddress(value string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return address.Address, nil
}

func buildSMTPMessage(from, to, subject, body string) []byte {
	var message strings.Builder
	fmt.Fprintf(&message, "From: %s\r\n", safeHeader(from))
	fmt.Fprintf(&message, "To: %s\r\n", safeHeader(to))
	fmt.Fprintf(&message, "Subject: %s\r\n", safeHeader(subject))
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	message.WriteString(crlf(body))
	message.WriteString("\r\n")
	return []byte(message.String())
}

func safeHeader(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\x00", "")), " ")
}

func crlf(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}
