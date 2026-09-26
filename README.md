# facturable

Sello Veri*Factu local: convierte cualquier forma de facturar (Excel, plantillas, software viejo) en un SIF conforme, sin migrar y sin nube para los datos.

- `cmd/sello` — agente local (producto de escritorio, UI web local).
- `cmd/api` — API pública: licencias Ed25519, relay de ingesta E2E, webhooks de pago.
- `web/` — web de venta (SvelteKit, marca facturable.es).
- `internal/verifactu` — núcleo puro de cumplimiento (huella SHA-256, cadena, XML SOAP, QR) con TDD sobre especificaciones AEAT.
- `docs/informes/` — investigación verificada (normativa, negocio, producto) que justifica cada decisión.
- `docs/PLAN-DESARROLLO.md` — plan atómico de lanzamiento (fuente de tareas).

## Quick start
    make up        # api + web via docker compose
    make test      # go test ./... + web checks
    make run-sello # agente local (dev)

Versioning: SemVer via VERSION, derived from Conventional Commits (see docs/adrs/ADR-002).
