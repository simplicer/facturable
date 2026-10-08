# NORMATIVA — Regulatory basis

Every rule this software implements traces back to a published Spanish
regulation. This document lists each source with its official BOE link
(verified), what it mandates, and where in this codebase it lives.

Todo lo que este software implementa tiene su origen en una norma
española publicada. Este documento enumera cada fuente con su enlace
oficial al BOE (verificado), lo que exige, y dónde vive en el código.

## 1. Sources / Fuentes

| Norm | BOE ID | Link | What it mandates |
| :--- | :--- | :--- | :--- |
| **Ley 11/2021**, de 9 de julio, de medidas de prevención y lucha contra el fraude fiscal | [BOE-A-2021-11473](https://www.boe.es/eli/es/l/2021/07/09/11) | [texto consolidado](https://www.boe.es/eli/es/l/2021/07/09/11/con) | Adds art. 29.2.j) and 201 bis to the Ley General Tributaria: the legal root of the billing-record requirements. |
| **RD 1007/2023**, de 5 de diciembre (RRSIF — Reglamento de sistemas informáticos de facturación) | [BOE-A-2023-24840](https://www.boe.es/eli/es/rd/2023/12/05/1007) | [texto consolidado](https://www.boe.es/eli/es/rd/2023/12/05/1007/con) | Integrity, traceability, unalterability and legibility of invoicing records; the chained SHA-256 record ("huella"); the VERI*FACTU voluntary regime; producer obligations (arts. 13-15). |
| **Orden HAC/1177/2024**, de 17 de octubre | [BOE-A-2024-22138](https://www.boe.es/eli/es/o/2024/10/17/hac1177) | [texto consolidado](https://www.boe.es/eli/es/o/2024/10/17/hac1177/con) | The technical annexes: record fields and XML formats, huella computation, QR code ("URL de cotejo"), SOAP WebService (SF), error codes, flow control. |
| **RD 1619/2012**, de 30 de noviembre, obligaciones de facturación | [BOE-A-2012-14696](https://www.boe.es/eli/es/rd/2012/11/30/1619) | [texto consolidado](https://www.boe.es/eli/es/rd/2012/11/30/1619/con) | General invoicing obligations (who must invoice, invoice content) — the surface this tool stamps. |

The AEAT publishes the companion technical specifications (huella
specification, SF WebService, validaciones, "parámetros genéricos") on its
Sede, section Veri*Factu: <https://sede.agenciatributaria.gob.es>. The
pre-production endpoints exercised by `make acceptance` are documented in
[docs/runbooks/aeat-preprod-qa.md](runbooks/aeat-preprod-qa.md).

## 2. Timeline / Calendario

- Entry into force of RD 1007/2023: 2023-12-07.
- QR validation service live since 2024-10-28 (AEAT rejects records dated
  before that date with error 1201).
- **Mandatory: 2028-10-01 for all taxpayers** — postponed from
  2026-01-01 (corporate tax filers) and 2026-07-01 (rest) by RDL 15/2025
  and RD 254/2025 (see the consolidated RD 1007/2023 for the current text).
- Until then, joining the VERI*FACTU regime (automatic submission) is
  voluntary and, once joined, systems are presumed compliant by design.

## 3. Rule → code map

| Requirement | Source | Where it lives |
| :--- | :--- | :--- |
| Chained SHA-256 fingerprint, uppercase hex 64 | RRSIF art. 5; Orden HAC/1177/2024 (huella spec) | `internal/verifactu/domain/huella.go` |
| Verify previous links (n-1/n-2) before sealing | Orden HAC/1177/2024 (encadenamiento) | `internal/verifactu/domain/cadena.go` |
| NIF validation (DNI/NIE/CIF control digit/letter) | RD 1553/2004 art. 4; Orden HAC/1177/2024 | `internal/verifactu/domain/nif.go` |
| Exact hash input field order and formats | Orden HAC/1177/2024 (huella spec pp. 5-7) | `internal/verifactu/domain/huella_test.go` (pinned to the official AEAT worked example) |
| QR: 30 mm minimum, top of invoice, «URL de cotejo» without huella | Orden HAC/1177/2024 (QR annex); RRSIF art. 7 | `internal/verifactu/domain/qr`, `internal/sello/pdf` |
| VERI*FACTU legend on invoices | Orden HAC/1177/2024 | `internal/sello/pdf` |
| XML alta/anulación records, batch envelope (≤1000, one taxpayer) | Orden HAC/1177/2024 (annexes, SF §6.4.4.1) | `internal/verifactu/domain/xmlalta` |
| SOAP submission with qualified certificate, flow control (60 s / 1000) | Orden HAC/1177/2024 (SF WebService) | `internal/verifactu/infrastructure/soap`, `internal/verifactu/application` (ControlFlujo) |
| AEAT error dictionary with Spanish actions | Orden HAC/1177/2024 (error catalogue) | `internal/verifactu/domain/errores.go` |
| Immutability of records (no UPDATE/DELETE) | RRSIF arts. 2-3 (integridad, inalterabilidad) | `internal/sello/persistence/store.go` (append-only triggers) |
| CSV de remisión persisted on acceptance | Orden HAC/1177/2024 (respuesta SF) | `internal/sello/submit.go`, `remisiones` table |
| Producer declaration of conformity | RRSIF arts. 13-15 | `GET /v1/declaration`, `internal/sello/declaration` |
| Numeric timezone offset (+01:00/+02:00, never Z) in timestamps | Orden HAC/1177/2024 (formato fecha-hora) | `internal/verifactu/domain` (FechaHoraHuso) |

## 4. Producer obligations / Obligaciones del productor

If you sell or deploy software that supports invoicing (this agent
included), RRSIF arts. 13-15 require you to:

1. File a **declaración responsable** with the AEAT before first
   commercial use (template served by `GET /v1/declaration`).
2. Register each product version and keep it visible in the system
   (`/version`, `GET /v1/declaration`).
3. Deliver the standard **anexo informativo** content to users.

Fines can reach €150,000/year per infringement type and €1,000 per
non-compliant unit sold. Running this agent for your own companies still
makes *you* the software producer: file the declaration.

## 5. Disclaimer

This documentation and this software are provided under GPL-3.0 without
legal warranty; they implement the published rules as faithfully as
possible, but only the BOE consolidated texts and the AEAT specifications
are authoritative. Nothing here is legal advice. Foral territories
(TicketBAI, Navarra) and the SII regime are out of scope by design.
