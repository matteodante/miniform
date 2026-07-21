package stress_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedSMTPMessage struct {
	from, raw string
	to        []string
}

type smtpCaptureSnapshot struct {
	messages           []capturedSMTPMessage
	attempts, rejected int
	errors             []string
}

type smtpCaptureServer struct {
	listener net.Listener
	host     string
	port     int

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	wait   sync.WaitGroup

	mu                sync.Mutex
	failuresRemaining int
	messages          []capturedSMTPMessage
	attempts          int
	rejected          int
	errors            []string
}

func newSMTPCapture(t *testing.T, transientFailures int) *smtpCaptureServer {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	address := listener.Addr().(*net.TCPAddr)
	server := &smtpCaptureServer{
		listener: listener, host: "127.0.0.1", port: address.Port,
		ctx: ctx, cancel: cancel, failuresRemaining: transientFailures,
	}
	server.wait.Add(1)
	go server.accept()
	t.Cleanup(server.close)
	return server
}

func (server *smtpCaptureServer) accept() {
	defer server.wait.Done()
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			select {
			case <-server.ctx.Done():
				return
			default:
				server.recordError(fmt.Errorf("accept SMTP connection: %w", err))
				return
			}
		}
		server.wait.Add(1)
		go func() {
			defer server.wait.Done()
			defer connection.Close()
			if err := server.serve(connection); err != nil {
				server.recordError(err)
			}
		}()
	}
}

func (server *smtpCaptureServer) serve(connection net.Conn) error {
	if err := connection.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return fmt.Errorf("set SMTP capture deadline: %w", err)
	}
	reader := bufio.NewReader(connection)
	write := func(line string) error {
		_, err := fmt.Fprintf(connection, "%s\r\n", line)
		return err
	}
	if err := write("220 miniform local capture ready"); err != nil {
		return err
	}

	var message capturedSMTPMessage
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
			_, err = io.WriteString(connection, "250-localhost\r\n250 8BITMIME\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"):
			message.from, err = smtpEnvelopeAddress(line, "MAIL FROM:")
			if err == nil {
				err = write("250 sender accepted")
			}
		case strings.HasPrefix(line, "RCPT TO:"):
			var recipient string
			recipient, err = smtpEnvelopeAddress(line, "RCPT TO:")
			if err != nil {
				break
			}
			if !strings.HasSuffix(strings.ToLower(recipient), ".invalid") {
				server.recordError(fmt.Errorf("SMTP capture blocked non-test recipient %q", recipient))
				return write("550 recipient blocked by test capture")
			}
			message.to = append(message.to, recipient)
			err = write("250 recipient accepted")
		case line == "DATA":
			if err = write("354 send message"); err != nil {
				break
			}
			message.raw, err = readCapturedSMTPData(reader)
			if err != nil {
				break
			}
			if server.capture(message) {
				return write("451 temporary local test failure")
			}
			err = write("250 queued locally")
		case line == "RSET", line == "NOOP":
			err = write("250 ok")
		case line == "QUIT":
			if err = write("221 bye"); err == nil {
				return nil
			}
		default:
			err = write("250 ok")
		}
		if err != nil {
			return err
		}
	}
}

func smtpEnvelopeAddress(line, prefix string) (string, error) {
	value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	start := strings.IndexByte(value, '<')
	end := strings.IndexByte(value, '>')
	if start < 0 || end <= start+1 {
		return "", fmt.Errorf("invalid SMTP envelope command %q", line)
	}
	return value[start+1 : end], nil
}

func readCapturedSMTPData(reader *bufio.Reader) (string, error) {
	var message strings.Builder
	for message.Len() <= mebibyte {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			return message.String(), nil
		}
		if strings.HasPrefix(trimmed, "..") {
			trimmed = trimmed[1:]
		}
		message.WriteString(trimmed)
		message.WriteString("\r\n")
	}
	return "", errors.New("SMTP test message exceeds 1 MiB")
}

func (server *smtpCaptureServer) capture(message capturedSMTPMessage) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.attempts++
	if server.failuresRemaining > 0 {
		server.failuresRemaining--
		server.rejected++
		return true
	}
	message.to = append([]string(nil), message.to...)
	server.messages = append(server.messages, message)
	return false
}

func (server *smtpCaptureServer) recordError(err error) {
	server.mu.Lock()
	server.errors = append(server.errors, err.Error())
	server.mu.Unlock()
}

func (server *smtpCaptureServer) snapshot() smtpCaptureSnapshot {
	server.mu.Lock()
	defer server.mu.Unlock()
	snapshot := smtpCaptureSnapshot{
		attempts: server.attempts, rejected: server.rejected,
		errors:   append([]string(nil), server.errors...),
		messages: make([]capturedSMTPMessage, len(server.messages)),
	}
	for index, message := range server.messages {
		message.to = append([]string(nil), message.to...)
		snapshot.messages[index] = message
	}
	return snapshot
}

func (server *smtpCaptureServer) close() {
	server.once.Do(func() {
		server.cancel()
		_ = server.listener.Close()
		server.wait.Wait()
	})
}

func waitForSMTPCount(t *testing.T, process *serverProcess, server *smtpCaptureServer, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(server.snapshot().messages) >= expected {
			return
		}
		select {
		case <-process.done:
			t.Fatalf("server exited while draining email: %v", process.result())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("captured %d/%d email messages", len(server.snapshot().messages), expected)
}

func validateSMTPCapture(t *testing.T, snapshot smtpCaptureSnapshot, sequences []int) {
	t.Helper()
	expected := make(map[string]int, len(sequences)*2)
	for _, sequence := range sequences {
		expected[fmt.Sprintf("Internal %d", sequence)] = sequence
		expected[fmt.Sprintf("Customer %d", sequence)] = sequence
	}
	for _, captured := range snapshot.messages {
		assert.Equal(t, "no-reply@sender.invalid", captured.from)
		for _, recipient := range captured.to {
			assert.True(t, strings.HasSuffix(recipient, ".invalid"), "unexpected SMTP recipient %q", recipient)
		}

		message, err := mail.ReadMessage(strings.NewReader(captured.raw))
		require.NoError(t, err)
		subject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
		require.NoError(t, err)
		sequence, found := expected[subject]
		require.Truef(t, found, "unexpected or duplicate subject %q", subject)
		delete(expected, subject)

		customer := fmt.Sprintf("customer-%d@recipient.invalid", sequence)
		switch {
		case strings.HasPrefix(subject, "Internal "):
			assert.ElementsMatch(t, []string{"qa-internal@team.invalid", "qa-archive@archive.invalid"}, captured.to)
			replyTo, err := mail.ParseAddress(message.Header.Get("Reply-To"))
			require.NoError(t, err)
			assert.Equal(t, customer, replyTo.Address)
			textBody, htmlBody := capturedEmailBodies(t, message)
			assert.Contains(t, textBody, `<script>alert("smtp")</script> & "quotes"`)
			assert.Contains(t, htmlBody, "&lt;script&gt;alert(&#34;smtp&#34;)&lt;/script&gt; &amp; &#34;quotes&#34;")
			assert.NotContains(t, htmlBody, "<script>")
		case strings.HasPrefix(subject, "Customer "):
			assert.Equal(t, []string{"qa-customer@recipient.invalid"}, captured.to)
			replyTo, err := mail.ParseAddress(message.Header.Get("Reply-To"))
			require.NoError(t, err)
			assert.Equal(t, "Support", replyTo.Name)
			assert.Equal(t, "support@sender.invalid", replyTo.Address)
			textBody, htmlBody := capturedEmailBodies(t, message)
			assert.Contains(t, textBody, fmt.Sprintf("Load %d", sequence))
			assert.Empty(t, htmlBody)
		}
	}
	assert.Empty(t, expected, "missing SMTP messages")
}

func capturedEmailBodies(t *testing.T, message *mail.Message) (string, string) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	require.NoError(t, err)
	if mediaType == "text/plain" {
		body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
		require.NoError(t, err)
		return string(body), ""
	}
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
