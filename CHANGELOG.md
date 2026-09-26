# Changelog

## [0.1.0-unstable] - 2026-09-26 (unreleased line)
### Added
- Veri*Factu compliance core: SHA-256 fingerprint passing the official AEAT vector, per-taxpayer chain with n-1/n-2 verification, SOAP alta/anulación builder, QR cotejo URL, 52-code error dictionary, response parser and 60s/1000 flow control.
- Local agent: CSV ingest with per-row Spanish errors, immutable SQLite ledger, AEAT submission (batched ≤1000), stamped PDF with 30mm QR top-center, ledger export by period, cancellation, integrity self-check, encrypted backups, certificate onboarding, responsible declaration endpoint, license gate.
- API: Ed25519 idempotent licenses with never-hostage validity, Stripe webhook issuance, E2E relay mailbox (X25519+HKDF), key registry.
- Web: landing, pricing, FAQ, downloads, user guide.
- Security: PIN auth, certificate rate limit and at-rest encryption, relay rate limit, fail-closed release signing, nonroot containers.
### Fixed
- TLS trust pool (client cert no longer replaced system anchors).
- Cancellation ObligadoEmision now the invoice issuer.
- Negative amount parsing (-5.25), Madrid offsets instead of Z, anulacion response tags.
- Leaked license keypair removed from index and rotated.
