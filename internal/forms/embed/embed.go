// Package embed produces copyable form markup from a stored endpoint.
package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const SDKScriptTag = `<!-- Optional Miniform SDK: retry and pending-state helpers -->
<script src="/assets/miniform.js"></script>`

type Options struct {
	ShowToken  bool
	IncludeSDK bool
}

type Result struct {
	Action      string
	HTML        string
	IncludesSDK bool
	Redacted    bool
	Warning     string
}

func Build(form *forms.Form, options Options) Result {
	if form == nil {
		return Result{Warning: "form context missing"}
	}

	view := *form
	if !options.ShowToken {
		view.Token = "YOUR_FORM_TOKEN"
	}
	action := formAction(view.Slug, view.Token)
	result := Result{Action: action, IncludesSDK: options.IncludeSDK, Redacted: !options.ShowToken}

	if strings.TrimSpace(view.GeneratedHTML) == "" {
		result.HTML = defaultMarkup(&view, action)
	} else {
		result.HTML, result.Warning = rewriteForm(view.GeneratedHTML, &view, action)
	}
	result.HTML = addTurnstile(result.HTML, turnstileFor(&view))
	if options.IncludeSDK {
		result.HTML = appendBlock(result.HTML, SDKScriptTag)
	}
	return result
}

func defaultMarkup(form *forms.Form, action string) string {
	opening := htmlnode.Token{Type: htmlnode.StartTagToken, DataAtom: atom.Form, Data: "form"}
	setAttribute(&opening, "action", action)
	setAttribute(&opening, "method", "POST")
	setAttribute(&opening, "data-form-id", fmt.Sprint(form.ID))
	if form.PublicID != "" {
		setAttribute(&opening, "data-form-public-id", form.PublicID)
	}
	if form.Token != "" {
		setAttribute(&opening, "data-form-token", form.Token)
	}

	return opening.String() + `
  <label>Name
    <input type="text" name="name" required>
  </label>
  <label>Email
    <input type="email" name="email" required>
  </label>
  <label>Message
    <textarea name="message" rows="4"></textarea>
  </label>
  <button type="submit">Send</button>
</form>`
}

func rewriteForm(source string, form *forms.Form, action string) (string, string) {
	start, end, token, err := firstFormTag(source)
	if err != nil {
		return source, err.Error()
	}
	setAttribute(&token, "action", action)
	setAttribute(&token, "method", "POST")
	setAttribute(&token, "data-form-id", fmt.Sprint(form.ID))
	if form.PublicID != "" {
		setAttribute(&token, "data-form-public-id", form.PublicID)
	}
	if form.Token != "" {
		setAttribute(&token, "data-form-token", form.Token)
	}
	return source[:start] + token.String() + source[end:], ""
}

func firstFormTag(source string) (int, int, htmlnode.Token, error) {
	tokenizer := htmlnode.NewTokenizer(strings.NewReader(source))
	offset := 0
	for {
		kind := tokenizer.Next()
		raw := tokenizer.Raw()
		start := offset
		offset += len(raw)
		switch kind {
		case htmlnode.ErrorToken:
			if !errors.Is(tokenizer.Err(), io.EOF) {
				return 0, 0, htmlnode.Token{}, fmt.Errorf("parse generated HTML: %w", tokenizer.Err())
			}
			return 0, 0, htmlnode.Token{}, errors.New("form tag not found in generated HTML")
		case htmlnode.StartTagToken:
			token := tokenizer.Token()
			if token.DataAtom == atom.Form || strings.EqualFold(token.Data, "form") {
				return start, offset, token, nil
			}
		}
	}
}

func setAttribute(token *htmlnode.Token, name, value string) {
	for index := range token.Attr {
		if token.Attr[index].Namespace == "" && strings.EqualFold(token.Attr[index].Key, name) {
			token.Attr[index].Key = name
			token.Attr[index].Val = value
			return
		}
	}
	token.Attr = append(token.Attr, htmlnode.Attribute{Key: name, Val: value})
}

type captchaMarkup struct {
	widget string
	script string
}

func turnstileFor(form *forms.Form) *captchaMarkup {
	if form.CaptchaProfileID == nil || form.CaptchaProfile == nil ||
		!strings.EqualFold(strings.TrimSpace(form.CaptchaProfile.Provider), "turnstile") {
		return nil
	}

	settings := integrations.ResolveCaptchaSettings(form.CaptchaProfile.PolicyJSON, form.CaptchaOverridesJSON)
	if settings.SiteKey == "" {
		settings.SiteKey = chooseSiteKey(form.AllowedOrigins, decodeSiteKeys(form.CaptchaProfile.SiteKeysJSON))
	}
	if settings.SiteKey == "" {
		settings.SiteKey = "YOUR_TURNSTILE_SITE_KEY"
	}

	widget := htmlnode.Token{Type: htmlnode.StartTagToken, Data: "div", Attr: []htmlnode.Attribute{
		{Key: "class", Val: "cf-turnstile"},
		{Key: "data-sitekey", Val: settings.SiteKey},
	}}
	for _, attribute := range []struct{ name, value string }{
		{"data-action", settings.Action}, {"data-theme", settings.Theme},
		{"data-language", settings.Language}, {"data-size", settings.Size},
	} {
		if attribute.value != "" {
			setAttribute(&widget, attribute.name, attribute.value)
		}
	}
	return &captchaMarkup{
		widget: `  <div class="miniform-captcha-block">` + widget.String() + `</div>`,
		script: `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>`,
	}
}

func addTurnstile(source string, captcha *captchaMarkup) string {
	if captcha == nil {
		return source
	}
	if position := closingFormOffset(source); position >= 0 {
		source = strings.TrimRight(source[:position], "\n") + "\n" + captcha.widget + "\n" + source[position:]
	} else {
		source = appendBlock(source, captcha.widget)
	}
	return appendBlock(source, captcha.script)
}

func closingFormOffset(source string) int {
	tokenizer := htmlnode.NewTokenizer(strings.NewReader(source))
	offset := 0
	for {
		kind := tokenizer.Next()
		raw := tokenizer.Raw()
		start := offset
		offset += len(raw)
		if kind == htmlnode.ErrorToken {
			return -1
		}
		if kind == htmlnode.EndTagToken {
			token := tokenizer.Token()
			if token.DataAtom == atom.Form || strings.EqualFold(token.Data, "form") {
				return start
			}
		}
	}
}

func appendBlock(source, block string) string {
	if strings.TrimSpace(block) == "" {
		return source
	}
	return strings.TrimRight(source, "\n") + "\n" + strings.TrimSpace(block)
}

type siteKey struct {
	HostPattern string `json:"host_pattern"`
	SiteKey     string `json:"site_key"`
}

func decodeSiteKeys(raw string) []siteKey {
	var keys []siteKey
	if json.Unmarshal([]byte(raw), &keys) != nil {
		return nil
	}
	return keys
}

func chooseSiteKey(origins string, keys []siteKey) string {
	if origins = strings.TrimSpace(origins); origins != "" && origins != "*" {
		for _, origin := range strings.Split(origins, ",") {
			pattern := normalizedHostPattern(origin)
			if strings.HasPrefix(pattern, "*.") {
				if key := keyForWildcard(strings.TrimPrefix(pattern, "*."), keys); key != "" {
					return key
				}
				continue
			}
			if key := keyForHost(originHost(origin), keys); key != "" {
				return key
			}
		}
	}
	for _, pattern := range []string{"*", ""} {
		for _, key := range keys {
			if strings.TrimSpace(key.SiteKey) != "" && (pattern == "" || strings.TrimSpace(key.HostPattern) == pattern) {
				return strings.TrimSpace(key.SiteKey)
			}
		}
	}
	return ""
}

func keyForWildcard(base string, keys []siteKey) string {
	for _, entry := range keys {
		pattern := normalizedHostPattern(entry.HostPattern)
		key := strings.TrimSpace(entry.SiteKey)
		if key == "" || !strings.HasPrefix(pattern, "*.") {
			continue
		}
		keyBase := strings.TrimPrefix(pattern, "*.")
		if base == keyBase || strings.HasSuffix(base, "."+keyBase) {
			return key
		}
	}
	return ""
}

func keyForHost(host string, keys []siteKey) string {
	for _, entry := range keys {
		pattern := strings.ToLower(strings.TrimSpace(entry.HostPattern))
		key := strings.TrimSpace(entry.SiteKey)
		if host == "" || pattern == "" || key == "" {
			continue
		}
		if pattern == "*" || pattern == host {
			return key
		}
		if strings.HasPrefix(pattern, "*.") || strings.HasPrefix(pattern, ".") {
			base := strings.TrimPrefix(strings.TrimPrefix(pattern, "*."), ".")
			if host == base || strings.HasSuffix(host, "."+base) {
				return key
			}
		}
	}
	return ""
}

func originHost(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	if !strings.Contains(origin, "://") {
		origin = "//" + origin
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
}

func normalizedHostPattern(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	wildcard := strings.HasPrefix(value, "*.")
	if wildcard {
		value = strings.TrimPrefix(value, "*.")
	}
	host := originHost(value)
	if host == "" {
		return ""
	}
	if wildcard {
		return "*." + host
	}
	return host
}

func formAction(slug, token string) string {
	if slug = strings.TrimSpace(slug); slug == "" {
		slug = "your-form"
	}
	if token = strings.TrimSpace(token); token == "" {
		token = "YOUR_FORM_TOKEN"
	}
	return "/forms/" + url.PathEscape(slug) + "/submit?token=" + url.QueryEscape(token)
}
