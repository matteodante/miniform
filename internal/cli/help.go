package cli

import (
	"fmt"
	"sort"
	"strings"
)

// CommandSpec is the machine-readable contract exposed by `miniform commands`.
type CommandSpec struct {
	Name             string   `json:"name"`
	Summary          string   `json:"summary"`
	Mutates          bool     `json:"mutates"`
	RequiresDatabase bool     `json:"requires_database"`
	SupportsJSON     bool     `json:"supports_json"`
	Flags            []string `json:"flags,omitempty"`
	Examples         []string `json:"examples,omitempty"`
	Notes            []string `json:"notes,omitempty"`
}

func commandManifest() []CommandSpec {
	commands := []CommandSpec{
		{Name: "commands", Summary: "Print the complete machine-readable command catalog."},
		{Name: "help", Summary: "Show help for all commands or one command family.", Flags: []string{"RESOURCE", "ACTION"}},
		{Name: "serve", Summary: "Start the Miniform HTTP server.", Flags: []string{"--seed", "--version"}},
		{Name: "install", Summary: "Install the self-hosted container deployment.", Mutates: true},
		{Name: "update", Summary: "Update the installed container deployment.", Mutates: true},
		{Name: "reload", Summary: "Reload the installed container deployment.", Mutates: true},
		{Name: "backup", Summary: "Create a verified deployment database backup.", Mutates: true},
		{Name: "restore-db", Summary: "Restore the installed deployment database from backup.", Mutates: true},
		{Name: "check", Summary: "Run deployment security checks."},
		{Name: "version", Summary: "Print build version and commit."},
		{Name: "account show", Summary: "Show the operator account without its password hash.", RequiresDatabase: true},
		{Name: "account set-email", Summary: "Change the operator email after password verification.", Mutates: true, RequiresDatabase: true, Flags: []string{"--email STRING", "--current-password-file PATH|-"}, Examples: []string{"miniform account set-email --email admin@example.com --current-password-file -"}},
		{Name: "account change-password", Summary: "Change the operator password after verifying the current password.", Mutates: true, RequiresDatabase: true, Flags: []string{"--current-password-file PATH|-", "--new-password-file PATH|-"}},
		{Name: "account reset-password", Summary: "Administratively replace the password without the old password.", Mutates: true, RequiresDatabase: true, Flags: []string{"--email STRING", "--new-password-file PATH|-"}, Notes: []string{"Use only from a trusted local administrative shell."}},
		{Name: "config show", Summary: "Show effective runtime configuration with secrets redacted.", Flags: []string{"--show-secrets"}},
		{Name: "form list", Summary: "List configured form endpoints.", RequiresDatabase: true},
		{Name: "form get", Summary: "Get one form by id or slug.", RequiresDatabase: true, Flags: []string{"--id UINT", "--slug STRING", "--show-secrets"}},
		{Name: "form code", Summary: "Build copyable native form HTML with optional captcha.", RequiresDatabase: true, Flags: []string{"--id UINT", "--slug STRING", "--base-url URL", "--show-secrets"}, Notes: []string{"Set --base-url to emit a deployable absolute action URL.", "The token is replaced with YOUR_FORM_TOKEN unless --show-secrets is set."}},
		{Name: "form create", Summary: "Create a form endpoint and its delivery policies.", Mutates: true, RequiresDatabase: true, Flags: []string{"--template STRING", "--name STRING", "--slug STRING", "--allowed-origins STRING", "--uploads-enabled", "--generated-html-file PATH", "--mailer-profile-id UINT", "--captcha-profile-id UINT", "--email-enabled", "--email-recipient STRING", "--email-format text|html", "--webhook-enabled", "--webhook-url URL", "--webhook-secret-file PATH|-", "--webhook-headers-file PATH"}},
		{Name: "form update", Summary: "Update form and delivery settings; omitted flags preserve current values.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--name STRING", "--slug STRING", "--allowed-origins STRING", "--uploads-enabled=BOOL", "--generated-html-file PATH", "--clear-generated-html", "--mailer-profile-id UINT", "--clear-mailer-profile", "--captcha-profile-id UINT", "--clear-captcha-profile", "--email-enabled=BOOL", "--email-recipient STRING", "--email-format text|html", "--webhook-enabled=BOOL", "--webhook-url URL", "--webhook-secret-file PATH|-", "--clear-webhook-secret", "--webhook-headers-file PATH", "--clear-webhook-headers"}},
		{Name: "form rotate-token", Summary: "Replace a form submission token and invalidate the old token.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes", "--show-secrets"}},
		{Name: "form template-list", Summary: "List built-in form templates."},
		{Name: "form template-get", Summary: "Get or render one built-in form template.", Flags: []string{"--template STRING", "--action URL"}},
		{Name: "form delete", Summary: "Delete a form and related deliveries/submissions.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes"}},
		{Name: "email list", Summary: "List email notifications for one form.", RequiresDatabase: true, Flags: []string{"--form-id UINT"}},
		{Name: "email get", Summary: "Get one email notification including its templates.", RequiresDatabase: true, Flags: []string{"--id UINT"}},
		{Name: "email create", Summary: "Add an independent email notification to a form.", Mutates: true, RequiresDatabase: true, Flags: []string{"--form-id UINT", "--name STRING", "--enabled", "--mailer-profile-id UINT", "--recipient-source static|field", "--recipient STRING", "--reply-to-source none|static|field", "--reply-to STRING", "--subject-template STRING", "--format text|html", "--text-template-file PATH", "--html-template-file PATH"}},
		{Name: "email update", Summary: "Update an email notification; omitted flags preserve current values.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--name STRING", "--enabled=BOOL", "--mailer-profile-id UINT", "--clear-mailer-profile", "--recipient-source static|field", "--recipient STRING", "--reply-to-source none|static|field", "--reply-to STRING", "--subject-template STRING", "--format text|html", "--text-template-file PATH", "--html-template-file PATH"}},
		{Name: "email delete", Summary: "Delete an email notification while preserving delivery history.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes"}},
		{Name: "mailer list", Summary: "List reusable mailer profiles with secrets redacted.", RequiresDatabase: true, Flags: []string{"--show-secrets"}},
		{Name: "mailer get", Summary: "Get a mailer profile and usage count.", RequiresDatabase: true, Flags: []string{"--id UINT", "--show-secrets"}},
		{Name: "mailer create", Summary: "Create an SMTP profile.", Mutates: true, RequiresDatabase: true, Flags: []string{"--name STRING", "--default-from-name STRING", "--default-from-email STRING", "--smtp-host STRING", "--smtp-port INT", "--smtp-username STRING", "--smtp-password-file PATH|-", "--smtp-encryption starttls|tls|none"}},
		{Name: "mailer update", Summary: "Update a mailer; omitted flags preserve current values.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--name STRING", "--default-from-name STRING", "--default-from-email STRING", "--smtp-host STRING", "--smtp-port INT", "--smtp-username STRING", "--smtp-password-file PATH|-", "--smtp-encryption starttls|tls|none", "--clear-smtp-password"}},
		{Name: "mailer delete", Summary: "Delete an unused mailer profile.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes"}},
		{Name: "captcha list", Summary: "List captcha profiles with secrets redacted.", RequiresDatabase: true, Flags: []string{"--show-secrets"}},
		{Name: "captcha get", Summary: "Get a captcha profile and usage count.", RequiresDatabase: true, Flags: []string{"--id UINT", "--show-secrets"}},
		{Name: "captcha create", Summary: "Create a Turnstile profile.", Mutates: true, RequiresDatabase: true, Flags: []string{"--name STRING", "--site-key STRING", "--secret-key-file PATH|-"}},
		{Name: "captcha update", Summary: "Update a Turnstile profile; omitted flags preserve current values.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--name STRING", "--site-key STRING", "--secret-key-file PATH|-"}},
		{Name: "captcha delete", Summary: "Delete an unused captcha profile.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes"}},
		{Name: "submission list", Summary: "List and filter inbox submissions.", RequiresDatabase: true, Flags: []string{"--form-id UINT", "--range all|7d|30d|90d", "--query STRING", "--spam true|false", "--page INT", "--per-page INT"}},
		{Name: "submission get", Summary: "Get a submission with payload, events, and files.", RequiresDatabase: true, Flags: []string{"--id UINT"}},
		{Name: "submission delete", Summary: "Delete a submission and its uploaded files.", Mutates: true, RequiresDatabase: true, Flags: []string{"--id UINT", "--yes"}},
		{Name: "submission create", Summary: "Create a submission from a JSON payload with optional files.", Mutates: true, RequiresDatabase: true, Flags: []string{"--form-id UINT", "--slug STRING", "--data-file PATH|-", "--file FIELD=PATH (repeatable)", "--user-agent STRING"}},
		{Name: "submission file-list", Summary: "List uploaded files for one submission.", RequiresDatabase: true, Flags: []string{"--id UINT"}},
		{Name: "submission file-copy", Summary: "Copy one uploaded file to a local path or stdout.", RequiresDatabase: true, Flags: []string{"--id UINT", "--file-id UINT", "--output PATH|-", "--force"}},
		{Name: "event list", Summary: "List webhook or email delivery events.", RequiresDatabase: true, Flags: []string{"--type webhook|email", "--form-id UINT", "--status STRING", "--limit INT"}},
		{Name: "event retry", Summary: "Schedule one failed webhook or email event for immediate retry.", Mutates: true, RequiresDatabase: true, Flags: []string{"--type webhook|email", "--id UINT", "--yes"}},
	}
	for index := range commands {
		commands[index].SupportsJSON = supportsJSON(commands[index].Name)
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func supportsJSON(command string) bool {
	resource, _, _ := strings.Cut(command, " ")
	switch resource {
	case "account", "captcha", "commands", "config", "email", "event", "form", "help", "mailer", "submission":
		return true
	default:
		return false
	}
}

//nolint:errcheck // Help cannot recover from a closed output stream.
func (r *Runner) runHelp(args []string) (any, error) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if r.JSON {
		if query == "" {
			return commandManifest(), nil
		}
		matching := matchingCommands(query)
		if len(matching) == 0 {
			return nil, usageError("unknown command help topic: " + query)
		}
		return matching, nil
	}

	if query == "" {
		r.writeRootHelp()
		return nil, nil
	}
	matching := matchingCommands(query)
	if len(matching) == 0 {
		return nil, usageError("unknown command help topic: " + query)
	}
	for i, spec := range matching {
		if i > 0 {
			fmt.Fprintln(r.Stdout)
		}
		writeSpec(r, spec)
	}
	return nil, nil
}

//nolint:errcheck // Help cannot recover from a closed output stream.
func (r *Runner) writeRootHelp() {
	fmt.Fprintln(r.Stdout, "Miniform CLI — local administration and automation")
	fmt.Fprintln(r.Stdout)
	fmt.Fprintln(r.Stdout, "Usage:")
	fmt.Fprintln(r.Stdout, "  miniform [--json] [--show-secrets] <resource> <action> [flags]")
	fmt.Fprintln(r.Stdout)
	fmt.Fprintln(r.Stdout, "Resources:")
	fmt.Fprintln(r.Stdout, "  account     Operator email and password")
	fmt.Fprintln(r.Stdout, "  config      Effective runtime configuration")
	fmt.Fprintln(r.Stdout, "  form        Form endpoints and delivery policies")
	fmt.Fprintln(r.Stdout, "  mailer      SMTP profiles")
	fmt.Fprintln(r.Stdout, "  captcha     Captcha profiles")
	fmt.Fprintln(r.Stdout, "  submission  Inbox entries and uploaded files")
	fmt.Fprintln(r.Stdout, "  event       Webhook and email delivery events")
	fmt.Fprintln(r.Stdout)
	fmt.Fprintln(r.Stdout, "Discovery:")
	fmt.Fprintln(r.Stdout, "  miniform commands --json")
	fmt.Fprintln(r.Stdout, "  miniform help <resource> [action]")
	fmt.Fprintln(r.Stdout)
	fmt.Fprintln(r.Stdout, "Existing process/deployment commands remain available: serve, install, update, reload, backup, restore-db, check, version.")
}

//nolint:errcheck // Help cannot recover from a closed output stream.
func (r *Runner) writeCommandHelp(command string) {
	matching := matchingCommands(strings.ReplaceAll(command, ".", " "))
	if len(matching) == 0 {
		r.writeRootHelp()
		return
	}
	for i, spec := range matching {
		if i > 0 {
			fmt.Fprintln(r.Stdout)
		}
		writeSpec(r, spec)
	}
}

func matchingCommands(query string) []CommandSpec {
	query = strings.TrimSpace(strings.ReplaceAll(query, ".", " "))
	var matching []CommandSpec
	for _, spec := range commandManifest() {
		if spec.Name == query || strings.HasPrefix(spec.Name, query+" ") {
			matching = append(matching, spec)
		}
	}
	return matching
}

//nolint:errcheck // Help cannot recover from a closed output stream.
func writeSpec(r *Runner, spec CommandSpec) {
	fmt.Fprintf(r.Stdout, "Usage: miniform %s [flags]\n", spec.Name)
	fmt.Fprintf(r.Stdout, "  %s\n", spec.Summary)
	if len(spec.Flags) > 0 {
		fmt.Fprintln(r.Stdout, "Flags:")
		for _, item := range spec.Flags {
			fmt.Fprintf(r.Stdout, "  %s\n", item)
		}
	}
	if len(spec.Notes) > 0 {
		fmt.Fprintln(r.Stdout, "Notes:")
		for _, item := range spec.Notes {
			fmt.Fprintf(r.Stdout, "  %s\n", item)
		}
	}
	if len(spec.Examples) > 0 {
		fmt.Fprintln(r.Stdout, "Examples:")
		for _, item := range spec.Examples {
			fmt.Fprintf(r.Stdout, "  %s\n", item)
		}
	}
}
