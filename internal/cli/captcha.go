package cli

import (
	"flag"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type captchaView struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	SiteKey    string    `json:"site_key"`
	SecretKey  string    `json:"secret_key,omitempty"`
	UsageCount int64     `json:"usage_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type captchaFlags struct {
	name          *string
	siteKey       *string
	secretKeyFile *string
}

func (r *Runner) runCaptcha(args []string) (any, error) {
	action, actionArgs, err := requireAction("captcha", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.captchaList(actionArgs)
	case "get":
		return r.captchaGet(actionArgs)
	case "create":
		return r.captchaCreate(actionArgs)
	case "update":
		return r.captchaUpdate(actionArgs)
	case "delete":
		return r.captchaDelete(actionArgs)
	default:
		return nil, usageError("unknown captcha action: " + action)
	}
}

func (r *Runner) captchaList(args []string) (any, error) {
	set := newFlagSet("captcha.list")
	if err := r.parseFlags(set, "captcha.list", args); err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	profiles, err := integrations.ListCaptchaProfiles(r.DB)
	if err != nil {
		return nil, err
	}
	views := make([]captchaView, 0, len(profiles))
	for i := range profiles {
		view, err := r.newCaptchaView(&profiles[i])
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (r *Runner) captchaGet(args []string) (any, error) {
	set := newFlagSet("captcha.get")
	id := set.Uint("id", 0, "captcha profile id")
	if err := r.parseFlags(set, "captcha.get", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	profile, err := integrations.GetCaptchaProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	return r.newCaptchaView(profile)
}

func (r *Runner) captchaCreate(args []string) (any, error) {
	set := newFlagSet("captcha.create")
	flags := defineCaptchaFlags(set)
	if err := r.parseFlags(set, "captcha.create", args); err != nil {
		return nil, err
	}
	if err := requireString(*flags.name, "name"); err != nil {
		return nil, err
	}
	secretKey, err := readFileValue(*flags.secretKeyFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	params := captchaParams(set, flags, integrations.CaptchaProfileParams{}, secretKey)
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	profile, err := integrations.CreateCaptchaProfile(r.Logger, r.DB, params)
	if err != nil {
		return nil, err
	}
	return r.newCaptchaView(profile)
}

func (r *Runner) captchaUpdate(args []string) (any, error) {
	set := newFlagSet("captcha.update")
	id := set.Uint("id", 0, "captcha profile id")
	flags := defineCaptchaFlags(set)
	if err := r.parseFlags(set, "captcha.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	secretKey, err := readFileValue(*flags.secretKeyFile, r.Stdin)
	if err != nil {
		return nil, validationError(err.Error())
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	current, err := integrations.GetCaptchaProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params := captchaParams(set, flags, captchaParamsFromProfile(current), secretKey)
	profile, err := integrations.UpdateCaptchaProfile(r.Logger, r.DB, *id, params)
	if err != nil {
		return nil, err
	}
	return r.newCaptchaView(profile)
}

func (r *Runner) captchaDelete(args []string) (any, error) {
	set := newFlagSet("captcha.delete")
	id := set.Uint("id", 0, "captcha profile id")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "captcha.delete", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("captcha delete requires --yes")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	if _, err := integrations.GetCaptchaProfileByID(r.DB, *id); err != nil {
		return nil, err
	}
	usage, err := forms.CaptchaProfileUsage(r.DB, *id)
	if err != nil {
		return nil, err
	}
	if usage > 0 {
		return nil, conflictError("captcha profile is referenced by one or more forms")
	}
	if err := integrations.DeleteCaptchaProfile(r.Logger, r.DB, *id); err != nil {
		return nil, err
	}
	return map[string]any{"id": *id, "deleted": true}, nil
}

func defineCaptchaFlags(set *flag.FlagSet) captchaFlags {
	return captchaFlags{
		name:          set.String("name", "", "profile name"),
		siteKey:       set.String("site-key", "", "Turnstile site key"),
		secretKeyFile: set.String("secret-key-file", "", "path containing the Turnstile secret key, or - for stdin"),
	}
}

func captchaParams(set *flag.FlagSet, flags captchaFlags, params integrations.CaptchaProfileParams, secretKey string) integrations.CaptchaProfileParams {
	applyStringFlag(set, "name", flags.name, &params.Name)
	applyStringFlag(set, "site-key", flags.siteKey, &params.SiteKey)

	if *flags.secretKeyFile != "" {
		params.SecretKey = secretKey
	}
	return params
}

func captchaParamsFromProfile(profile *integrations.CaptchaProfile) integrations.CaptchaProfileParams {
	return integrations.CaptchaProfileParams{
		Name:      profile.Name,
		SiteKey:   profile.SiteKey,
		SecretKey: profile.SecretKey,
	}
}

func (r *Runner) newCaptchaView(profile *integrations.CaptchaProfile) (captchaView, error) {
	usage, err := forms.CaptchaProfileUsage(r.DB, profile.ID)
	if err != nil {
		return captchaView{}, err
	}
	return captchaView{
		ID:         profile.ID,
		Name:       profile.Name,
		SiteKey:    profile.SiteKey,
		SecretKey:  redact(profile.SecretKey, r.ShowSecrets),
		UsageCount: usage,
		CreatedAt:  profile.CreatedAt,
		UpdatedAt:  profile.UpdatedAt,
	}, nil
}
