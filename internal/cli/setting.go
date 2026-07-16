package cli

import (
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/accounts"
)

type settingView struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *Runner) runSetting(args []string) (any, error) {
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	action, actionArgs, err := requireAction("setting", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.settingList(actionArgs)
	case "get":
		return r.settingGet(actionArgs)
	case "set":
		return r.settingSet(actionArgs)
	case "delete":
		return r.settingDelete(actionArgs)
	default:
		return nil, usageError("unknown setting action: " + action)
	}
}

func (r *Runner) settingList(args []string) (any, error) {
	set := newFlagSet("setting.list")
	if err := r.parseFlags(set, "setting.list", args); err != nil {
		return nil, err
	}
	settings, err := accounts.ListSettings(r.DB)
	if err != nil {
		return nil, err
	}
	views := make([]settingView, 0, len(settings))
	for i := range settings {
		views = append(views, newSettingView(&settings[i], r.ShowSecrets))
	}
	return views, nil
}

func (r *Runner) settingGet(args []string) (any, error) {
	set := newFlagSet("setting.get")
	key := set.String("key", "", "setting key")
	if err := r.parseFlags(set, "setting.get", args); err != nil {
		return nil, err
	}
	if err := requireString(*key, "key"); err != nil {
		return nil, err
	}

	value, err := accounts.GetSetting(r.DB, strings.TrimSpace(*key))
	if err != nil {
		return nil, err
	}
	return map[string]any{"key": strings.TrimSpace(*key), "value": redact(value, r.ShowSecrets)}, nil
}

func (r *Runner) settingSet(args []string) (any, error) {
	set := newFlagSet("setting.set")
	key := set.String("key", "", "setting key")
	value := set.String("value", "", "setting value; avoid for secrets")
	valueFile := set.String("value-file", "", "path containing setting value, or - for stdin")
	if err := r.parseFlags(set, "setting.set", args); err != nil {
		return nil, err
	}
	if err := requireString(*key, "key"); err != nil {
		return nil, err
	}
	if flagWasSet(set, "value") == flagWasSet(set, "value-file") {
		return nil, usageError("set exactly one of --value or --value-file")
	}
	if *valueFile != "" {
		fileValue, err := readFileValue(*valueFile, r.Stdin)
		if err != nil {
			return nil, validationError(err.Error())
		}
		*value = fileValue
	}
	if err := accounts.SetSetting(r.DB, r.Logger, strings.TrimSpace(*key), *value); err != nil {
		return nil, err
	}
	return map[string]any{"key": strings.TrimSpace(*key), "updated": true}, nil
}

func (r *Runner) settingDelete(args []string) (any, error) {
	set := newFlagSet("setting.delete")
	key := set.String("key", "", "setting key")
	yes := set.Bool("yes", false, "confirm destructive operation")
	if err := r.parseFlags(set, "setting.delete", args); err != nil {
		return nil, err
	}
	if err := requireString(*key, "key"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("setting delete requires --yes")
	}
	if err := accounts.DeleteSetting(r.Logger, r.DB, strings.TrimSpace(*key)); err != nil {
		return nil, err
	}
	return map[string]any{"key": strings.TrimSpace(*key), "deleted": true}, nil
}

func newSettingView(setting *accounts.Settings, showSecrets bool) settingView {
	return settingView{
		Key:       setting.Key,
		Value:     redact(setting.Value, showSecrets),
		CreatedAt: setting.CreatedAt,
		UpdatedAt: setting.UpdatedAt,
	}
}
