package forms

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

var (
	ErrRedirectNotAllowed = errors.New("redirect URL not in allowed origins")
	errInvalidRedirect    = errors.New("invalid redirect URL")
	errOriginsRequired    = errors.New("absolute redirects not allowed without configured origins")
)

func (form *Form) IsOriginAllowed(host string) bool {
	allowedOrigins := strings.TrimSpace(form.AllowedOrigins)
	if allowedOrigins == "" {
		return false
	}
	entries := strings.Split(allowedOrigins, ",")
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "*" {
			return true
		}
	}

	host = normalizedHost(host)
	if host == "" {
		return false
	}
	for _, entry := range entries {
		if originMatches(host, entry) {
			return true
		}
	}
	return false
}

func (form *Form) ValidateRedirectURL(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	if strings.HasPrefix(target, "//") || strings.Contains(target, `\`) {
		return ErrRedirectNotAllowed
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return errInvalidRedirect
	}
	if !parsed.IsAbs() && parsed.Host == "" {
		return nil
	}
	if strings.TrimSpace(form.AllowedOrigins) == "" {
		return errOriginsRequired
	}
	if parsed.Scheme != "" && !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return ErrRedirectNotAllowed
	}
	if parsed.Hostname() == "" {
		return ErrRedirectNotAllowed
	}
	if form.IsOriginAllowed(parsed.Hostname()) {
		return nil
	}
	return ErrRedirectNotAllowed
}

func originMatches(host, entry string) bool {
	entry = strings.TrimSpace(entry)
	if entry == "*" {
		return true
	}
	entry = strings.TrimPrefix(entry, "*.")
	allowedHost := normalizedHost(entry)
	return allowedHost != "" && (host == allowedHost || strings.HasSuffix(host, "."+allowedHost))
}

func normalizedHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if net.ParseIP(value) != nil {
		return strings.ToLower(value)
	}

	parsed, err := url.Parse(value)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	parsed, err = url.Parse("//" + value)
	if err == nil {
		return strings.ToLower(parsed.Hostname())
	}
	return ""
}
