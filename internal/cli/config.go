package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var knownConfigKeys = map[string]bool{
	"MINIFORM_ENV":                      false,
	"MINIFORM_PORT":                     false,
	"MINIFORM_SESSION_SECRET":           true,
	"MINIFORM_ANON_SALT":                true,
	"MINIFORM_LOG_LEVEL":                false,
	"MINIFORM_DATA_DIR":                 false,
	"MINIFORM_DATABASE_FILENAME":        false,
	"MINIFORM_DATABASE_PATH":            false,
	"MINIFORM_LOGS_DIR":                 false,
	"MINIFORM_SESSION_TIMEOUT_SECONDS":  false,
	"MINIFORM_DEBUG":                    false,
	"MINIFORM_MAX_INPUT_FIELDS":         false,
	"MINIFORM_WEBHOOK_SIGNATURE_HEADER": false,
	"MINIFORM_WEBHOOK_RETRY_LIMIT":      false,
	"MINIFORM_WEBHOOK_BACKOFF_SCHEDULE": false,
}

type effectiveConfigView struct {
	Environment           string `json:"environment"`
	Port                  string `json:"port"`
	LogLevel              string `json:"log_level"`
	DataDirectory         string `json:"data_directory"`
	DatabaseFilename      string `json:"database_filename"`
	DatabasePath          string `json:"database_path"`
	LogsDirectory         string `json:"logs_directory"`
	SessionTimeoutSeconds int    `json:"session_timeout_seconds"`
	SessionSecret         string `json:"session_secret"`
	AnonSalt              string `json:"anon_salt"`
	MaxInputFields        int    `json:"max_input_fields"`
	WebhookSignature      string `json:"webhook_signature_header"`
	WebhookRetryLimit     int    `json:"webhook_retry_limit"`
	WebhookBackoff        string `json:"webhook_backoff_schedule"`
}

func (r *Runner) runConfig(args []string) (any, error) {
	action, actionArgs, err := requireAction("config", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "show":
		return r.configShow(actionArgs)
	case "set":
		return r.configSet(actionArgs)
	case "unset":
		return r.configUnset(actionArgs)
	default:
		return nil, usageError("unknown config action: " + action)
	}
}

func (r *Runner) configShow(args []string) (any, error) {
	set := newFlagSet("config.show")
	if err := r.parseFlags(set, "config.show", args); err != nil {
		return nil, err
	}
	if r.Config == nil {
		return nil, internalError("load configuration", fmt.Errorf("configuration dependency is unavailable"))
	}
	return effectiveConfigView{
		Environment:           r.Config.Environment,
		Port:                  r.Config.Port,
		LogLevel:              r.Config.LogLevel,
		DataDirectory:         r.Config.DataDirectory,
		DatabaseFilename:      r.Config.DatabaseFilename,
		DatabasePath:          r.Config.DatabasePath,
		LogsDirectory:         r.Config.LogsDirectory,
		SessionTimeoutSeconds: r.Config.SessionTimeout,
		SessionSecret:         redact(r.Config.SessionSecret, r.ShowSecrets),
		AnonSalt:              redact(r.Config.AnonSalt, r.ShowSecrets),
		MaxInputFields:        r.Config.MaxInputFields,
		WebhookSignature:      r.Config.Webhook.SignatureHeader,
		WebhookRetryLimit:     r.Config.Webhook.RetryLimit,
		WebhookBackoff:        r.Config.Webhook.BackoffSchedule,
	}, nil
}

func (r *Runner) configSet(args []string) (any, error) {
	set := newFlagSet("config.set")
	key := set.String("key", "", "MINIFORM_* configuration key")
	value := set.String("value", "", "configuration value; not accepted for secret keys")
	valueFile := set.String("value-file", "", "path containing value, or - for stdin")
	envFile := set.String("env-file", ".env", "dotenv file to update")
	if err := r.parseFlags(set, "config.set", args); err != nil {
		return nil, err
	}
	normalizedKey, secret, err := validateConfigKey(*key)
	if err != nil {
		return nil, err
	}
	if flagWasSet(set, "value") == flagWasSet(set, "value-file") {
		return nil, usageError("set exactly one of --value or --value-file")
	}
	if secret && flagWasSet(set, "value") {
		return nil, usageError("secret configuration requires --value-file to avoid process-list exposure")
	}
	if *valueFile != "" {
		fileValue, err := readFileValue(*valueFile, r.Stdin)
		if err != nil {
			return nil, validationError(err.Error())
		}
		*value = fileValue
	}
	if err := updateEnvFile(*envFile, normalizedKey, value); err != nil {
		return nil, internalError("update env file", err)
	}
	return map[string]any{
		"key":              normalizedKey,
		"env_file":         *envFile,
		"updated":          true,
		"restart_required": true,
	}, nil
}

func (r *Runner) configUnset(args []string) (any, error) {
	set := newFlagSet("config.unset")
	key := set.String("key", "", "MINIFORM_* configuration key")
	envFile := set.String("env-file", ".env", "dotenv file to update")
	if err := r.parseFlags(set, "config.unset", args); err != nil {
		return nil, err
	}
	normalizedKey, _, err := validateConfigKey(*key)
	if err != nil {
		return nil, err
	}
	if err := updateEnvFile(*envFile, normalizedKey, nil); err != nil {
		return nil, internalError("update env file", err)
	}
	return map[string]any{
		"key":              normalizedKey,
		"env_file":         *envFile,
		"deleted":          true,
		"restart_required": true,
	}, nil
}

func validateConfigKey(key string) (string, bool, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	secret, ok := knownConfigKeys[key]
	if !ok {
		return "", false, validationError("unsupported configuration key: " + key)
	}
	return key, secret, nil
}

func updateEnvFile(path, key string, value *string) error {
	lines, mode, err := readEnvLines(path)
	if err != nil {
		return err
	}

	pattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	updated := make([]string, 0, len(lines)+1)
	written := false
	for _, line := range lines {
		if !pattern.MatchString(line) {
			updated = append(updated, line)
			continue
		}
		if value != nil && !written {
			updated = append(updated, key+"="+encodeEnvValue(*value))
			written = true
		}
	}
	if value != nil && !written {
		updated = append(updated, key+"="+encodeEnvValue(*value))
	}

	content := strings.Join(updated, "\n")
	if len(updated) > 0 {
		content += "\n"
	}
	return writeFileAtomically(path, []byte(content), mode)
}

func readEnvLines(path string) (lines []string, mode os.FileMode, resultErr error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if err := file.Close(); resultErr == nil {
			resultErr = err
		}
	}()

	mode = os.FileMode(0o600)
	if info, statErr := file.Stat(); statErr == nil {
		mode = info.Mode().Perm()
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return lines, mode, nil
}

func writeFileAtomically(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".miniform-env-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func encodeEnvValue(value string) string {
	safe := regexp.MustCompile(`^[A-Za-z0-9_./:@+,-]*$`)
	if safe.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}
