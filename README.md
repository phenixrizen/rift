# rift

Rift is a Go CLI for orchestrating AWS IAM Identity Center (SSO) access and EKS kube contexts across many accounts.

It discovers all accounts/permission sets available to the current SSO user, normalizes naming, and keeps:

- `~/.aws/config` (managed `rift-` profiles)
- `~/.kube/config` (managed `rift-` contexts)

in sync over time as new accounts and clusters are added.

## Features

- `rift init` interactive config bootstrap
- `rift auth` run AWS SSO login using Rift config
- `rift sync` idempotent discovery + sync with `--dry-run`
- `rift list` account/role/cluster table
- `rift use <filter>` fuzzy context switch
- `rift ui` k9s-style TUI (search, sync, refresh, use)
- `rift graph` ASCII/JSON topology graph with filters and depth control

## Requirements

- Go 1.22+
- AWS CLI v2 installed and configured for SSO
- Valid SSO login cache (`rift auth` or `aws sso login`)
- `kubectl` for `rift use` and TUI context switching
- `k9s` for TUI context-specific namespace browsing

## Installation

```bash
go build -o rift ./cmd/rift
```

OR

```bash
go install github.com/phenixrizen/rift/cmd/rift@latest
```

## Configuration

Default files:

- `~/.config/rift/config.yaml`
- `~/.config/rift/state.json`

Initialize config:

```bash
rift init
```

If SSO token is missing/expired, Rift prints:

```text
Run: rift auth
```

See `config.example.yaml` for all supported config keys.

`discover_namespaces` defaults to `true`. Namespace discovery is best-effort and does not block profile/context sync.

## Command Usage

### `rift init`

Interactive setup:

- SSO start URL
- SSO region

Writes config and validates local SSO token cache.

### `rift auth [--no-browser]`

Ensures `[sso-session rift]` in `~/.aws/config` from `config.yaml` and runs:

- `aws sso login --sso-session rift`

Use `--no-browser` for device flow environments.

### `rift sync [--dry-run]`

- Discovers SSO accounts and roles
- Enumerates EKS clusters in configured regions
- Discovers cluster namespaces (when `discover_namespaces: true`)
- Generates canonical names:
  - AWS profile: `rift-<env>-<account-slug>-<role-slug>`
  - Kube context: `rift-<env>-<account-slug>-<cluster-slug>`
- Syncs managed entries in AWS and kube configs
- Writes `state.json` (unless `--dry-run`)

Safety:

- Only rewrites/deletes `rift-` profiles/contexts
- Never touches non-`rift-` user entries

### `rift list`

Prints:

`Env | Account | Role | Region | Cluster | AWS Profile | Kube Context`

### `rift use <filter>`

Fuzzy-matches known context names from state and runs:

```bash
kubectl config use-context <match>
```

### `rift ui`

TUI layout:

- Top-left: `TRAVERSE THE CLOUD RIFT` + version hash
- Top-right: `RIFT` ASCII
- Left: context table
- Right: details (account ID, role, cluster ARN)
  - Hotkeys box directly under details
  - `RIFT` ASCII in the lower-right corner
- Bottom: status line

Keybinds:

- `/` open boxed search input
- `\` clear search filter
- `enter` use context
- `k` launch k9s on namespace selector for selected context
- `s` sync
- `r` refresh state file
- `q` quit

### `rift graph [flags]`

Builds `Account -> Role -> Cluster -> Namespace` topology (namespace optional).

Flags:

- `--env <prod|staging|dev|int|other|all>`
- `--account <substring>`
- `--role <substring>`
- `--region <region>`
- `--cluster <substring>`
- `--namespaces`
- `--format <ascii|json>`
- `--max-width <n>`
- `--depth <2|3|4>`

Examples:

```bash
rift graph --env prod --depth 3
rift graph --role admin --format json
```

## Environment Inference

Rift infers `env` from names:

- contains `prod` -> `prod`
- contains `staging` or `stage` -> `stg`
- contains `dev` or `development` -> `dev`
- contains `int` or `integration` -> `int`
- otherwise -> `other`

## Development

```bash
make build
make test
make lint
```
