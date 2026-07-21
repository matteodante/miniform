package forms_test

import (
	"errors"
	"testing"

	"github.com/matteodante/miniform/internal/forms"

	"github.com/stretchr/testify/assert"
)

func TestOrigins(t *testing.T) {
	t.Run("matches configured hosts", func(t *testing.T) {
		cases := []struct {
			name, allowed, host string
			want                bool
		}{
			{"empty configuration", "", "example.com", false},
			{"wildcard", "*", "anything.test", true},
			{"wildcard without request origin", "*", "", true},
			{"wildcard in list without request origin", "example.com, *", "", true},
			{"exact host", "example.com", "example.com", true},
			{"nested subdomain", "example.com", "deep.app.example.com", true},
			{"wildcard base domain", "*.example.com", "example.com", true},
			{"legacy leading-dot domain", ".example.com", "app.example.com", true},
			{"comma-separated list", "one.test, two.test", "two.test", true},
			{"full URL", "https://example.com/path", "example.com", true},
			{"IPv6 URL", "http://[::1]", "::1", true},
			{"case insensitive", "Example.COM", "EXAMPLE.com", true},
			{"lookalike prefix", "example.com", "notexample.com", false},
			{"empty host", "example.com", "", false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				form := forms.Form{AllowedOrigins: tc.allowed}
				assert.Equal(t, tc.want, form.IsOriginAllowed(tc.host))
			})
		}
	})

	t.Run("validates redirect destinations", func(t *testing.T) {
		cases := []struct {
			name, target, allowed string
			wantError             bool
			wantNotAllowed        bool
		}{
			{"empty", "", "", false, false},
			{"root-relative", "/thanks?source=form", "", false, false},
			{"path-relative", "thanks", "", false, false},
			{"parent-relative", "../thanks", "", false, false},
			{"query-relative", "?source=form", "", false, false},
			{"fragment-relative", "#complete", "", false, false},
			{"network-path", "//evil.test/thanks", "example.com", true, true},
			{"triple-slash network-path", "///evil.test/thanks", "example.com", true, true},
			{"backslash network-path", `\\evil.test/thanks`, "example.com", true, true},
			{"mixed-slash network-path", `/\evil.test/thanks`, "example.com", true, true},
			{"absolute without allowlist", "https://example.com/thanks", "", true, false},
			{"matching domain", "https://example.com/thanks", "example.com", false, false},
			{"case-insensitive scheme", "HTTPS://example.com/thanks", "example.com", false, false},
			{"matching IPv6 host", "http://[::1]/thanks", "http://[::1]", false, false},
			{"matching subdomain and port", "https://app.example.com:8443/thanks", "example.com", false, false},
			{"second allowed host", "https://two.test/thanks", "one.test, two.test", false, false},
			{"wildcard cannot authorize an absolute redirect", "https://evil.test/thanks", "*", true, true},
			{"different host", "https://evil.test/thanks", "example.com", true, true},
			{"malformed URL", "://invalid", "example.com", true, false},
			{"absolute scheme without host", "https:evil.test/thanks", "*", true, true},
			{"non HTTP scheme", "javascript:alert(1)", "example.com", true, true},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				form := forms.Form{AllowedOrigins: tc.allowed}
				err := form.ValidateRedirectURL(tc.target)
				if tc.wantError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
				assert.Equal(t, tc.wantNotAllowed, errors.Is(err, forms.ErrRedirectNotAllowed))
			})
		}
	})
}
