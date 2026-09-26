# AGENTS.md — facturable

Rules for all agents working this repo (protocol: ralph-loop-ddd; versioning: simplicer-semver; APIs: restful-api-guidelines).

## Stack (ADR-001)
- Go for: agent (`cmd/sello`), API (`cmd/api`), CLI (`cmd/selloctl`). Stdlib first (ADR-003).
- SvelteKit for web (`web/`). No UI kits.
- Docker/compose only via `make` (infra/). Config in `etc/`. Logs JSON (Loki) to `var/log/`. Executables in `bin/`.

## Architecture (screaming)
- Vertical slices under `internal/<slice>/` with `domain/` and `application/` inside (Go: package layout, not folders named Domain).
- Dependency direction: `infrastructure → application → domain`. Domain pure: no IO, no framework, no env.
- `internal/verifactu` = 100% pure domain+application (no DB, no HTTP, no clock; ports injected). Everything else adapts it.
- Use cases are named imperatively (SellInvoice, CancelInvoice, SubmitBatch). No `Service`/`Manager`/`Util` names.
- Records (registro de facturación) are IMMUTABLE: append-only storage; corrections = new records (anulación / rectificativa R1 / sustitutivo).

## Conventions
- Go standard project layout (cmd/, internal/). Tests colocated `_test.go`. Table-driven tests.
- Conventional Commits (English) — one commit per task. Version derived per simplicer-semver; `VERSION` is source of truth, keep in sync.
- Errors to humans in Spanish; code identifiers/commits in English.
- Numeric amounts: exact 2 decimals as strings in domain (avoid float). Dates: DD-MM-AAAA in AEAT fields; time.Time with TZ internally.
