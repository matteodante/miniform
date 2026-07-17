package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnstileVerifier(t *testing.T) {
	t.Run("sends all verification fields", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				http.Error(response, err.Error(), http.StatusBadRequest)
				return
			}
			assert.Equal(t, "secret", request.Form.Get("secret"))
			assert.Equal(t, "token", request.Form.Get("response"))
			assert.Equal(t, "127.0.0.1", request.Form.Get("remoteip"))
			assert.NotEmpty(t, request.Form.Get("idempotency_key"))
			assert.NoError(t, json.NewEncoder(response).Encode(turnstileResponse{
				Success: true, Hostname: "example.com", Action: "submit",
			}))
		}))
		defer server.Close()

		result, err := testVerifier(server.URL).verify(context.Background(), "secret", "token", "127.0.0.1")
		require.NoError(t, err)
		assert.Equal(t, &TurnstileResult{Success: true, Hostname: "example.com", Action: "submit"}, result)
	})

	t.Run("reports provider rejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			assert.NoError(t, json.NewEncoder(response).Encode(turnstileResponse{
				ErrorCodes: []string{"invalid-input-response", "timeout-or-duplicate"},
			}))
		}))
		defer server.Close()

		result, err := testVerifier(server.URL).verify(context.Background(), "secret", "token", "")
		assert.Nil(t, result)
		assert.ErrorContains(t, err, "invalid-input-response")
	})

	t.Run("retries server failures with one idempotency key", func(t *testing.T) {
		var attempts atomic.Int32
		var idempotencyKey string
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				http.Error(response, err.Error(), http.StatusBadRequest)
				return
			}
			if idempotencyKey == "" {
				idempotencyKey = request.Form.Get("idempotency_key")
			}
			assert.Equal(t, idempotencyKey, request.Form.Get("idempotency_key"))
			if attempts.Add(1) < 3 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			assert.NoError(t, json.NewEncoder(response).Encode(turnstileResponse{Success: true}))
		}))
		defer server.Close()

		result, err := testVerifier(server.URL).verify(context.Background(), "secret", "token", "")
		require.NoError(t, err)
		assert.True(t, result.Success)
		assert.Equal(t, int32(3), attempts.Load())
	})

	t.Run("does not retry client failures", func(t *testing.T) {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			response.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		_, err := testVerifier(server.URL).verify(context.Background(), "secret", "token", "")
		assert.ErrorContains(t, err, "status 400")
		assert.Equal(t, int32(1), attempts.Load())
	})

	t.Run("exhausts transient retries", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		_, err := testVerifier(server.URL).verify(context.Background(), "secret", "token", "")
		assert.ErrorIs(t, err, ErrTurnstileUnavailable)
	})

	t.Run("stops retry when context is cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := testVerifier(server.URL).verify(ctx, "secret", "token", "")
		assert.True(t, errors.Is(err, context.Canceled))
	})
}

func testVerifier(endpoint string) turnstileVerifier {
	return turnstileVerifier{
		endpoint: endpoint, client: &http.Client{Timeout: time.Second}, attempts: 3, retryDelay: time.Millisecond,
	}
}
