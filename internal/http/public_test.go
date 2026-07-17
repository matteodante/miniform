package http

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

func TestPublicHelpers(t *testing.T) {
	t.Run("extracts and removes captcha token variants", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			value any
			want  string
		}{
			{"hyphen field", "token-a", "token-a"},
			{"multipart value", []string{"token-b"}, "token-b"},
			{"JSON array", []any{"token-c"}, "token-c"},
			{"bytes", []byte("token-d"), "token-d"},
			{"empty", "", ""},
		} {
			t.Run(test.name, func(t *testing.T) {
				payload := map[string]any{"cf-turnstile-response": test.value, "name": "Ada"}
				token, found := extractCaptchaToken(payload)
				assert.Equal(t, test.want, token)
				assert.Equal(t, test.want != "", found)
				assert.NotContains(t, payload, "cf-turnstile-response")
			})
		}

		payload := map[string]any{
			"cf-turnstile-response": "first", "cf_turnstile_response": "second",
		}
		token, found := extractCaptchaToken(payload)
		assert.True(t, found)
		assert.Equal(t, "first", token)
		assert.Empty(t, payload)
		token, found = extractCaptchaToken(nil)
		assert.False(t, found)
		assert.Empty(t, token)
	})

	t.Run("normalizes request origins", func(t *testing.T) {
		cases := map[string]string{
			"https://EXAMPLE.COM":                   "example.com",
			"http://example.com:8080/path?q=1#part": "example.com",
			"https://sub.example.com/path":          "sub.example.com",
			"example.com":                           "example.com",
			"[2001:db8::1]:8443":                    "2001:db8::1",
			"":                                      "",
		}
		for input, expected := range cases {
			assert.Equal(t, expected, extractDomain(input), input)
		}
	})

	t.Run("keeps repeated form values", func(t *testing.T) {
		payload := map[string]any{}
		appendFormValue(payload, "tag", "one")
		appendFormValue(payload, "tag", "two")
		appendFormValue(payload, "tag", "three")
		assert.Equal(t, []string{"one", "two", "three"}, payload["tag"])
		setFormValues(payload, "single", []string{"value"})
		assert.Equal(t, "value", payload["single"])
	})

	t.Run("validates Turnstile action and hostname", func(t *testing.T) {
		form := &forms.Form{AllowedOrigins: "example.com,*.trusted.test"}
		settings := integrations.CaptchaSettings{Required: true, Action: "contact"}

		assert.Empty(t, turnstileResultFailure(form, settings, &integrations.TurnstileResult{
			Success: true, Hostname: "forms.example.com", Action: "contact",
		}))
		assert.Equal(t, "action mismatch", turnstileResultFailure(form, settings, &integrations.TurnstileResult{
			Success: true, Hostname: "forms.example.com", Action: "login",
		}))
		assert.Equal(t, "hostname mismatch", turnstileResultFailure(form, settings, &integrations.TurnstileResult{
			Success: true, Action: "contact",
		}))
		assert.Equal(t, "hostname mismatch", turnstileResultFailure(form, settings, &integrations.TurnstileResult{
			Success: true, Hostname: "attacker.test", Action: "contact",
		}))

		form.AllowedOrigins = "*"
		assert.Empty(t, turnstileResultFailure(form, settings, &integrations.TurnstileResult{
			Success: true, Action: "contact",
		}))
	})
}
