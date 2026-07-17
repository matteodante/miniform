package cli

import (
	"net/url"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	formembed "github.com/matteodante/miniform/internal/forms/embed"
)

type formView struct {
	ID               uint                 `json:"id"`
	PublicID         string               `json:"public_id"`
	Name             string               `json:"name"`
	Slug             string               `json:"slug"`
	Token            string               `json:"token"`
	Endpoint         string               `json:"endpoint"`
	AllowedOrigins   string               `json:"allowed_origins"`
	UseSDK           bool                 `json:"use_sdk"`
	HasGeneratedHTML bool                 `json:"has_generated_html"`
	GeneratedHTML    string               `json:"generated_html,omitempty"`
	CaptchaProfileID *uint                `json:"captcha_profile_id,omitempty"`
	EmailDelivery    *emailDeliveryView   `json:"email_delivery,omitempty"`
	WebhookDelivery  *webhookDeliveryView `json:"webhook_delivery,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type emailDeliveryView struct {
	Enabled         bool   `json:"enabled"`
	MailerProfileID *uint  `json:"mailer_profile_id,omitempty"`
	Recipient       string `json:"recipient,omitempty"`
}

type webhookDeliveryView struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url,omitempty"`
	Secret      string `json:"secret,omitempty"`
	HeadersJSON string `json:"headers_json,omitempty"`
}

type formTemplateView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Slug        string `json:"slug"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	HTML        string `json:"html,omitempty"`
}

type formCodeView struct {
	FormID      uint   `json:"form_id"`
	Action      string `json:"action"`
	HTML        string `json:"html"`
	IncludesSDK bool   `json:"includes_sdk"`
	Redacted    bool   `json:"redacted"`
	Warning     string `json:"warning,omitempty"`
}

func (r *Runner) runForm(args []string) (any, error) {
	action, actionArgs, err := requireAction("form", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.formList(actionArgs)
	case "get":
		return r.formGet(actionArgs)
	case "create":
		return r.formCreate(actionArgs)
	case "update":
		return r.formUpdate(actionArgs)
	case "delete":
		return r.formDelete(actionArgs)
	case "rotate-token":
		return r.formRotateToken(actionArgs)
	case "code":
		return r.formCode(actionArgs)
	case "template-list":
		return r.formTemplateList(actionArgs)
	case "template-get":
		return r.formTemplateGet(actionArgs)
	default:
		return nil, usageError("unknown form action: " + action)
	}
}

func (r *Runner) formCode(args []string) (any, error) {
	set := newFlagSet("form.code")
	id := set.Uint("id", 0, "form id")
	slug := set.String("slug", "", "form slug")
	includeSDK := set.Bool("include-sdk", false, "include the optional JavaScript SDK")
	baseURL := set.String("base-url", "", "public Miniform base URL")
	if err := r.parseFlags(set, "form.code", args); err != nil {
		return nil, err
	}
	if (*id == 0) == (strings.TrimSpace(*slug) == "") {
		return nil, usageError("set exactly one of --id or --slug")
	}
	publicBaseURL, err := validatedBaseURL(*baseURL)
	if err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}

	var form *forms.Form
	if *id > 0 {
		form, err = forms.GetByID(r.DB, *id)
	} else {
		form, err = forms.GetBySlug(r.DB, strings.TrimSpace(*slug))
	}
	if err != nil {
		return nil, err
	}
	useSDK := form.UseSDK
	if flagWasSet(set, "include-sdk") {
		useSDK = *includeSDK
	}
	result := formembed.Build(form, formembed.Options{
		ShowToken:  r.ShowSecrets,
		IncludeSDK: useSDK,
		BaseURL:    publicBaseURL,
	})
	return formCodeView{
		FormID:      form.ID,
		Action:      result.Action,
		HTML:        result.HTML,
		IncludesSDK: result.IncludesSDK,
		Redacted:    result.Redacted,
		Warning:     result.Warning,
	}, nil
}

func validatedBaseURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", validationError("base-url must be an absolute HTTP(S) URL")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (r *Runner) formList(args []string) (any, error) {
	set := newFlagSet("form.list")
	if err := r.parseFlags(set, "form.list", args); err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	items, err := forms.List(r.DB)
	if err != nil {
		return nil, err
	}
	views := make([]formView, 0, len(items))
	for i := range items {
		form, err := forms.GetByID(r.DB, items[i].ID)
		if err != nil {
			return nil, err
		}
		views = append(views, newFormView(form, r.ShowSecrets))
	}
	return views, nil
}

func (r *Runner) formGet(args []string) (any, error) {
	set := newFlagSet("form.get")
	id := set.Uint("id", 0, "form id")
	slug := set.String("slug", "", "form slug")
	if err := r.parseFlags(set, "form.get", args); err != nil {
		return nil, err
	}
	if (*id == 0) == (strings.TrimSpace(*slug) == "") {
		return nil, usageError("set exactly one of --id or --slug")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}

	var form *forms.Form
	var err error
	if *id > 0 {
		form, err = forms.GetByID(r.DB, *id)
	} else {
		form, err = forms.GetBySlug(r.DB, strings.TrimSpace(*slug))
	}
	if err != nil {
		return nil, err
	}
	return newFormView(form, r.ShowSecrets), nil
}

func (r *Runner) formCreate(args []string) (any, error) {
	set := newFlagSet("form.create")
	templateID := set.String("template", "", "built-in template id")
	name := set.String("name", "", "form name")
	slug := set.String("slug", "", "unique form slug")
	origins := set.String("allowed-origins", "", "comma-separated origins or *")
	useSDK := set.Bool("use-sdk", false, "include the JavaScript SDK")
	generatedHTMLFile := set.String("generated-html-file", "", "path containing generated form HTML")
	mailerID := set.Uint("mailer-profile-id", 0, "mailer profile id")
	captchaID := set.Uint("captcha-profile-id", 0, "captcha profile id")
	emailEnabled := set.Bool("email-enabled", false, "enable email forwarding")
	emailRecipient := set.String("email-recipient", "", "email forwarding recipient")
	webhookEnabled := set.Bool("webhook-enabled", false, "enable webhook forwarding")
	webhookURL := set.String("webhook-url", "", "webhook URL")
	webhookSecretFile := set.String("webhook-secret-file", "", "path containing webhook signing secret, or - for stdin")
	webhookHeadersFile := set.String("webhook-headers-file", "", "path containing JSON webhook headers")
	if err := r.parseFlags(set, "form.create", args); err != nil {
		return nil, err
	}
	var template *forms.FormTemplate
	if strings.TrimSpace(*templateID) != "" {
		template = forms.GetTemplateByID(strings.TrimSpace(*templateID))
		if template == nil {
			return nil, validationError("unknown form template: " + strings.TrimSpace(*templateID))
		}
		if !flagWasSet(set, "name") {
			*name = template.Name
		}
		if !flagWasSet(set, "slug") {
			*slug = template.Slug
		}
	}
	if err := requireString(*name, "name"); err != nil {
		return nil, err
	}
	if err := requireString(*slug, "slug"); err != nil {
		return nil, err
	}
	if err := requireString(*origins, "allowed-origins"); err != nil {
		return nil, err
	}

	generatedHTML, err := readContentFile(*generatedHTMLFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	webhookSecret, err := readFileValue(*webhookSecretFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	webhookHeaders, err := readContentFile(*webhookHeadersFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	form, err := forms.Create(r.Logger, r.DB, forms.CreateParams{
		Name:               *name,
		Slug:               *slug,
		AllowedOrigins:     *origins,
		UseSDK:             *useSDK,
		GeneratedHTML:      generatedHTML,
		TemplateID:         strings.TrimSpace(*templateID),
		MailerProfileID:    optionalUint(*mailerID),
		CaptchaProfileID:   optionalUint(*captchaID),
		EmailRecipient:     *emailRecipient,
		EmailEnabled:       *emailEnabled,
		WebhookEnabled:     *webhookEnabled,
		WebhookURL:         *webhookURL,
		WebhookSecret:      webhookSecret,
		WebhookHeadersJSON: webhookHeaders,
	})
	if err != nil {
		return nil, err
	}
	return newFormView(form, r.ShowSecrets), nil
}

//nolint:gocyclo // Each branch maps one independent CLI flag to the domain update parameters.
func (r *Runner) formUpdate(args []string) (any, error) {
	set := newFlagSet("form.update")
	id := set.Uint("id", 0, "form id")
	name := set.String("name", "", "form name")
	slug := set.String("slug", "", "unique form slug")
	origins := set.String("allowed-origins", "", "comma-separated origins or *")
	useSDK := set.Bool("use-sdk", false, "include the JavaScript SDK")
	generatedHTMLFile := set.String("generated-html-file", "", "path containing generated form HTML")
	clearGeneratedHTML := set.Bool("clear-generated-html", false, "clear generated form HTML")
	mailerID := set.Uint("mailer-profile-id", 0, "mailer profile id")
	clearMailer := set.Bool("clear-mailer-profile", false, "remove mailer profile assignment")
	captchaID := set.Uint("captcha-profile-id", 0, "captcha profile id")
	clearCaptcha := set.Bool("clear-captcha-profile", false, "remove captcha profile assignment")
	emailEnabled := set.Bool("email-enabled", false, "enable email forwarding")
	emailRecipient := set.String("email-recipient", "", "email forwarding recipient")
	webhookEnabled := set.Bool("webhook-enabled", false, "enable webhook forwarding")
	webhookURL := set.String("webhook-url", "", "webhook URL")
	webhookSecretFile := set.String("webhook-secret-file", "", "path containing webhook signing secret, or - for stdin")
	clearWebhookSecret := set.Bool("clear-webhook-secret", false, "clear webhook signing secret")
	webhookHeadersFile := set.String("webhook-headers-file", "", "path containing JSON webhook headers")
	clearWebhookHeaders := set.Bool("clear-webhook-headers", false, "clear webhook headers")
	if err := r.parseFlags(set, "form.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if *clearMailer && flagWasSet(set, "mailer-profile-id") {
		return nil, usageError("--clear-mailer-profile conflicts with --mailer-profile-id")
	}
	if *clearCaptcha && flagWasSet(set, "captcha-profile-id") {
		return nil, usageError("--clear-captcha-profile conflicts with --captcha-profile-id")
	}
	if *clearGeneratedHTML && *generatedHTMLFile != "" {
		return nil, usageError("--clear-generated-html conflicts with --generated-html-file")
	}
	if *clearWebhookSecret && *webhookSecretFile != "" {
		return nil, usageError("--clear-webhook-secret conflicts with --webhook-secret-file")
	}
	if *clearWebhookHeaders && *webhookHeadersFile != "" {
		return nil, usageError("--clear-webhook-headers conflicts with --webhook-headers-file")
	}
	generatedHTML, err := readContentFile(*generatedHTMLFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	webhookSecret, err := readFileValue(*webhookSecretFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	webhookHeaders, err := readContentFile(*webhookHeadersFile)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}

	current, err := forms.GetByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params := updateParamsFromForm(current)
	if flagWasSet(set, "name") {
		params.Name = *name
	}
	if flagWasSet(set, "slug") {
		params.Slug = *slug
	}
	if flagWasSet(set, "allowed-origins") {
		params.AllowedOrigins = *origins
	}
	if flagWasSet(set, "use-sdk") {
		params.UseSDK = *useSDK
	}
	if *clearGeneratedHTML {
		params.GeneratedHTML = ""
		params.UpdateGeneratedHTML = true
	}
	if *generatedHTMLFile != "" {
		params.GeneratedHTML = generatedHTML
		params.UpdateGeneratedHTML = true
	}
	if flagWasSet(set, "mailer-profile-id") {
		params.MailerProfileID = optionalUint(*mailerID)
	}
	if *clearMailer {
		params.MailerProfileID = nil
	}
	if flagWasSet(set, "captcha-profile-id") {
		params.CaptchaProfileID = optionalUint(*captchaID)
	}
	if *clearCaptcha {
		params.CaptchaProfileID = nil
	}
	if flagWasSet(set, "email-enabled") {
		params.EmailEnabled = *emailEnabled
	}
	if flagWasSet(set, "email-recipient") {
		params.EmailRecipient = *emailRecipient
	}
	if flagWasSet(set, "webhook-enabled") {
		params.WebhookEnabled = *webhookEnabled
	}
	if flagWasSet(set, "webhook-url") {
		params.WebhookURL = *webhookURL
	}
	if *clearWebhookSecret {
		params.WebhookSecret = ""
	}
	if *webhookSecretFile != "" {
		params.WebhookSecret = webhookSecret
	}
	if *clearWebhookHeaders {
		params.WebhookHeadersJSON = ""
	}
	if *webhookHeadersFile != "" {
		params.WebhookHeadersJSON = webhookHeaders
	}

	updated, err := forms.Update(r.Logger, r.DB, params)
	if err != nil {
		return nil, err
	}
	return newFormView(updated, r.ShowSecrets), nil
}

func (r *Runner) formDelete(args []string) (any, error) {
	set := newFlagSet("form.delete")
	id := set.Uint("id", 0, "form id")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "form.delete", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("form delete requires --yes")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	dataDir := ""
	if r.Config != nil {
		dataDir = r.Config.DataDirectory
	}
	if err := forms.DeleteForm(r.Logger, r.DB, dataDir, *id); err != nil {
		return nil, err
	}
	return map[string]any{"id": *id, "deleted": true}, nil
}

func (r *Runner) formRotateToken(args []string) (any, error) {
	set := newFlagSet("form.rotate-token")
	id := set.Uint("id", 0, "form id")
	yes := set.Bool("yes", false, "confirm token invalidation")
	if err := r.parseFlags(set, "form.rotate-token", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("form rotate-token requires --yes")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	form, err := forms.RotateToken(r.Logger, r.DB, *id)
	if err != nil {
		return nil, err
	}
	return newFormView(form, r.ShowSecrets), nil
}

func (r *Runner) formTemplateList(args []string) (any, error) {
	set := newFlagSet("form.template-list")
	if err := r.parseFlags(set, "form.template-list", args); err != nil {
		return nil, err
	}
	templates := forms.GetFormTemplates()
	views := make([]formTemplateView, 0, len(templates))
	for i := range templates {
		views = append(views, newFormTemplateView(&templates[i], "", false))
	}
	return views, nil
}

func (r *Runner) formTemplateGet(args []string) (any, error) {
	set := newFlagSet("form.template-get")
	templateID := set.String("template", "", "built-in template id")
	action := set.String("action", "", "form action URL used when rendering HTML")
	if err := r.parseFlags(set, "form.template-get", args); err != nil {
		return nil, err
	}
	if err := requireString(*templateID, "template"); err != nil {
		return nil, err
	}
	template := forms.GetTemplateByID(strings.TrimSpace(*templateID))
	if template == nil {
		return nil, validationError("unknown form template: " + strings.TrimSpace(*templateID))
	}
	return newFormTemplateView(template, *action, true), nil
}

func updateParamsFromForm(form *forms.Form) forms.UpdateParams {
	params := forms.UpdateParams{
		ID:               form.ID,
		Name:             form.Name,
		Slug:             form.Slug,
		AllowedOrigins:   form.AllowedOrigins,
		UseSDK:           form.UseSDK,
		GeneratedHTML:    form.GeneratedHTML,
		CaptchaProfileID: form.CaptchaProfileID,
	}
	if form.EmailDelivery != nil {
		params.EmailEnabled = form.EmailDelivery.Enabled
		params.MailerProfileID = form.EmailDelivery.MailerProfileID
		params.EmailRecipient = form.EmailDelivery.Recipient
	}
	if form.WebhookDelivery != nil {
		params.WebhookEnabled = form.WebhookDelivery.Enabled
		params.WebhookURL = form.WebhookDelivery.URL
		params.WebhookSecret = form.WebhookDelivery.Secret
		params.WebhookHeadersJSON = form.WebhookDelivery.HeadersJSON
	}
	return params
}

func newFormView(form *forms.Form, showSecrets bool) formView {
	view := formView{
		ID:               form.ID,
		PublicID:         form.PublicID,
		Name:             form.Name,
		Slug:             form.Slug,
		Token:            redact(form.Token, showSecrets),
		Endpoint:         "/forms/" + form.Slug + "/submit",
		AllowedOrigins:   form.AllowedOrigins,
		UseSDK:           form.UseSDK,
		HasGeneratedHTML: strings.TrimSpace(form.GeneratedHTML) != "",
		CaptchaProfileID: form.CaptchaProfileID,
		CreatedAt:        form.CreatedAt,
		UpdatedAt:        form.UpdatedAt,
	}
	if showSecrets {
		view.GeneratedHTML = form.GeneratedHTML
	}
	if form.EmailDelivery != nil {
		view.EmailDelivery = &emailDeliveryView{
			Enabled:         form.EmailDelivery.Enabled,
			MailerProfileID: form.EmailDelivery.MailerProfileID,
			Recipient:       form.EmailDelivery.Recipient,
		}
	}
	if form.WebhookDelivery != nil {
		view.WebhookDelivery = &webhookDeliveryView{
			Enabled:     form.WebhookDelivery.Enabled,
			URL:         form.WebhookDelivery.URL,
			Secret:      redact(form.WebhookDelivery.Secret, showSecrets),
			HeadersJSON: redact(form.WebhookDelivery.HeadersJSON, showSecrets),
		}
	}
	return view
}

func newFormTemplateView(template *forms.FormTemplate, action string, includeHTML bool) formTemplateView {
	view := formTemplateView{
		ID:          template.ID,
		Name:        template.Name,
		Description: template.Description,
		Slug:        template.Slug,
		Icon:        template.Icon,
		Color:       template.Color,
	}
	if includeHTML {
		view.HTML = template.RenderHTML(action)
	}
	return view
}
