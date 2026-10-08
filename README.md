# Facturable

**Veri*Factu compliance agent** — stamp any invoice with the Spanish Tax Agency (AEAT) QR code without changing your invoicing software.

**Agente de cumplimiento Veri\*Factu** — sella cualquier factura con el QR de la AEAT sin cambiar tu programa de facturación.

---

## What it does / Qué hace

You keep using **your** invoicing software (Excel, Word, TPV, ERP, whatever). Facturable stamps the invoice PDF with the AEAT QR code, submits the billing record to the Spanish Tax Agency, and returns your original PDF now legally compliant.

Sigues usando **tu** programa de facturación (Excel, Word, TPV, ERP, lo que sea). Facturable sella el PDF de tu factura con el QR de la AEAT, remite el registro de facturación a Hacienda, y te devuelve tu PDF original ahora legalmente conforme.

## Status / Estado

Veri*Factu mandatory compliance has been **postponed to October 2028**. This implementation follows RD 1007/2023 (RRSIF) and Orden HAC/1177/2024 as published. The code is complete and tested but the deadline has moved.

La obligatoriedad de Veri*Factu se ha **aplazado hasta octubre de 2028**. Esta implementación sigue el RD 1007/2023 (RRSIF) y la Orden HAC/1177/2024 según lo publicado. El código está completo y testeado pero la fecha límite se ha movido.

## Key features / Características

- **Stamp any PDF**: drag your invoice, confirm totals, get it back with QR / **Sella cualquier PDF**: arrastra tu factura, confirma totales, recíbela con QR
- **SHA-256 chained records**: per-taxpayer chain with n-1/n-2 verification / **Registros encadenados SHA-256**: cadena por obligado tributario con verificación n-1/n-2
- **SOAP submission to AEAT**: with 60s/1000 flow control / **Remisión SOAP a la AEAT**: con control de flujo 60s/1000
- **30mm QR top-center**: per AEAT FAQ (30-40mm range) / **QR 30mm arriba centrado**: según FAQ AEAT (rango 30-40mm)
- **Immutable ledger**: SQLite append-only, triggers block UPDATE/DELETE / **Libro inmutable**: SQLite append-only, triggers bloquean UPDATE/DELETE
- **Cancellation & rectification (R1)**: chained like any record / **Anulación y rectificativa (R1)**: encadenadas como cualquier registro
- **Encrypted backups**: AES-256-GCM, PBKDF2 / **Backups cifrados**: AES-256-GCM, PBKDF2
- **License system**: Ed25519 signed, 7-day offline grace / **Sistema de licencias**: firmadas Ed25519, gracia offline 7 días
- **Input methods**: PDF drag-drop, CSV import, quick form, watched folder, E2E relay / **Métodos de entrada**: arrastrar PDF, CSV, formulario rápido, carpeta vigilada, relay E2E
- **Auto-updater**: SHA-256 verified / **Auto-actualización**: verificada SHA-256
- **Client memory**: remembers frequent customers / **Memoria de clientes**: recuerda clientes frecuentes

## Quick start / Inicio rápido

```bash
# Build / Compilar
make build

# Run the agent (development mode, simulated AEAT) / Arrancar el agente (modo desarrollo, AEAT simulada)
make run-sello
# → opens http://127.0.0.1:8787

# Run tests / Ejecutar tests
make test
```

## Architecture / Arquitectura

```
facturable/
├── cmd/
│   ├── sello/          # Local agent (the SIF) / Agente local (el SIF)
│   ├── api/            # Public API: licenses, relay, payments / API pública
│   └── selloctl/       # CLI: key generation, signing / CLI: generación de claves
├── internal/
│   ├── verifactu/      # Pure compliance core / Núcleo de cumplimiento puro
│   │   ├── domain/     #   SHA-256 hash, chain, NIF, XML, QR / Hash, cadena, NIF, XML, QR
│   │   ├── application/#   Use cases: SealInvoice, SubmitBatch / Casos de uso
│   │   └── infrastructure/soap/ # AEAT SOAP client / Cliente SOAP AEAT
│   ├── sello/          # Agent: ingest, persistence, PDF, email / Agente
│   ├── licenses/       # Ed25519 license system / Sistema de licencias
│   ├── relay/          # E2E encrypted mailbox (X25519+AES-GCM) / Buzón cifrado
│   └── platform/       # Shared: logging, config / Compartido: logging, config
├── docs/
│   ├── SPECS.md        # Business specification / Especificación de negocio
│   └── adrs/           # Architecture Decision Records
├── infra/              # Docker / Docker
└── etc/                # Configuration / Configuración
```

## API (v1)

| Method | Path | Description |
|---|---|---|
| PUT | `/v1/records/{uuid}` | Create stamped records from CSV (idempotent) |
| GET | `/v1/records` | Ledger (JSON/CSV, paginated) |
| GET | `/v1/records/{uuid}/pdf` | Stamped invoice PDF |
| POST | `/v1/submissions` | Submit pending records to AEAT |
| POST | `/v1/invoices` | Quick invoice form (5 fields with defaults) |
| POST | `/v1/stamps` | Upload PDF, stamp with QR |
| POST | `/v1/extract` | Extract fiscal fields from PDF |
| POST | `/v1/cancellations` | Cancel an invoice |
| PUT | `/v1/certificates` | Install P12 certificate |
| GET | `/v1/connectivity` | Test AEAT connection |
| GET | `/v1/declaration` | Producer declaration (RRSIF art. 13-15) |

Full contract: `docs/openapi.yaml`

## Legal / Legal

This software implements the requirements of:
- **Ley 11/2021** (anti-fraud measures) — [BOE-A-2021-11473](https://www.boe.es/eli/es/l/2021/07/09/11)
- **RD 1007/2023** (RRSIF) — Reglamento de sistemas informáticos de facturación — [BOE-A-2023-24840](https://www.boe.es/eli/es/rd/2023/12/05/1007)
- **Orden HAC/1177/2024** — Technical specifications — [BOE-A-2024-22138](https://www.boe.es/eli/es/o/2024/10/17/hac1177)

Every rule and its mapping to the code is documented in [docs/NORMATIVA.md](docs/NORMATIVA.md).

Producer obligations (RRSIF art. 13-15): you must sign a responsible declaration before using this software commercially. See `GET /v1/declaration` for the template.

Obligaciones del productor (RRSIF art. 13-15): debes firmar una declaración responsable antes de usar este software comercialmente. Ver `GET /v1/declaration` para la plantilla. Cada norma y su correspondencia con el código están documentadas en [docs/NORMATIVA.md](docs/NORMATIVA.md).

## License / Licencia

GPL-3.0 — see [LICENSE](LICENSE)

## Contributing

Contributions welcome. Read `AGENTS.md` for coding standards. Conventional Commits required.

Las contribuciones son bienvenidas. Lee `AGENTS.md` para los estándares de código. Se requieren Conventional Commits.
