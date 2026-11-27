# AGENTS.md

Guidelines for coding agents working in this repository.

## Stack & Layout

- **Backend**: Go `1.27.4`, GoFiber `v3` (`github.com/gofiber/fiber/v3`), GORM (`gorm.io/gorm`), Viper (`RUNWAY_*` env prefix), Uber Zap, PostgreSQL `18+`.
- **Entrypoint**: `cmd/server/main.go` (supports `--config` and `--healthcheck`).
- **Frontend**: TanStack Start SPA (`web/`, `spa: { enabled: true }`), React 19, Tailwind CSS v4, shadcn `base-lyra` (`@base-ui/react`), Hugeicons. Built assets (`web/dist/client/*`) embed into `internal/webui/dist/`.
- **Deployment & Ops**: Multi-stage `Dockerfile` (`scratch` runtime), `docker-compose.yml` + `docker-compose.prod.yml`, `scripts/deploy.sh`, `scripts/release.sh`.

## Architecture Rules

- Keep layers separated:
  - `internal/domain`: pure structs, sentinel errors, interfaces (no GORM or Fiber imports).
  - `internal/repository`: GORM models (`models.go`, `Models()`), `pq.StringArray` for `text[]`, `mapGormError` for PG/GORM errors. Always add `gorm:"column:..."` on uppercase acronyms (`vcpu`, `os`, `csrf_token`, `head_sha`, `is_fork_pr`) and `gorm:"->;-:migration"` on read-only join-projected fields.
  - `internal/database`: GORM `AutoMigrate` under a PostgreSQL advisory lock (no manual SQL migration files).
  - `internal/moderation`: deterministic priority-ordered policy engine (no DB/network side effects).
  - `internal/orchestrator`: bounded worker pool, reconciler, and runner provisioners (`docker`, `simulated`).
  - `internal/service`: business logic, JIT config dispatch, audit logging.
  - `internal/http`: Fiber v3 routes, auth/CSRF/rate-limit middlewares, embedded SPA fallback (`_shell.html` -> `index.html`).
- Do not duplicate error mapping, pagination clamping, or audit helpers across packages.

## Security & Platform Invariants

- **GoFiber v3**: Use `fiber.Ctx` (value, not `*fiber.Ctx`), `c.Bind().Body(&req)`, and `fiber.ListenConfig`.
- **Secrets**: Never log private keys, webhook secrets, session/bearer tokens, or `encoded_jit_config`.
- **Auth & CSRF**: Cookie sessions require `X-CSRF-Token` on `POST`/`PUT`/`PATCH`/`DELETE`. Never expose SQL errors or stack traces in HTTP responses.
- **Webhooks**: Verify `X-Hub-Signature-256` in constant time against raw request bytes and deduplicate by `X-GitHub-Delivery`.
- **Frontend**: Use Base UI `render` prop on triggers (not `asChild`), keep `<meta name="color-scheme" content="light dark">`, and preserve accessible labels/table regions.
- **PostgreSQL 18+**: Always mount Docker volume at `/var/lib/postgresql` (never `/var/lib/postgresql/data`).

## Verification

Run checks relevant to modified files before finishing:

```sh
go vet ./cmd/... ./internal/...
go test -race -v ./cmd/... ./internal/...
cd web && bun run generate-routes && bun run typecheck && bun run build
make build
bash -n scripts/deploy.sh scripts/release.sh
docker compose -f docker-compose.yml -f docker-compose.prod.yml config --quiet
```
