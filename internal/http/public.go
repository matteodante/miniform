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
)

var (
	errEmptySubmission = errors.New("submission payload empty")
	errTooManyFields   = errors.New("too many fields")
	errCaptchaFailed   = errors.New("captcha verification failed")
)

type publicFailure struct {
	status  int
	message string
}

func (failure *publicFailure) Error() string { return failure.message }

func PublicFormSubmission(ctx *cartridge.Context, cfg *config.Config) error {
	form, err := publicForm(ctx)
	if err != nil {
		var failure *publicFailure
		if !errors.As(err, &failure) {
			return fiber.ErrInternalServerError
		}
		return jsonError(ctx, failure.status, failure.message)
	}

	payload, err := extractSubmissionPayload(ctx, cfg)
	if err != nil {
		return submissionFailure(ctx, form, payload, fiber.StatusBadRequest, err.Error())
	}
	successURL := redirectField(payload, "_success_url")
	errorURL := redirectField(payload, "_error_url")
	if !validRedirect(form, successURL) {
		return jsonError(ctx, fiber.StatusBadRequest, "invalid success redirect URL")
	}
	if !validRedirect(form, errorURL) {
		return jsonError(ctx, fiber.StatusBadRequest, "invalid error redirect URL")
	}

	if err := verifyCaptcha(ctx, form, payload); err != nil {
		return redirectOrJSON(ctx, errorURL, fiber.StatusBadRequest, err.Error())
	}
	delete(payload, "_success_url")
	delete(payload, "_error_url")

	files, err := uploadedFiles(ctx)
	if err != nil {
		return redirectOrJSON(ctx, errorURL, fiber.StatusBadRequest, err.Error())
	}
	submission, err := forms.CreateSubmissionWithFiles(
		ctx.Logger, ctx.DB(), form, payload, ctx.Get(fiber.HeaderUserAgent),
		cfg.DataDirectory, files,
	)
	if err != nil {
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

func publicForm(ctx *cartridge.Context) (*forms.Form, error) {
	slug := strings.TrimSpace(ctx.Params("slug"))
	if slug == "" {
		return nil, &publicFailure{fiber.StatusNotFound, "form not found"}
	}
	form, err := forms.GetBySlug(ctx.DB(), slug)
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
	if strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEApplicationJSON) {
		if err := json.Unmarshal(ctx.Body(), &payload); err != nil {
			return payload, err
		}
		if len(payload) > cfg.MaxInputFields {
			return payload, errTooManyFields
		}
		if len(payload) == 0 {
			return payload, errEmptySubmission
		}
		return payload, nil
	}

	fieldCount := 0
	if strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEMultipartForm) {
		multipart, err := ctx.MultipartForm()
		if err != nil {
			return payload, err
		}
		for name, values := range multipart.Value {
			fieldCount += len(values)
			if fieldCount > cfg.MaxInputFields {
				return payload, errTooManyFields
			}
			setFormValues(payload, name, values)
		}
	} else {
		for name, value := range ctx.Request().PostArgs().All() {
			fieldCount++
			if fieldCount <= cfg.MaxInputFields {
				appendFormValue(payload, string(name), string(value))
			}
		}
		if fieldCount > cfg.MaxInputFields {
			return payload, errTooManyFields
		}
	}
	if len(payload) == 0 {
		return payload, errEmptySubmission
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

func uploadedFiles(ctx *cartridge.Context) ([]*forms.UploadedFile, error) {
	if !strings.Contains(ctx.Get(fiber.HeaderContentType), fiber.MIMEMultipartForm) {
		return nil, nil
	}
	multipart, err := ctx.MultipartForm()
	if err != nil {
		return nil, err
	}
	return forms.ExtractFiles(multipart)
}

func submissionFailure(ctx *cartridge.Context, form *forms.Form, payload map[string]any, status int, message string) error {
	redirect := redirectField(payload, "_error_url")
	if validRedirect(form, redirect) && redirect != "" {
		return ctx.Redirect(redirect)
	}
	return jsonError(ctx, status, message)
}

func redirectField(payload map[string]any, name string) string {
	value, _ := payload[name].(string)
	return strings.TrimSpace(value)
}

func validRedirect(form *forms.Form, target string) bool {
	return target == "" || form.ValidateRedirectURL(target) == nil
}

func redirectOrJSON(ctx *cartridge.Context, target string, status int, message string) error {
	if target != "" {
		return ctx.Redirect(target)
	}
	return jsonError(ctx, status, message)
}

func verifyCaptcha(ctx *cartridge.Context, form *forms.Form, payload map[string]any) error {
	if form.CaptchaProfileID == nil {
		return nil
	}
	profile := form.CaptchaProfile
	if profile == nil || !strings.EqualFold(strings.TrimSpace(profile.Provider), "turnstile") {
		ctx.Logger.Warn("captcha profile unavailable", slog.Uint64("form_id", uint64(form.ID)))
		return errCaptchaFailed
	}
	settings := integrations.ResolveCaptchaSettings(profile.PolicyJSON, form.CaptchaOverridesJSON)
	token, ok := extractCaptchaToken(payload)
	if !ok {
		if settings.Required {
			return errCaptchaFailed
		}
		return nil
	}
	if strings.TrimSpace(profile.SecretKey) == "" {
		ctx.Logger.Warn("captcha secret unavailable", slog.Uint64("form_id", uint64(form.ID)))
		return errCaptchaFailed
	}
	result, err := integrations.VerifyTurnstileToken(ctx.UserContext(), profile.SecretKey, token, ctx.IP())
	if err != nil {
		ctx.Logger.Warn("turnstile rejected submission", slog.Uint64("form_id", uint64(form.ID)), slog.Any("error", err))
		return errCaptchaFailed
	}
	if reason := turnstileResultFailure(form, settings, result); reason != "" {
		ctx.Logger.Warn("turnstile result rejected", slog.Uint64("form_id", uint64(form.ID)), slog.String("reason", reason))
		return errCaptchaFailed
	}
	return nil
}

func turnstileResultFailure(form *forms.Form, settings integrations.CaptchaSettings, result *integrations.TurnstileResult) string {
	if result == nil || !result.Success {
		return "unsuccessful verification"
	}
	if strings.TrimSpace(result.Action) != settings.Action {
		return "action mismatch"
	}
	if strings.TrimSpace(form.AllowedOrigins) == "*" {
		return ""
	}
	if hostname := strings.TrimSpace(result.Hostname); hostname == "" || !form.IsOriginAllowed(hostname) {
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
	origin := ctx.Get(fiber.HeaderOrigin)
	if origin == "" {
		origin = ctx.Get(fiber.HeaderReferer)
	}
	return extractDomain(origin)
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
