# Runway

Self-hosted GitHub Actions runner moderator and ephemeral JIT runner control plane, compiled into a single compact Go binary with an embedded TanStack Start web console.

## Features

- **Policy Moderation**: Priority-ordered rules for repository/branch/actor/label globs, fork-PR blocking, per-repo concurrency caps, and manual approval queues.
- **Ephemeral JIT Provisioning**: Mints single-use GitHub Actions JIT runner configs via GitHub App RS256 JWT auth (`docker` or `simulated` provisioners).
- **Security Hardened**: Constant-time HMAC-SHA256 webhook verification, delivery idempotency, Argon2id auth, HttpOnly cookies + `X-CSRF-Token`, rate limiting, and `scratch` non-root container runtime.
- **Single-Binary UI**: TanStack Start SPA embedded into `cmd/server` via `go:embed`, backed by GORM `AutoMigrate` on PostgreSQL 18+.

## Quick Start (Docker Compose)

1. Initialize production environment & generated secrets (`chmod 600`):
   ```sh
   ./scripts/deploy.sh init
   ```
2. Set `RUNWAY_GITHUB_APP_ID` and `RUNWAY_GITHUB_PRIVATE_KEY_PEM` in `.env`, then deploy:
   ```sh
   ./scripts/deploy.sh deploy --mode pull --prod
   ```
   _(Use `--mode build` to build the `scratch` container locally.)_

## GitHub App Setup

- **Webhook URL**: `https://<your-domain>/webhooks/github`
- **Webhook Secret**: matches `RUNWAY_GITHUB_WEBHOOK_SECRET`
- **Events**: `workflow_job`, `installation`
- **Permissions**: `Actions` (Read & write), `Administration` / `Organization self-hosted runners` (Read & write), `Metadata` (Read-only)

## Running Workflows & Docker Provisioner

Target any enabled pool in your workflow using its slug or label subset:

```yaml
jobs:
  build:
    runs-on: runway-ubuntu-4vcpu # or [self-hosted, runway-ubuntu-4vcpu]
    steps:
      - uses: actions/checkout@v4
```

To provision real Docker runner containers (`RUNWAY_ORCHESTRATOR_PROVIDER=docker`), include [docker-compose.docker.yml](docker-compose.docker.yml), which routes API calls through a hardened `docker-socket-proxy` (automatically included by `./scripts/deploy.sh` when `RUNWAY_ORCHESTRATOR_PROVIDER=docker`):

```sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml -f docker-compose.docker.yml up -d --wait
```

## Distributed Agents (`runway-agent`)

Register an agent token in the Runway console (**Agents -> Register Agent**) and deploy `runway-agent` on any Linux, macOS, or Windows host (with automatic background updates enabled by default):

```sh
# Linux & macOS (systemd / launchd)
curl -fsSL https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.sh | \
  sudo sh -s -- --server https://<your-domain> --token <rwa_...>
```

```powershell
# Windows PowerShell (Scheduled Task)
$env:RUNWAY_AGENT_SERVER="https://<your-domain>"; $env:RUNWAY_AGENT_TOKEN="<rwa_...>"
irm https://raw.githubusercontent.com/cubetiqlabs/runway/main/scripts/install-agent.ps1 | iex
```

Run `runway-agent update` to upgrade manually, or `--no-auto-update` to disable background updates.

## Operations & Releases

```sh
# Auto-pull/deploy and nightly backups via cron
./scripts/deploy.sh install-cron --mode pull --prod

# PostgreSQL 18 backup & restore
./scripts/deploy.sh backup
./scripts/deploy.sh restore backups/runway_20261127T040000Z.dump

# Commit, tag, push, and publish GitHub Release
./scripts/release.sh v0.1.0 --force
```

## Local Development

```sh
docker compose up -d postgres
RUNWAY_DATABASE_URL="postgres://runway:runway_dev_password@localhost:5432/runway?sslmode=disable" make dev-api
make dev-web   # http://localhost:3000 proxied to :8080
make build     # compile embedded SPA + bin/server
```

### Contributors

- Sambo Chea <sombochea@cubis.tech>
