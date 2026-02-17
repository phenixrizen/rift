# AGENTS.md

This file is the onboarding and operating guide for future agents working in this repository.

## Project Summary

`rift` is a Go CLI that orchestrates AWS IAM Identity Center (SSO) and EKS contexts across many AWS accounts.

Primary responsibilities:

- Discover accessible SSO accounts and roles.
- Discover EKS clusters across configured regions.
- Normalize naming into stable `rift-` profile/context names.
- Sync managed entries in `~/.aws/config` and `~/.kube/config`.
- Persist discovered inventory to state (`~/.config/rift/state.json`).

Core design goals:

- Idempotent sync.
- Safe ownership boundaries (only manage `rift-` resources).
- Good operator UX (`list`, `use`, `ui`, `graph`).

## Tech Stack

- Go `1.22+`
- AWS SDK for Go v2
- Cobra CLI
- Bubble Tea / Bubbles / Lip Gloss for TUI
- YAML config + JSON state
- Kubernetes client-go for namespace discovery

## Quick Commands

- Build: `make build` or `go build -o rift ./cmd/rift`
- Test: `make test` or `go test ./...`
- Lint: `make lint` (`go vet ./...`)
- Format: `make fmt`
- Version: `./rift version`

## Runtime Files

- Config: `~/.config/rift/config.yaml`
- State: `~/.config/rift/state.json`
- AWS config managed: `~/.aws/config`
- kubeconfig managed: `~/.kube/config` (or first path in `KUBECONFIG`)

## CLI Commands (Current)

- `rift init`
- `rift auth [--no-browser]`
- `rift sync [--dry-run]`
- `rift list`
- `rift use <filter>`
- `rift ui`
- `rift graph [flags]`
- `rift version`

## Command Behavior Notes

### `init`

- Prompts for `sso_start_url` and `sso_region`.
- Writes config file.
- Validates SSO cache presence; if missing/expired, tells user to run `rift auth`.

### `auth`

- Ensures AWS `[sso-session rift]` block exists in `~/.aws/config`.
- Runs `aws sso login --sso-session rift`.
- Falls back to legacy `--profile rift-auth` mode for older AWS CLI behavior.
- Prints approval hint for app prompt (`botocore-client-rift`).

### `sync`

- Runs discovery, naming normalization, AWS config sync, kubeconfig sync, state save.
- `--dry-run` computes and prints change summary without writing files.

### `list`

- Renders table from `state.json`.
- If state missing: instructs user to run `rift sync`.

### `use`

- Fuzzy-matches `KubeContext` from state.
- Executes `kubectl config use-context <match>`.

### `ui`

Main behavior:

- Search opens with `/` (inline search box sized to table pane width).
- Search closes with `enter` or `esc`.
- Global clear filter hotkey is `\` (main mode, not search mode).
- `enter` uses selected context.
- `k` launches `k9s --context <ctx> --command ns`.
- `s` runs sync (with spinner status + warning/error modal).
- `r` reloads state.
- Modal is scrollable (`up/down`, `PgUp/PgDn`, `j/k`, `g/G`).

### `graph`

- Supports `ascii` and `json`.
- Layers are Account -> Role -> Cluster -> Namespace (optional by depth/options).
- `--env` accepts `staging` (also maps `stg` alias to `staging`).

### `version`

- Prints `internal/version.ResolveCommit()`.

## Config Contract (`internal/config/config.go`)

Fields:

- `sso_start_url` (required)
- `sso_region` (required)
- `regions` (defaults to `us-east-1`, `us-west-2`)
- `namespace_defaults` (map by env)
- `discover_namespaces` (default `true`)

Normalization details:

- Regions are lowercased, deduped, sorted.
- Namespace default keys are lowercased and trimmed.
- Env namespace lookup supports compatibility mapping between `staging` and `stg`.

## Naming Contract (`internal/naming/naming.go`)

Slug:

- Lowercase, non `[a-z0-9]` collapsed to `-`, trimmed, fallback `unknown`.

Env inference:

- contains `prod` -> `prod`
- contains `staging` or `stage` -> `staging`
- contains `development` or `dev` -> `dev`
- contains `integration` or `int` -> `int`
- else -> `other`

Generated names:

- AWS profile: `rift-<env>-<account-slug>-<role-slug>`
- kube context: `rift-<env>-<account-slug>-<cluster-slug>`

Uniqueness:

- Collisions get numeric suffix (`-2`, `-3`, ...).

## Ownership and Safety Rules

AWS config (`internal/awsconfig/manager.go`):

- Manages/rewrites only sections with `profile rift-...`.
- Keeps non-rift profiles untouched.
- Maintains `[sso-session rift]`.

kubeconfig (`internal/kubeconfig/manager.go`):

- Manages/removes only contexts/clusters/users named `rift-...`.
- Keeps non-rift entries untouched.
- Uses exec auth: `aws eks get-token --profile <profile> --cluster-name <cluster> --region <region>`.

State:

- Written only by sync when not dry-run.

## Repo Map

- Entrypoint: `cmd/rift/main.go`
- Root command wiring: `internal/cli/root.go`
- Commands:
  - `internal/cli/init.go`
  - `internal/cli/auth.go`
  - `internal/cli/sync.go`
  - `internal/cli/list.go`
  - `internal/cli/use.go`
  - `internal/cli/ui.go`
  - `internal/cli/graph.go`
  - `internal/cli/version.go`
- Discovery: `internal/discovery/*`
- Namespace discovery: `internal/namespaces/discovery.go`
- Naming/state transform: `internal/naming/naming.go`
- State model IO: `internal/state/state.go`
- AWS config sync: `internal/awsconfig/manager.go`
- kubeconfig sync: `internal/kubeconfig/manager.go`
- Graph build/render: `internal/graphview/*`
- Table renderer: `internal/tableview/table.go`
- Version resolution: `internal/version/version.go`

## UI Styling and Rendering Constraints

Color conventions currently in `internal/cli/ui.go`:

- Accent/cyan: `81`
- Status/spinner green: `42`

Current UI layout contract:

- Top row is two columns: left has `TRAVERSE THE CLOUD RIFT` + version; right has RIFT ASCII art.
- Search input is hidden by default and only shown in search mode (`/`).
- Search box width must match the left table pane width.
- Hotkeys are rendered as a single status line at the bottom.
- Table env display uses `stg` when the canonical env is `staging` to avoid truncation (`displayEnv` in `internal/cli/ui.go`).

Important rendering rule:

- For wide bordered UI blocks (especially modal/search), **wrap content before applying border/style**.
- This avoids right-edge clipping in some terminals.

## Known Gotchas

- Search filter state lives in `m.search.Value()`; clearing filter should clear that value and re-run `applyFilter()`.
- Table cursor rendering can drift if table width/height are not kept in sync with current layout; use `syncTableLayout()` before table update events.
- AWS CLI behavior differs by version for `sso login`; keep both modern and legacy fallback paths in `auth`.
- Namespace discovery is best-effort and logs warnings; do not fail sync solely due to per-cluster namespace errors.

## Versioning / Build Metadata

- Default version is `v0.0.1` from `internal/version/version.go`.
- `Commit` can be overridden with:
  - `-ldflags "-X github.com/phenixrizen/rift/internal/version.Commit=<sha>"`
- `ResolveCommit()` also checks build info and `RIFT_COMMIT`.

## Development Checklist for Changes

Before completing a change:

- Run `gofmt -w` on touched Go files.
- Run `go test ./...`.
- Run `go build ./cmd/rift`.
- If UI touched, verify:
  - Search open/close/clear flow
  - Table selection + scrolling
  - Sync modal display and scrolling
  - No right-edge clipping in active terminal
