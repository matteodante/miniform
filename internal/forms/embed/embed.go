// Package embed builds the copyable HTML snippet for a configured form.
package embed

import (
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"strings"

	"github.com/matteodante/miniform/internal/forms"
	htmlnode "golang.org/x/net/html"
)

const SDKScriptTag = `<!-- Miniform SDK: adds automatic retry & loading states -->
<script src="/assets/miniform.js"></script>`

// Options controls secret disclosure and optional SDK inclusion.
type Options struct {
	ShowToken  bool
	IncludeSDK bool
}

// Result contains the resolved form action and copyable HTML.
type Result struct {
	Action      string
	HTML        string
	IncludesSDK bool
	Redacted    bool
	Warning     string
}

// Build renders the same form snippet exposed by the Web UI.
func Build(form *forms.Form, options Options) Result {
	if form == nil {
		return Result{Warning: "form context missing"}
	}

	formCopy := *form
	if !options.ShowToken {
		formCopy.Token = "YOUR_FORM_TOKEN"
	}
	action := liveFormAction(formCopy.Slug, formCopy.Token)
	captcha := buildCaptchaEmbed(&formCopy)
	result := Result{
		Action:      action,
		IncludesSDK: options.IncludeSDK,
		Redacted:    !options.ShowToken,
	}

	if strings.TrimSpace(formCopy.GeneratedHTML) != "" {
		prepared, err := normalizeFormHTML(formCopy.GeneratedHTML, action, &formCopy)
		if err != nil {
			result.Warning = err.Error()
			prepared = formCopy.GeneratedHTML
		}
		result.HTML = injectCaptchaSnippet(prepared, captcha)
	} else {
		result.HTML = buildDefaultFormCode(action, &formCopy, captcha)
	}

	if options.IncludeSDK {
		result.HTML = strings.TrimRight(result.HTML, "\n") + "\n\n" + SDKScriptTag
	}
	return result
}

func buildDefaultFormCode(actionURL string, form *forms.Form, captcha *captchaEmbed) string {
	publicAttr := ""
	if publicID := strings.TrimSpace(form.PublicID); publicID != "" {
		publicAttr = fmt.Sprintf(` data-form-public-id="%s"`, htmlstd.EscapeString(publicID))
	}
	tokenAttr := ""
	if token := strings.TrimSpace(form.Token); token != "" {
		tokenAttr = fmt.Sprintf(` data-form-token="%s"`, htmlstd.EscapeString(token))
	}

	baseForm := fmt.Sprintf(`<form action="%s" method="POST" data-form-id="%d"%s%s>
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
</form>`, htmlstd.EscapeString(actionURL), form.ID, publicAttr, tokenAttr)

	return injectCaptchaSnippet(baseForm, captcha)
}

func normalizeFormHTML(rawHTML, actionURL string, form *forms.Form) (string, error) {
	if strings.TrimSpace(rawHTML) == "" {
		return "", errors.New("generated HTML is empty")
	}
	start, end := findFormTagBounds(rawHTML)
	if start == -1 || end == -1 {
		return rawHTML, errors.New("form tag not found in generated HTML")
	}

	opening := rawHTML[start : end+1]
	rewritten, err := rewriteOpeningFormTag(opening, actionURL, form)
	if err != nil {
		return rawHTML, err
	}
	return rawHTML[:start] + rewritten + rawHTML[end+1:], nil
}

func rewriteOpeningFormTag(tag, actionURL string, form *forms.Form) (string, error) {
	nodes, err := htmlnode.ParseFragment(strings.NewReader(tag+"</form>"), nil)
	if err != nil {
		return "", err
	}
	var formNode *htmlnode.Node
	for _, node := range nodes {
		formNode = findFormNode(node)
		if formNode != nil {
			break
		}
	}
	if formNode == nil {
		return "", errors.New("form node missing in fragment")
	}

	setOrAddAttr(formNode, "action", actionURL)
	setOrAddAttr(formNode, "method", "POST")
	setOrAddAttr(formNode, "data-form-id", fmt.Sprintf("%d", form.ID))
	if publicID := strings.TrimSpace(form.PublicID); publicID != "" {
		setOrAddAttr(formNode, "data-form-public-id", publicID)
	}
	if token := strings.TrimSpace(form.Token); token != "" {
		setOrAddAttr(formNode, "data-form-token", token)
	}
	return serializeOpeningTag(formNode), nil
}

func findFormNode(node *htmlnode.Node) *htmlnode.Node {
	if node == nil {
		return nil
	}
	if node.Type == htmlnode.ElementNode && strings.EqualFold(node.Data, "form") {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findFormNode(child); found != nil {
			return found
		}
	}
	return nil
}

func setOrAddAttr(node *htmlnode.Node, key, value string) {
	for i := range node.Attr {
		attr := &node.Attr[i]
		if attr.Namespace == "" && strings.EqualFold(attr.Key, key) {
			attr.Key = key
			attr.Val = value
			return
		}
	}
	node.Attr = append(node.Attr, htmlnode.Attribute{Key: key, Val: value})
}

func serializeOpeningTag(node *htmlnode.Node) string {
	var builder strings.Builder
	builder.WriteString("<")
	builder.WriteString(node.Data)
	for _, attr := range node.Attr {
		if attr.Key == "" {
			continue
		}
		builder.WriteString(" ")
		if attr.Namespace != "" {
			builder.WriteString(attr.Namespace)
			builder.WriteString(":")
		}
		builder.WriteString(attr.Key)
		builder.WriteString(`="`)
		builder.WriteString(htmlstd.EscapeString(attr.Val))
		builder.WriteString(`"`)
	}
	builder.WriteString(">")
	return builder.String()
}

func findFormTagBounds(input string) (int, int) {
	lower := strings.ToLower(input)
	start := strings.Index(lower, "<form")
	if start == -1 {
		return -1, -1
	}

	var quote byte
	for i := start; i < len(input); i++ {
		character := input[i]
		if quote != 0 {
			if character == quote {
				quote = 0
			} else if character == '\\' && i+1 < len(input) {
				i++
			}
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
			continue
		}
		if character == '>' {
			return start, i
		}
	}
	return start, -1
}

type captchaEmbed struct {
	WidgetMarkup string
	ScriptTag    string
}

func injectCaptchaSnippet(html string, captcha *captchaEmbed) string {
	if captcha == nil {
		return html
	}
	widget := strings.TrimSpace(captcha.WidgetMarkup)
	script := strings.TrimSpace(captcha.ScriptTag)
	if widget == "" && script == "" {
		return html
	}

	closeIndex := strings.Index(strings.ToLower(html), "</form>")
	if closeIndex == -1 {
		parts := []string{strings.TrimRight(html, "\n")}
		if widget != "" {
			parts = append(parts, widget)
		}
		if script != "" {
			parts = append(parts, script)
		}
		return strings.Join(parts, "\n")
	}

	before := html[:closeIndex]
	after := html[closeIndex:]
	if widget != "" {
		before = strings.TrimRight(before, "\n") + "\n" + widget + "\n"
	}
	result := before + after
	if script != "" {
		result = strings.TrimRight(result, "\n") + "\n" + script
	}
	return result
}

func buildCaptchaEmbed(form *forms.Form) *captchaEmbed {
	if form.CaptchaProfileID == nil || form.CaptchaProfile == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(form.CaptchaProfile.Provider), "turnstile") {
		return nil
	}
	return buildTurnstileEmbed(form)
}

func buildTurnstileEmbed(form *forms.Form) *captchaEmbed {
	profile := form.CaptchaProfile
	policy := parseCaptchaPolicy(profile.PolicyJSON, form.CaptchaOverridesJSON)
	siteKey := strings.TrimSpace(policy.SiteKey)
	if siteKey == "" {
		siteKey = selectCaptchaSiteKey(form, parseCaptchaSiteKeys(profile.SiteKeysJSON))
	}
	if siteKey == "" {
		siteKey = "YOUR_TURNSTILE_SITE_KEY"
	}

	attrs := []string{fmt.Sprintf(`data-sitekey="%s"`, htmlstd.EscapeString(siteKey))}
	if policy.Action != "" {
		attrs = append(attrs, fmt.Sprintf(`data-action="%s"`, htmlstd.EscapeString(policy.Action)))
	}
	if policy.Theme != "" {
		attrs = append(attrs, fmt.Sprintf(`data-theme="%s"`, htmlstd.EscapeString(policy.Theme)))
	}
	if policy.Language != "" {
		attrs = append(attrs, fmt.Sprintf(`data-language="%s"`, htmlstd.EscapeString(policy.Language)))
	}
	if policy.Size != "" {
		attrs = append(attrs, fmt.Sprintf(`data-size="%s"`, htmlstd.EscapeString(policy.Size)))
	} else if strings.EqualFold(policy.Widget, "invisible") {
		attrs = append(attrs, `data-size="invisible"`)
	}

	return &captchaEmbed{
		WidgetMarkup: fmt.Sprintf(`    <div class="miniform-captcha-block">
        <div class="cf-turnstile" %s></div>
    </div>`, strings.Join(attrs, " ")),
		ScriptTag: `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>`,
	}
}

type captchaSiteKeyEntry struct {
	HostPattern string `json:"host_pattern"`
	SiteKey     string `json:"site_key"`
}

func parseCaptchaSiteKeys(raw string) []captchaSiteKeyEntry {
	var entries []captchaSiteKeyEntry
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &entries) != nil {
		return nil
	}
	return entries
}

type captchaPolicy struct {
	Action   string
	Theme    string
	Language string
	Widget   string
	Size     string
	SiteKey  string
}

func parseCaptchaPolicy(baseJSON, overrideJSON string) captchaPolicy {
	policy := captchaPolicy{Action: "submit", Theme: "auto"}
	policy = applyPolicyJSON(policy, baseJSON)
	return applyPolicyJSON(policy, overrideJSON)
}

func applyPolicyJSON(policy captchaPolicy, raw string) captchaPolicy {
	var data map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &data) != nil {
		return policy
	}
	apply := func(key string, target *string) {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			*target = value
		}
	}
	apply("action", &policy.Action)
	apply("theme", &policy.Theme)
	apply("language", &policy.Language)
	apply("widget", &policy.Widget)
	apply("size", &policy.Size)
	apply("site_key", &policy.SiteKey)
	return policy
}

func selectCaptchaSiteKey(form *forms.Form, entries []captchaSiteKeyEntry) string {
	allowedOrigins := strings.TrimSpace(form.AllowedOrigins)
	if allowedOrigins != "" && allowedOrigins != "*" {
		for _, origin := range strings.Split(allowedOrigins, ",") {
			origin = strings.TrimSpace(origin)
			if origin == "" || strings.Contains(origin, "*") {
				continue
			}
			if key := findSiteKeyForHost(extractDomain(origin), entries); key != "" {
				return key
			}
		}
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.HostPattern) == "*" && strings.TrimSpace(entry.SiteKey) != "" {
			return entry.SiteKey
		}
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.SiteKey) != "" {
			return entry.SiteKey
		}
	}
	return ""
}

func findSiteKeyForHost(host string, entries []captchaSiteKeyEntry) string {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, entry := range entries {
		pattern := strings.ToLower(strings.TrimSpace(entry.HostPattern))
		key := strings.TrimSpace(entry.SiteKey)
		if pattern == "" || key == "" {
			continue
		}
		switch {
		case pattern == "*" || pattern == host:
			return key
		case strings.HasPrefix(pattern, "*."):
			base := strings.TrimPrefix(pattern, "*.")
			if host == base || strings.HasSuffix(host, "."+base) {
				return key
			}
		case strings.HasPrefix(pattern, ".") && strings.HasSuffix(host, strings.TrimPrefix(pattern, ".")):
			return key
		}
	}
	return ""
}

func extractDomain(value string) string {
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	if index := strings.IndexAny(value, "/?#"); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndex(value, ":"); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(value)
}

func liveFormAction(slug, token string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "your-form"
	}
	token = strings.TrimSpace(token)
	if token == "" {
		token = "YOUR_FORM_TOKEN"
	}
	return fmt.Sprintf("/forms/%s/submit?token=%s", slug, token)
}
