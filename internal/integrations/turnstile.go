package integrations

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var ErrTurnstileUnavailable = errors.New("turnstile service temporarily unavailable")

type TurnstileResult struct {
	Success  bool
	Hostname string
	Action   string
}

type turnstileResponse struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

type turnstileVerifier struct {
	endpoint   string
	client     *http.Client
	attempts   int
	retryDelay time.Duration
}

var defaultTurnstileVerifier = turnstileVerifier{
	endpoint:   turnstileEndpoint,
	client:     &http.Client{Timeout: 10 * time.Second},
	attempts:   3,
	retryDelay: 500 * time.Millisecond,
}

func VerifyTurnstileToken(ctx context.Context, secret, token, remoteIP string) (*TurnstileResult, error) {
	return defaultTurnstileVerifier.verify(ctx, secret, token, remoteIP)
}

func (verifier turnstileVerifier) verify(ctx context.Context, secret, token, remoteIP string) (*TurnstileResult, error) {
	idempotencyKey, err := randomUUID()
	if err != nil {
		return nil, fmt.Errorf("create turnstile idempotency key: %w", err)
	}

	values := url.Values{
		"secret":          {secret},
		"response":        {token},
		"idempotency_key": {idempotencyKey},
	}
	if remoteIP != "" {
		values.Set("remoteip", remoteIP)
	}

	var lastErr error
	for attempt := 0; attempt < verifier.attempts; attempt++ {
		result, retry, err := verifier.request(ctx, values.Encode())
		if err == nil {
			return result, nil
		}
		if !retry || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		lastErr = err
		if attempt+1 < verifier.attempts {
			if err := waitForRetry(ctx, verifier.retryDelay*time.Duration(1<<attempt)); err != nil {
				return nil, err
			}
		}
	}

	return nil, fmt.Errorf("%w: %w", ErrTurnstileUnavailable, lastErr)
}

func (verifier turnstileVerifier) request(ctx context.Context, encodedForm string) (*TurnstileResult, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, verifier.endpoint, strings.NewReader(encodedForm))
	if err != nil {
		return nil, false, fmt.Errorf("create turnstile request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := verifier.client.Do(request)
	if err != nil {
		return nil, true, fmt.Errorf("turnstile request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= http.StatusInternalServerError {
		return nil, true, fmt.Errorf("turnstile returned status %d", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("turnstile returned status %d", response.StatusCode)
	}

	var payload turnstileResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("decode turnstile response: %w", err)
	}
	if !payload.Success {
		if len(payload.ErrorCodes) == 0 {
			return nil, false, errors.New("turnstile verification failed")
		}
		return nil, false, fmt.Errorf("turnstile verification failed: %s", strings.Join(payload.ErrorCodes, ", "))
	}

	return &TurnstileResult{Success: true, Hostname: payload.Hostname, Action: payload.Action}, false, nil
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}
