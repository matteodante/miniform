package embed_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/forms"
	formembed "github.com/matteodante/miniform/internal/forms/embed"
	"github.com/matteodante/miniform/internal/integrations"
)

func TestBuild(t *testing.T) {
	t.Run("redacts token and follows SDK option", func(t *testing.T) {
		form := &forms.Form{ID: 7, PublicID: "public-id", Slug: "contact", Token: "live-token"}

		result := formembed.Build(form, formembed.Options{
			BaseURL: "https://forms.example.com/", IncludeSDK: true,
		})

		assert.True(t, result.Redacted)
		assert.True(t, result.IncludesSDK)
		assert.Equal(t, "https://forms.example.com/forms/contact/submit?token=YOUR_FORM_TOKEN", result.Action)
		assert.NotContains(t, result.HTML, "live-token")
		assert.Contains(t, result.HTML, `<script src="https://forms.example.com/assets/miniform.js"></script>`)
	})

	t.Run("normalizes generated HTML with the live action", func(t *testing.T) {
		form := &forms.Form{
			ID:            8,
			PublicID:      "public-id",
			Slug:          "feedback",
			Token:         "live-token",
			GeneratedHTML: `<section><form action="old" method="GET"><button>Send</button></form></section>`,
		}

		result := formembed.Build(form, formembed.Options{BaseURL: "https://forms.example.com", ShowToken: true})

		require.Empty(t, result.Warning)
		assert.False(t, result.Redacted)
		assert.Contains(t, result.HTML, `action="https://forms.example.com/forms/feedback/submit?token=live-token"`)
		assert.Contains(t, result.HTML, `method="POST"`)
		assert.Contains(t, result.HTML, `data-form-id="8"`)
	})

	t.Run("injects assigned Turnstile credentials with the fixed action", func(t *testing.T) {
		profileID := uint(4)
		form := &forms.Form{
			ID:               9,
			Slug:             "secure",
			Token:            "token",
			CaptchaProfileID: &profileID,
			CaptchaProfile: &integrations.CaptchaProfile{
				SiteKey:   "public-site-key",
				SecretKey: "secret-key",
			},
		}

		result := formembed.Build(form, formembed.Options{BaseURL: "https://forms.example.com", ShowToken: true})

		assert.Contains(t, result.HTML, `data-sitekey="public-site-key"`)
		assert.Contains(t, result.HTML, `data-action="submit"`)
		assert.Contains(t, result.HTML, "challenges.cloudflare.com/turnstile")
	})

	t.Run("does not inject Turnstile without an assigned profile", func(t *testing.T) {
		result := formembed.Build(&forms.Form{ID: 10, Slug: "plain", Token: "token"}, formembed.Options{
			BaseURL: "https://forms.example.com", ShowToken: true,
		})

		assert.NotContains(t, result.HTML, "cf-turnstile")
		assert.NotContains(t, result.HTML, "challenges.cloudflare.com/turnstile")
	})
}
