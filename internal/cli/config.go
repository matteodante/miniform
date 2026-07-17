package cli

import "fmt"

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
	if action != "show" {
		return nil, usageError("unknown config action: " + action)
	}
	return r.configShow(actionArgs)
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
		MaxInputFields:        r.Config.MaxInputFields,
		WebhookSignature:      r.Config.Webhook.SignatureHeader,
		WebhookRetryLimit:     r.Config.Webhook.RetryLimit,
		WebhookBackoff:        r.Config.Webhook.BackoffSchedule,
	}, nil
}
