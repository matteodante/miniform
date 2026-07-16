package cli

import (
	"flag"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type mailerView struct {
	ID               uint      `json:"id"`
	Name             string    `json:"name"`
	Provider         string    `json:"provider"`
	APIKey           string    `json:"api_key,omitempty"`
	Domain           string    `json:"domain,omitempty"`
	DefaultFromName  string    `json:"default_from_name,omitempty"`
	DefaultFromEmail string    `json:"default_from_email,omitempty"`
	DefaultsJSON     string    `json:"defaults_json,omitempty"`
	SMTPHost         string    `json:"smtp_host,omitempty"`
	SMTPPort         int       `json:"smtp_port,omitempty"`
	SMTPUsername     string    `json:"smtp_username,omitempty"`
	SMTPPassword     string    `json:"smtp_password,omitempty"`
	SMTPEncryption   string    `json:"smtp_encryption,omitempty"`
	UsageCount       int64     `json:"usage_count"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type mailerFlags struct {
	name             *string
	provider         *string
	apiKeyFile       *string
	domain           *string
	defaultFromName  *string
	defaultFromEmail *string
	defaultsFile     *string
	smtpHost         *string
	smtpPort         *int
	smtpUsername     *string
	smtpPasswordFile *string
	smtpEncryption   *string
}

func (r *Runner) runMailer(args []string) (any, error) {
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	action, actionArgs, err := requireAction("mailer", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.mailerList(actionArgs)
	case "get":
		return r.mailerGet(actionArgs)
	case "create":
		return r.mailerCreate(actionArgs)
	case "update":
		return r.mailerUpdate(actionArgs)
	case "delete":
		return r.mailerDelete(actionArgs)
	default:
		return nil, usageError("unknown mailer action: " + action)
	}
}

func (r *Runner) mailerList(args []string) (any, error) {
	set := newFlagSet("mailer.list")
	if err := r.parseFlags(set, "mailer.list", args); err != nil {
		return nil, err
	}
	profiles, err := integrations.ListMailerProfiles(r.DB)
	if err != nil {
		return nil, err
	}
	views := make([]mailerView, 0, len(profiles))
	for i := range profiles {
		view, err := r.newMailerView(&profiles[i])
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (r *Runner) mailerGet(args []string) (any, error) {
	set := newFlagSet("mailer.get")
	id := set.Uint("id", 0, "mailer profile id")
	if err := r.parseFlags(set, "mailer.get", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	profile, err := integrations.GetMailerProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	return r.newMailerView(profile)
}

func (r *Runner) mailerCreate(args []string) (any, error) {
	set := newFlagSet("mailer.create")
	flags := defineMailerFlags(set)
	if err := r.parseFlags(set, "mailer.create", args); err != nil {
		return nil, err
	}
	if err := requireString(*flags.name, "name"); err != nil {
		return nil, err
	}
	params, err := r.mailerParams(set, flags, integrations.MailerProfileParams{
		Provider:       "smtp",
		SMTPPort:       587,
		SMTPEncryption: "starttls",
	})
	if err != nil {
		return nil, err
	}
	if err := validateMailerParams(params); err != nil {
		return nil, err
	}
	profile, err := integrations.CreateMailerProfile(r.Logger, r.DB, params)
	if err != nil {
		return nil, err
	}
	return r.newMailerView(profile)
}

func (r *Runner) mailerUpdate(args []string) (any, error) {
	set := newFlagSet("mailer.update")
	id := set.Uint("id", 0, "mailer profile id")
	flags := defineMailerFlags(set)
	clearAPIKey := set.Bool("clear-api-key", false, "clear Mailgun API key")
	clearSMTPPassword := set.Bool("clear-smtp-password", false, "clear SMTP password")
	clearDefaults := set.Bool("clear-defaults", false, "clear defaults JSON")
	if err := r.parseFlags(set, "mailer.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if *clearAPIKey && *flags.apiKeyFile != "" {
		return nil, usageError("--clear-api-key conflicts with --api-key-file")
	}
	if *clearSMTPPassword && *flags.smtpPasswordFile != "" {
		return nil, usageError("--clear-smtp-password conflicts with --smtp-password-file")
	}
	if *clearDefaults && *flags.defaultsFile != "" {
		return nil, usageError("--clear-defaults conflicts with --defaults-file")
	}

	current, err := integrations.GetMailerProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params, err := r.mailerParams(set, flags, mailerParamsFromProfile(current))
	if err != nil {
		return nil, err
	}
	if *clearAPIKey {
		params.APIKey = ""
	}
	if *clearSMTPPassword {
		params.SMTPPassword = ""
	}
	if *clearDefaults {
		params.DefaultsJSON = ""
	}
	if err := validateMailerParams(params); err != nil {
		return nil, err
	}
	profile, err := integrations.UpdateMailerProfile(r.Logger, r.DB, *id, params)
	if err != nil {
		return nil, err
	}
	return r.newMailerView(profile)
}

func (r *Runner) mailerDelete(args []string) (any, error) {
	set := newFlagSet("mailer.delete")
	id := set.Uint("id", 0, "mailer profile id")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "mailer.delete", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("mailer delete requires --yes")
	}
	if _, err := integrations.GetMailerProfileByID(r.DB, *id); err != nil {
		return nil, err
	}
	usage, err := forms.MailerProfileUsage(r.DB, *id)
	if err != nil {
		return nil, err
	}
	if usage > 0 {
		return nil, conflictError("mailer profile is referenced by one or more forms")
	}
	if err := integrations.DeleteMailerProfile(r.Logger, r.DB, *id); err != nil {
		return nil, err
	}
	return map[string]any{"id": *id, "deleted": true}, nil
}

func defineMailerFlags(set *flag.FlagSet) mailerFlags {
	return mailerFlags{
		name:             set.String("name", "", "profile name"),
		provider:         set.String("provider", "", "smtp or mailgun"),
		apiKeyFile:       set.String("api-key-file", "", "path containing Mailgun API key, or - for stdin"),
		domain:           set.String("domain", "", "Mailgun sending domain"),
		defaultFromName:  set.String("default-from-name", "", "default sender name"),
		defaultFromEmail: set.String("default-from-email", "", "default sender email"),
		defaultsFile:     set.String("defaults-file", "", "path containing defaults JSON"),
		smtpHost:         set.String("smtp-host", "", "SMTP hostname"),
		smtpPort:         set.Int("smtp-port", 0, "SMTP port"),
		smtpUsername:     set.String("smtp-username", "", "SMTP username"),
		smtpPasswordFile: set.String("smtp-password-file", "", "path containing SMTP password, or - for stdin"),
		smtpEncryption:   set.String("smtp-encryption", "", "starttls, tls, or none"),
	}
}

func (r *Runner) mailerParams(set *flag.FlagSet, flags mailerFlags, params integrations.MailerProfileParams) (integrations.MailerProfileParams, error) {
	if *flags.apiKeyFile == "-" && *flags.smtpPasswordFile == "-" {
		return params, usageError("only one secret file can be stdin")
	}
	applyStringFlag(set, "name", flags.name, &params.Name)
	applyStringFlag(set, "provider", flags.provider, &params.Provider)
	applyStringFlag(set, "domain", flags.domain, &params.Domain)
	applyStringFlag(set, "default-from-name", flags.defaultFromName, &params.DefaultFromName)
	applyStringFlag(set, "default-from-email", flags.defaultFromEmail, &params.DefaultFromEmail)
	applyStringFlag(set, "smtp-host", flags.smtpHost, &params.SMTPHost)
	applyStringFlag(set, "smtp-username", flags.smtpUsername, &params.SMTPUsername)
	applyStringFlag(set, "smtp-encryption", flags.smtpEncryption, &params.SMTPEncryption)
	if flagWasSet(set, "smtp-port") {
		params.SMTPPort = *flags.smtpPort
	}

	var err error
	if *flags.apiKeyFile != "" {
		params.APIKey, err = readFileValue(*flags.apiKeyFile, r.Stdin)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	if *flags.smtpPasswordFile != "" {
		params.SMTPPassword, err = readFileValue(*flags.smtpPasswordFile, r.Stdin)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	if *flags.defaultsFile != "" {
		params.DefaultsJSON, err = readContentFile(*flags.defaultsFile)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	return params, nil
}

func validateMailerParams(params integrations.MailerProfileParams) error {
	switch strings.ToLower(strings.TrimSpace(params.Provider)) {
	case "smtp", "mailgun":
	default:
		return validationError("provider must be smtp or mailgun")
	}
	if params.SMTPPort < 0 || params.SMTPPort > 65535 {
		return validationError("smtp port must be between 0 and 65535")
	}
	switch strings.ToLower(strings.TrimSpace(params.SMTPEncryption)) {
	case "", "starttls", "tls", "none":
	default:
		return validationError("smtp encryption must be starttls, tls, or none")
	}
	return nil
}

func mailerParamsFromProfile(profile *integrations.MailerProfile) integrations.MailerProfileParams {
	return integrations.MailerProfileParams{
		Name:             profile.Name,
		Provider:         profile.Provider,
		APIKey:           profile.APIKey,
		Domain:           profile.Domain,
		DefaultFromName:  profile.DefaultFromName,
		DefaultFromEmail: profile.DefaultFromEmail,
		DefaultsJSON:     profile.DefaultsJSON,
		SMTPHost:         profile.SMTPHost,
		SMTPPort:         profile.SMTPPort,
		SMTPUsername:     profile.SMTPUsername,
		SMTPPassword:     profile.SMTPPassword,
		SMTPEncryption:   profile.SMTPEncryption,
	}
}

func (r *Runner) newMailerView(profile *integrations.MailerProfile) (mailerView, error) {
	usage, err := forms.MailerProfileUsage(r.DB, profile.ID)
	if err != nil {
		return mailerView{}, err
	}
	return mailerView{
		ID:               profile.ID,
		Name:             profile.Name,
		Provider:         profile.Provider,
		APIKey:           redact(profile.APIKey, r.ShowSecrets),
		Domain:           profile.Domain,
		DefaultFromName:  profile.DefaultFromName,
		DefaultFromEmail: profile.DefaultFromEmail,
		DefaultsJSON:     redact(profile.DefaultsJSON, r.ShowSecrets),
		SMTPHost:         profile.SMTPHost,
		SMTPPort:         profile.SMTPPort,
		SMTPUsername:     profile.SMTPUsername,
		SMTPPassword:     redact(profile.SMTPPassword, r.ShowSecrets),
		SMTPEncryption:   profile.SMTPEncryption,
		UsageCount:       usage,
		CreatedAt:        profile.CreatedAt,
		UpdatedAt:        profile.UpdatedAt,
	}, nil
}

func applyStringFlag(set *flag.FlagSet, name string, source *string, target *string) {
	if flagWasSet(set, name) {
		*target = *source
	}
}
