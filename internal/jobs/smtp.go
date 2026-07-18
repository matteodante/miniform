package jobs

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
)

const smtpTimeout = 15 * time.Second

type smtpConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string
	From       string
	Recipients []string
}

type outboundEmail struct {
	From     string
	To       []string
	Subject  string
	Format   string
	TextBody string
	HTMLBody string
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
	if len(config.Recipients) == 0 {
		return fmt.Errorf("recipient missing")
	}
	recipients := make([]string, len(config.Recipients))
	for i := range config.Recipients {
		recipients[i], err = envelopeAddress(config.Recipients[i])
		if err != nil {
			return fmt.Errorf("invalid recipient: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return smtpIOError(ctx, "set SMTP sender", err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return smtpIOError(ctx, "set SMTP recipient", err)
		}
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

func buildSMTPMessage(email outboundEmail) ([]byte, error) {
	format, err := forms.NormalizeEmailFormat(email.Format)
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	contentType := "text/plain; charset=utf-8"
	transferEncoding := "quoted-printable"
	if format == forms.EmailFormatHTML {
		writer := multipart.NewWriter(&body)
		contentType = mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": writer.Boundary()})
		transferEncoding = ""
		if err := writeEmailPart(writer, "text/plain; charset=utf-8", email.TextBody); err != nil {
			return nil, err
		}
		if err := writeEmailPart(writer, "text/html; charset=utf-8", email.HTMLBody); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("finish multipart email: %w", err)
		}
	} else if err := writeQuotedPrintable(&body, email.TextBody); err != nil {
		return nil, err
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", safeHeader(email.From))
	fmt.Fprintf(&message, "To: %s\r\n", recipientHeader(email.To))
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", safeHeader(email.Subject)))
	fmt.Fprintf(&message, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	message.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: %s\r\n", contentType)
	if transferEncoding != "" {
		fmt.Fprintf(&message, "Content-Transfer-Encoding: %s\r\n", transferEncoding)
	}
	message.WriteString("\r\n")
	message.Write(body.Bytes())
	if !bytes.HasSuffix(body.Bytes(), []byte("\r\n")) {
		message.WriteString("\r\n")
	}
	return message.Bytes(), nil
}

func writeEmailPart(writer *multipart.Writer, contentType, body string) error {
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return fmt.Errorf("create email part: %w", err)
	}
	return writeQuotedPrintable(part, body)
}

func writeQuotedPrintable(writer io.Writer, body string) error {
	encoded := quotedprintable.NewWriter(writer)
	if _, err := encoded.Write([]byte(crlf(body))); err != nil {
		_ = encoded.Close()
		return fmt.Errorf("encode email body: %w", err)
	}
	if err := encoded.Close(); err != nil {
		return fmt.Errorf("finish email body: %w", err)
	}
	return nil
}

func safeHeader(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\x00", "")), " ")
}

func recipientHeader(recipients []string) string {
	safe := make([]string, len(recipients))
	for i := range recipients {
		safe[i] = safeHeader(recipients[i])
	}
	return strings.Join(safe, ",\r\n\t")
}

func crlf(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}
