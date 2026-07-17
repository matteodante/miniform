---
name: miniform-cli
description: Operate and automate a local or self-hosted Miniform instance through the `miniform` CLI. Use when an agent needs to inspect or manage Miniform accounts, runtime configuration, database settings, forms, embed code, mailer or captcha profiles, submissions, uploaded files, webhook/email events, retries, or when it needs deterministic JSON output and safe secret handling instead of the Web UI.
---

# Miniform CLI

Use the CLI as the administrative API for agents. Prefer it over direct SQLite edits, HTTP form emulation, or browser automation.

## Operating Workflow

1. Read [AGENTS.md](../../../AGENTS.md).
2. Identify the target instance and choose one invocation:
   - installed binary: `miniform`;
   - source checkout: `go run ./cmd/miniform`;
   - OCI deployment: `docker exec <container> miniform`.
3. Preserve the server's `MINIFORM_ENV`, `MINIFORM_DATA_DIR`, database path, and container context. Never infer that a database in the current directory is the live instance.
4. Run discovery before composing flags:

   ```bash
   miniform commands --json
   miniform --json help form
   miniform --json config show
   ```

5. Inspect the current resource with `list`, `get`, or `show` before mutating it.
6. Execute one focused mutation. Require user authorization for deletion, token rotation, redelivery, credential reset, or any other consequential action; `--yes` is a safety declaration, not authorization.
7. Read the resource again and verify the requested state.
8. Report the target instance, affected IDs, command result, and whether a restart is required.

Treat `miniform commands --json` as the authoritative command contract. Read the relevant section of [docs/cli.md](../../../docs/cli.md) when a command uses profiles, JSON files, uploads, destructive behavior, or secrets.

## Output Contract

Use `--json` for all data and administration commands. Parse stdout only after exit code `0`:

```json
{"ok":true,"command":"form.list","data":[]}
```

Parse failures from stderr:

```json
{"ok":false,"command":"form.get","error":{"code":"not_found","message":"resource not found"}}
```

Handle exit codes explicitly:

| Exit | Meaning | Agent response |
| ---: | --- | --- |
| `0` | Success | Validate returned state |
| `2` | Usage error | Re-read command help; do not retry unchanged |
| `3` | Validation/authentication | Correct input or request missing credentials |
| `4` | Not found | Re-list resources and resolve the intended ID |
| `5` | Conflict/reference | Inspect dependencies; do not force deletion |
| `10` | Storage/internal failure | Stop mutations and report the failure |

Check `supports_json` in the command catalog. Legacy deployment commands such as `install`, `update`, and `reload` retain human-oriented output.

## Safety Rules

- Default to redacted output. Use `--show-secrets` only when the requested result requires a live form token or stored credential.
- Never pass passwords, API keys, SMTP secrets, session secrets, captcha secrets, or webhook secrets directly as command-line values.
- Supply secrets with `--*-file PATH` or `--*-file -` through stdin. Only one secret per command may use stdin; use permission-restricted files for additional secrets.
- Never print, summarize, log, or retain secret-bearing output unnecessarily.
- Use explicit `--flag=true` or `--flag=false` for boolean updates. Omitted update flags preserve current values.
- Use the matching `--clear-*` flag to remove a stored optional value. Do not substitute empty strings unless the command contract says to.
- Never use `setting set` for forms, mailers, captcha, accounts, or other domain-owned records. Use their dedicated resources.
- Never modify SQLite directly to bypass validation, reference checks, retry transactions, or file cleanup.
- Do not retry an identical failed mutation automatically unless the failure is clearly transient and the operation is idempotent.

## Resource Routing

| Intent | Resource/actions |
| --- | --- |
| Operator identity and credentials | `account show`, `set-email`, `change-password`, `reset-password` |
| Effective environment and dotenv | `config show`, `set`, `unset` |
| Generic database-backed keys | `setting list`, `get`, `set`, `delete` |
| Endpoints, delivery policy, templates, embed HTML | `form list`, `get`, `code`, `create`, `update`, `rotate-token`, `delete`, `template-list`, `template-get` |
| SMTP routes | `mailer list`, `get`, `create`, `update`, `delete` |
| Turnstile profiles and policies | `captcha list`, `get`, `create`, `update`, `delete` |
| Inbox data and attachments | `submission list`, `get`, `create`, `delete`, `file-list`, `file-copy` |
| Delivery history and redelivery | `event list`, `retry` |

## Common Agent Patterns

Inspect without exposing credentials:

```bash
miniform --json config show
miniform --json form list
miniform --json mailer list
miniform --json captcha list
```

Create a form from a built-in template, then verify it:

```bash
miniform --json form template-list
miniform --json form create --template contact --allowed-origins example.com
miniform --json form get --slug contact
miniform --json form code --slug contact
```

Update one field without overwriting omitted settings:

```bash
miniform --json form get --id 1
miniform --json form update --id 1 --email-enabled=false
miniform --json form get --id 1
```

Filter the inbox and inspect one record:

```bash
miniform --json submission list --form-id 1 --range 30d --spam=false --page 1 --per-page 50
miniform --json submission get --id 42
miniform --json submission file-list --id 42
```

Inspect a failed delivery before requesting redelivery:

```bash
miniform --json event list --type webhook --status failed --limit 100
miniform --json event retry --type webhook --id 12 --yes
miniform --json event list --type webhook --form-id 1
```

For `config set` or `config unset`, report `restart_required` and do not restart or reload the service unless the user authorized it. In containers, change persistent container configuration rather than an ephemeral `.env` inside the image.

For raw file output, use `submission file-copy --output -` without `--json`; otherwise copy to an explicit path and keep overwrite protection unless the user authorized `--force`.
