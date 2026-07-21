package forms

import (
	"errors"
	"io"
	"net"
	"net/url"
	"strings"

	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/integrations"
)

func normalizeAllowedOrigins(value string) (string, error) {
	entries := strings.Split(value, ",")
	normalized := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			return "", invalid("allowed_origins", "Allowed origins must be a comma-separated list of hosts or *")
		}
		if entry == "*" {
			return "*", nil
		}

		wildcard := strings.HasPrefix(entry, "*.")
		if wildcard {
			entry = strings.TrimPrefix(entry, "*.")
		}
		if strings.Contains(entry, "*") {
			return "", invalid("allowed_origins", "Wildcards are allowed only as a leading *.")
		}

		host, err := originHost(entry)
		if err != nil || !validOriginHost(host) {
			return "", invalid("allowed_origins", "Each allowed origin must be a valid HTTP host")
		}
		host = strings.ToLower(host)
		if wildcard {
			host = "*." + host
		}
		if _, duplicate := seen[host]; duplicate {
			continue
		}
		seen[host] = struct{}{}
		normalized = append(normalized, host)
	}
	return strings.Join(normalized, ", "), nil
}

func originHost(value string) (string, error) {
	if net.ParseIP(value) != nil {
		return value, nil
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.User != nil || parsed.Hostname() == "" ||
			(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
			return "", errors.New("invalid origin URL")
		}
		return parsed.Hostname(), nil
	}
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid origin host")
	}
	return parsed.Hostname(), nil
}

func validOriginHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func validateGeneratedHTML(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	tokenizer := htmlnode.NewTokenizer(strings.NewReader(value))
	for {
		switch tokenizer.Next() {
		case htmlnode.StartTagToken, htmlnode.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.DataAtom == atom.Form || strings.EqualFold(token.Data, "form") {
				return nil
			}
		case htmlnode.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				return invalid("generated_html", "Generated HTML must contain a form element")
			}
			return invalid("generated_html", "Generated HTML could not be parsed")
		}
	}
}

func validateProfileReferences(tx *gorm.DB, mailerID, captchaID *uint) error {
	if mailerID != nil {
		var count int64
		if err := tx.Model(&integrations.MailerProfile{}).Where("id = ?", *mailerID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return invalid("mailer_profile_id", "Mailer profile not found")
		}
	}
	if captchaID != nil {
		var count int64
		if err := tx.Model(&integrations.CaptchaProfile{}).Where("id = ?", *captchaID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return invalid("captcha_profile_id", "Captcha profile not found")
		}
	}
	return nil
}
