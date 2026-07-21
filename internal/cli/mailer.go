package cli

import (
	"flag"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type mailerView struct {
	ID               uint      `json:"id"`
	Name             string    `json:"name"`
	DefaultFromName  string    `json:"default_from_name,omitempty"`
	DefaultFromEmail string    `json:"default_from_email,omitempty"`
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
	defaultFromName  *string
	defaultFromEmail *string
	smtpHost         *string
	smtpPort         *int
	smtpUsername     *string
	smtpPasswordFile *string
	smtpEncryption   *string
}

func (r *Runner) runMailer(args []string) (any, error) {
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
	if err := r.requireDatabase(); err != nil {
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
	if err := r.requireDatabase(); err != nil {
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
	password, err := readFileValue(*flags.smtpPasswordFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	params := mailerParams(set, flags, integrations.MailerProfileParams{
		SMTPPort:       587,
		SMTPEncryption: "starttls",
	}, password)
	if err := r.requireDatabase(); err != nil {
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
	clearSMTPPassword := set.Bool("clear-smtp-password", false, "clear SMTP password")
	if err := r.parseFlags(set, "mailer.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if *clearSMTPPassword && *flags.smtpPasswordFile != "" {
		return nil, usageError("--clear-smtp-password conflicts with --smtp-password-file")
	}
	password, err := readFileValue(*flags.smtpPasswordFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}

	current, err := integrations.GetMailerProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params := mailerParams(set, flags, mailerParamsFromProfile(current), password)
	if *clearSMTPPassword {
		params.SMTPPassword = ""
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
	if err := r.requireDatabase(); err != nil {
		return nil, err
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
		defaultFromName:  set.String("default-from-name", "", "default sender name"),
		defaultFromEmail: set.String("default-from-email", "", "default sender email"),
		smtpHost:         set.String("smtp-host", "", "SMTP hostname"),
		smtpPort:         set.Int("smtp-port", 0, "SMTP port"),
		smtpUsername:     set.String("smtp-username", "", "SMTP username"),
		smtpPasswordFile: set.String("smtp-password-file", "", "path containing SMTP password, or - for stdin"),
		smtpEncryption:   set.String("smtp-encryption", "", "starttls, tls, or none"),
	}
}

func mailerParams(set *flag.FlagSet, flags mailerFlags, params integrations.MailerProfileParams, password string) integrations.MailerProfileParams {
	applyStringFlag(set, "name", flags.name, &params.Name)
	applyStringFlag(set, "default-from-name", flags.defaultFromName, &params.DefaultFromName)
	applyStringFlag(set, "default-from-email", flags.defaultFromEmail, &params.DefaultFromEmail)
	applyStringFlag(set, "smtp-host", flags.smtpHost, &params.SMTPHost)
	applyStringFlag(set, "smtp-username", flags.smtpUsername, &params.SMTPUsername)
	applyStringFlag(set, "smtp-encryption", flags.smtpEncryption, &params.SMTPEncryption)
	if flagWasSet(set, "smtp-port") {
		params.SMTPPort = *flags.smtpPort
	}

	if *flags.smtpPasswordFile != "" {
		params.SMTPPassword = password
	}
	return params
}

func mailerParamsFromProfile(profile *integrations.MailerProfile) integrations.MailerProfileParams {
	return integrations.MailerProfileParams{
		Name:             profile.Name,
		DefaultFromName:  profile.DefaultFromName,
		DefaultFromEmail: profile.DefaultFromEmail,
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
		DefaultFromName:  profile.DefaultFromName,
		DefaultFromEmail: profile.DefaultFromEmail,
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
