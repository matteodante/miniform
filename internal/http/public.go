package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	miniformserver "github.com/matteodante/miniform/internal/server"
)

var (
	errTooManyFields   = errors.New("too many fields")
	errPayloadTooLarge = errors.New("submission fields exceed maximum size")
	errCaptchaFailed   = errors.New("captcha verification failed")
)

type publicFailure struct {
	status  int
	message string
}

func (failure *publicFailure) Error() string { return failure.message }

func PublicFormSubmission(ctx *cartridge.Context, cfg *config.Config) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := publicForm(ctx, db)
	if err != nil {
		var failure *publicFailure
		if !errors.As(err, &failure) {
			return fiber.ErrInternalServerError
		}
		return jsonError(ctx, failure.status, failure.message)
	}

	payload, err := extractSubmissionPayload(ctx, cfg)
	if err != nil {
		status := fiber.StatusBadRequest
		if errors.Is(err, errPayloadTooLarge) {
			status = fiber.StatusRequestEntityTooLarge
		}
		return submissionFailure(ctx, form, payload, status, err.Error())
	}
	origin := requestOrigin(ctx)
	successURL, err := resolvedRedirect(form, redirectField(payload, "_success_url"), origin)
	if err != nil {
		return jsonError(ctx, fiber.StatusBadRequest, "invalid success redirect URL")
	}
	errorURL, err := resolvedRedirect(form, redirectField(payload, "_error_url"), origin)
	if err != nil {
		return jsonError(ctx, fiber.StatusBadRequest, "invalid error redirect URL")
	}

	if err := verifyCaptcha(ctx, cfg, form, payload); err != nil {
		status, message := captchaFailureResponse(err)
		return redirectOrJSON(ctx, errorURL, status, message)
	}
	delete(payload, "_success_url")
	delete(payload, "_error_url")

	files, err := uploadedFiles(ctx, form)
	if err != nil {
		return redirectOrJSON(ctx, errorURL, fiber.StatusBadRequest, err.Error())
	}
	submission, err := forms.CreateSubmissionWithLimits(
		ctx.Logger, db, form, payload, ctx.Get(fiber.HeaderUserAgent),
		cfg.DataDirectory, files, forms.SubmissionLimits{MaxUploadStorageBytes: cfg.MaxUploadStorageBytes},
	)
	if err != nil {
		if errors.Is(err, forms.ErrEmptySubmission) {
			return redirectOrJSON(ctx, errorURL, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, forms.ErrUploadStorageQuotaExceeded) {
			return redirectOrJSON(ctx, errorURL, fiber.StatusInsufficientStorage, err.Error())
		}
		return redirectOrJSON(ctx, errorURL, fiber.StatusInternalServerError, "submission failed")
	}
	if successURL != "" {
		return ctx.Redirect(successURL)
	}
	return ctx.JSON(fiber.Map{
		"ok": true, "submission_id": submission.ID,
		"received_at": submission.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func publicForm(ctx *cartridge.Context, db *gorm.DB) (*forms.Form, error) {
	slug := strings.TrimSpace(ctx.Params("slug"))
	if slug == "" {
		return nil, &publicFailure{fiber.StatusNotFound, "form not found"}
	}
	form, err := forms.GetBySlug(db, slug)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &publicFailure{fiber.StatusNotFound, "form not found"}
		}
		return nil, &publicFailure{fiber.StatusInternalServerError, "form lookup failed"}
	}
	if token := ctx.Query("token"); token == "" || token != form.Token {
		return nil, &publicFailure{fiber.StatusUnauthorized, "invalid token"}
	}
	if !form.IsOriginAllowed(getRequestOrigin(ctx)) {
		return nil, &publicFailure{fiber.StatusForbidden, "origin not allowed"}
	}
	return form, nil
}

func extractSubmissionPayload(ctx *cartridge.Context, cfg *config.Config) (map[string]any, error) {
	payload := make(map[string]any)
	maxPayloadBytes := cfg.MaxPayloadBytes
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = 64 * 1024
	}
	if strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEApplicationJSON) {
		if len(ctx.Body()) > maxPayloadBytes {
			return payload, errPayloadTooLarge
		}
		if err := json.Unmarshal(ctx.Body(), &payload); err != nil {
			return payload, err
		}
		if len(payload) > cfg.MaxInputFields {
			return payload, errTooManyFields
		}
		if len(payload) == 0 {
			return payload, forms.ErrEmptySubmission
		}
		return payload, nil
	}

	fieldCount := 0
	payloadBytes := 0
	hasFiles := false
	if strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEMultipartForm) {
		multipart, err := ctx.MultipartForm()
		if err != nil {
			return payload, err
		}
		for name, values := range multipart.Value {
			fieldCount += len(values)
			payloadBytes += len(name)
			for _, value := range values {
				payloadBytes += len(value)
			}
			if payloadBytes > maxPayloadBytes {
				return payload, errPayloadTooLarge
			}
			if fieldCount > cfg.MaxInputFields {
				return payload, errTooManyFields
			}
			setFormValues(payload, name, values)
		}
		hasFiles = len(multipart.File) > 0
	} else {
		for name, value := range ctx.Request().PostArgs().All() {
			fieldCount++
			payloadBytes += len(name) + len(value)
			if payloadBytes > maxPayloadBytes {
				return payload, errPayloadTooLarge
			}
			if fieldCount <= cfg.MaxInputFields {
				appendFormValue(payload, string(name), string(value))
			}
		}
		if fieldCount > cfg.MaxInputFields {
			return payload, errTooManyFields
		}
	}
	if len(payload) == 0 && !hasFiles {
		return payload, forms.ErrEmptySubmission
	}
	return payload, nil
}

func setFormValues(payload map[string]any, name string, values []string) {
	if len(values) == 1 {
		payload[name] = values[0]
		return
	}
	payload[name] = append([]string(nil), values...)
}

func appendFormValue(payload map[string]any, name, value string) {
	switch current := payload[name].(type) {
	case nil:
		payload[name] = value
	case string:
		payload[name] = []string{current, value}
	case []string:
		payload[name] = append(current, value)
	}
}

func uploadedFiles(ctx *cartridge.Context, form *forms.Form) ([]*forms.UploadedFile, error) {
	if !strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEMultipartForm) {
		return nil, nil
	}
	multipart, err := ctx.MultipartForm()
	if err != nil {
		return nil, err
	}
	if !form.UploadsEnabled && len(multipart.File) > 0 {
		return nil, errors.New("file uploads are disabled for this form")
	}
	return forms.ExtractFiles(multipart)
}

func submissionFailure(ctx *cartridge.Context, form *forms.Form, payload map[string]any, status int, message string) error {
	redirect, err := resolvedRedirect(form, redirectField(payload, "_error_url"), requestOrigin(ctx))
	if err != nil {
		return jsonError(ctx, fiber.StatusBadRequest, "invalid error redirect URL")
	}
	if redirect != "" {
		return ctx.Redirect(redirect)
	}
	return jsonError(ctx, status, message)
}

func redirectField(payload map[string]any, name string) string {
	value, _ := payload[name].(string)
	return strings.TrimSpace(value)
}

func resolvedRedirect(form *forms.Form, target, origin string) (string, error) {
	target = strings.TrimSpace(target)
	if err := form.ValidateRedirectURL(target); err != nil || target == "" {
		return target, err
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.IsAbs() {
		return target, err
	}
	base, err := url.Parse(origin)
	if err != nil || base.Hostname() == "" || !form.IsOriginAllowed(base.Hostname()) {
		return "", forms.ErrRedirectNotAllowed
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", forms.ErrRedirectNotAllowed
	}
	base.Path, base.RawQuery, base.Fragment = "/", "", ""
	return base.ResolveReference(parsed).String(), nil
}

func redirectOrJSON(ctx *cartridge.Context, target string, status int, message string) error {
	if target != "" {
		return ctx.Redirect(target)
	}
	return jsonError(ctx, status, message)
}

func verifyCaptcha(ctx *cartridge.Context, cfg *config.Config, form *forms.Form, payload map[string]any) error {
	if form.CaptchaProfileID == nil {
		_, _ = extractCaptchaToken(payload)
		return nil
	}
	profile := form.CaptchaProfile
	if profile == nil {
		ctx.Logger.Warn("captcha profile unavailable", slog.Uint64("form_id", uint64(form.ID)))
		return errCaptchaFailed
	}
	token, ok := extractCaptchaToken(payload)
	if !ok {
		return errCaptchaFailed
	}
	if strings.TrimSpace(profile.SecretKey) == "" {
		ctx.Logger.Warn("captcha secret unavailable", slog.Uint64("form_id", uint64(form.ID)))
		return errCaptchaFailed
	}
	result, err := integrations.VerifyTurnstileToken(
		ctx.UserContext(), profile.SecretKey, token,
		miniformserver.ClientIP(ctx.Ctx, cfg.ProxyMode()),
	)
	if err != nil {
		ctx.Logger.Warn("turnstile rejected submission", slog.Uint64("form_id", uint64(form.ID)), slog.Any("error", err))
		if errors.Is(err, integrations.ErrTurnstileUnavailable) {
			return integrations.ErrTurnstileUnavailable
		}
		return errCaptchaFailed
	}
	if reason := turnstileResultFailure(form, result); reason != "" {
		ctx.Logger.Warn("turnstile result rejected", slog.Uint64("form_id", uint64(form.ID)), slog.String("reason", reason))
		return errCaptchaFailed
	}
	return nil
}

func captchaFailureResponse(err error) (int, string) {
	if errors.Is(err, integrations.ErrTurnstileUnavailable) {
		return fiber.StatusServiceUnavailable, "captcha service temporarily unavailable"
	}
	return fiber.StatusBadRequest, errCaptchaFailed.Error()
}

func turnstileResultFailure(form *forms.Form, result *integrations.TurnstileResult) string {
	if result == nil || !result.Success {
		return "unsuccessful verification"
	}
	if strings.TrimSpace(result.Action) != integrations.TurnstileAction {
		return "action mismatch"
	}
	hostname := strings.TrimSpace(result.Hostname)
	if hostname == "" {
		return "hostname mismatch"
	}
	if strings.TrimSpace(form.AllowedOrigins) != "*" && !form.IsOriginAllowed(hostname) {
		return "hostname mismatch"
	}
	return ""
}

func extractCaptchaToken(payload map[string]any) (string, bool) {
	var token string
	for _, name := range []string{"cf-turnstile-response", "cf_turnstile_response"} {
		value, found := payload[name]
		delete(payload, name)
		if found && token == "" {
			token = firstString(value)
		}
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func firstString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []string:
		if len(typed) > 0 {
			return typed[0]
		}
	case []any:
		if len(typed) > 0 {
			text, _ := typed[0].(string)
			return text
		}
	case []byte:
		return string(typed)
	}
	return ""
}

func jsonError(ctx *cartridge.Context, status int, message string) error {
	return ctx.Status(status).JSON(fiber.Map{"ok": false, "error": message})
}

func getRequestOrigin(ctx *cartridge.Context) string {
	return extractDomain(requestOrigin(ctx))
}

func requestOrigin(ctx *cartridge.Context) string {
	origin := strings.TrimSpace(ctx.Get(fiber.HeaderOrigin))
	if origin == "" {
		origin = strings.TrimSpace(ctx.Get(fiber.HeaderReferer))
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func extractDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
