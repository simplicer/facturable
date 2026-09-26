# ADR-006: Go-flat layout and spec-mirroring names

Decision (audit 1.3 resolution):
1. Slices stay Go-flat (`internal/<slice>/` without nested domain/application
   dirs): Go's package system makes the two-level nesting high-churn for a
   codebase this size. Use cases live in imperative-named files/functions
   (SealInvoice, SubmitPending, Cancel, VerifyChain, ExportPeriod) and the
   dependency direction is enforced by imports. The full nesting stays in
   `internal/verifactu` (domain/application/infrastructure), where it pays.
2. Domain value objects mirror AEAT wire fields in Spanish (FechaExpedicion,
   Huella, CuotaTotal, NumSerieFactura tags) — 1:1 traceability with the
   official record design beats translation; code identifiers, comments and
   docs remain English (AGENTS.md).
