package cli

import (
	"flag"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type captchaView struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	SecretKey    string    `json:"secret_key,omitempty"`
	SiteKeysJSON string    `json:"site_keys_json,omitempty"`
	PolicyJSON   string    `json:"policy_json,omitempty"`
	UsageCount   int64     `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type captchaFlags struct {
	name          *string
	provider      *string
	secretKeyFile *string
	siteKeysFile  *string
	policyFile    *string
}

func (r *Runner) runCaptcha(args []string) (any, error) {
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
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
	params, err := r.captchaParams(set, flags, integrations.CaptchaProfileParams{Provider: "turnstile"})
	if err != nil {
		return nil, err
	}
	if err := validateCaptchaParams(params); err != nil {
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
	clearSecret := set.Bool("clear-secret-key", false, "clear provider secret key")
	clearSiteKeys := set.Bool("clear-site-keys", false, "clear site keys JSON")
	clearPolicy := set.Bool("clear-policy", false, "clear policy JSON")
	if err := r.parseFlags(set, "captcha.update", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if *clearSecret && *flags.secretKeyFile != "" {
		return nil, usageError("--clear-secret-key conflicts with --secret-key-file")
	}
	if *clearSiteKeys && *flags.siteKeysFile != "" {
		return nil, usageError("--clear-site-keys conflicts with --site-keys-file")
	}
	if *clearPolicy && *flags.policyFile != "" {
		return nil, usageError("--clear-policy conflicts with --policy-file")
	}

	current, err := integrations.GetCaptchaProfileByID(r.DB, *id)
	if err != nil {
		return nil, err
	}
	params, err := r.captchaParams(set, flags, captchaParamsFromProfile(current))
	if err != nil {
		return nil, err
	}
	if *clearSecret {
		params.SecretKey = ""
	}
	if *clearSiteKeys {
		params.SiteKeysJSON = ""
	}
	if *clearPolicy {
		params.PolicyJSON = ""
	}
	if err := validateCaptchaParams(params); err != nil {
		return nil, err
	}
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
		provider:      set.String("provider", "", "captcha provider"),
		secretKeyFile: set.String("secret-key-file", "", "path containing provider secret key, or - for stdin"),
		siteKeysFile:  set.String("site-keys-file", "", "path containing site keys JSON"),
		policyFile:    set.String("policy-file", "", "path containing policy JSON"),
	}
}

func (r *Runner) captchaParams(set *flag.FlagSet, flags captchaFlags, params integrations.CaptchaProfileParams) (integrations.CaptchaProfileParams, error) {
	applyStringFlag(set, "name", flags.name, &params.Name)
	applyStringFlag(set, "provider", flags.provider, &params.Provider)

	var err error
	if *flags.secretKeyFile != "" {
		params.SecretKey, err = readFileValue(*flags.secretKeyFile, r.Stdin)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	if *flags.siteKeysFile != "" {
		params.SiteKeysJSON, err = readContentFile(*flags.siteKeysFile)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	if *flags.policyFile != "" {
		params.PolicyJSON, err = readContentFile(*flags.policyFile)
		if err != nil {
			return params, validationError(err.Error())
		}
	}
	return params, nil
}

func validateCaptchaParams(params integrations.CaptchaProfileParams) error {
	if strings.ToLower(strings.TrimSpace(params.Provider)) != "turnstile" {
		return validationError("provider must be turnstile")
	}
	return nil
}

func captchaParamsFromProfile(profile *integrations.CaptchaProfile) integrations.CaptchaProfileParams {
	return integrations.CaptchaProfileParams{
		Name:         profile.Name,
		Provider:     profile.Provider,
		SecretKey:    profile.SecretKey,
		SiteKeysJSON: profile.SiteKeysJSON,
		PolicyJSON:   profile.PolicyJSON,
	}
}

func (r *Runner) newCaptchaView(profile *integrations.CaptchaProfile) (captchaView, error) {
	usage, err := forms.CaptchaProfileUsage(r.DB, profile.ID)
	if err != nil {
		return captchaView{}, err
	}
	return captchaView{
		ID:           profile.ID,
		Name:         profile.Name,
		Provider:     profile.Provider,
		SecretKey:    redact(profile.SecretKey, r.ShowSecrets),
		SiteKeysJSON: profile.SiteKeysJSON,
		PolicyJSON:   profile.PolicyJSON,
		UsageCount:   usage,
		CreatedAt:    profile.CreatedAt,
		UpdatedAt:    profile.UpdatedAt,
	}, nil
}
