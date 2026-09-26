# facturable

Local Veri*Factu stamp: turns any invoicing flow (Excel, templates, legacy software) into a compliant billing system without migration and without cloud-hosted customer data.

- `cmd/sello` — local desktop agent (the SIF): CSV ingest, SHA-256 chained records, AEAT submission, stamped PDF with QR, ledger and exports.
- `cmd/api` — public API: Ed25519-signed licenses (idempotent PUT, never-hostage validity), ingestion relay (planned), payment webhooks (planned).
- `cmd/selloctl` — internal CLI (`claves` = license keypair generation).
- `internal/verifactu` — pure compliance core (fingerprint with AEAT vector, chain verification, SOAP XML builder, QR URL, error dictionary, response parser and flow control, SOAP client with P12).
- `internal/sello` — agent slices: ingest, append-only SQLite ledger (immutable records), integrity self-check, AES-GCM backups, own QR encoder (golden-validated against reference), own PDF writer.
- `internal/licencias` — license signing, offline grace and never-hostage gating.
- `docs/informes/` — verified research (regulatory, business, product) backing every decision (Spanish: business analysis for the team).
- `docs/PLAN-DESARROLLO.md` — atomic launch plan (task source of truth; Spanish: business planning document).

## API surface (v1)

| Method | Path | Purpose |
|---|---|---|
| PUT | `/v1/records/{uuid}` | create stamped records from a CSV body (idempotent, client UUID) |
| GET | `/v1/records` | ledger (JSON; `?format=csv&from=&to=` for the accountant) |
| GET | `/v1/records/{uuid}/pdf` | stamped invoice PDF with QR |
| POST | `/v1/submissions` | submit pending records (one per AEAT flow-control window) |
| PUT | `/v1/certificate` | install P12 certificate (idempotent singleton) |
| GET | `/v1/connectivity` | certificate + AEAT preprod connectivity state |
| PUT | `/v1/licenses/{uuid}` | idempotent license issue (client UUID) |
| GET | `/v1/licenses/{uuid}/validity` | never-hostage validity check |

OpenAPI contract: `docs/openapi.yaml`.

## Quick start
    make up        # api + web via docker compose
    make test      # go test ./... + web checks
    make run-sello # local agent (dev, simulated AEAT mode)

Versioning: SemVer via VERSION, derived from Conventional Commits (docs/adrs/ADR-002).
