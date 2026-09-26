# ADR-003: Dependency policy

Stdlib first (encoding/xml SOAP, crypto/ed25519, crypto/aes, net/smtp, net/http).
Allowed curated: modernc.org/sqlite (pure-Go SQLite, no cgo -> cross-compile), stripe-go (official).
Forbidden by default: SOAP libs, ORMs, UI kits, QR libs (own encoder, auditable), PDF libs (own minimal writer).
Rationale: producer liability (150k EUR/yr) demands auditable core; user mandate "no untested libraries".
