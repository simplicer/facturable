# AEAT preprod QA runbook (T-7.2)

Purpose: validate the whole agent pipeline against the real AEAT test
environment before piloting. Never runs in CI (no certificate).

## Prerequisites
1. Qualified certificate of the company (persona jurídica, FNMT) in P12.
2. The certificate NIF and the census legal name (Mis datos censales) —
   the AEAT rejects mismatches (error 4103).
3. Go toolchain.

## Run
    export SELLO_CERT_P12=/ruta/cert.p12
    export SELLO_CERT_PASS='...'
    export SELLO_CERT_NIF=Bxxxxxxxx        # certificate NIF
    go test -tags=aeat ./internal/verifactu/infrastructure/soap/ -v

## Scenarios covered
- Handshake: TLS+SOAP round trip (empty envelope answer).
- Alta: a real QA record (SellarAlta built) remitted to prewww1; expect
  EstadoGlobal and per-record verdicts; CSV persisted by the caller.
- Known-rejection scenarios to add incrementally (doc validaciones):
  bad recipient NIF (1207), date before 2024-10-28 (1201), >12 breakdowns
  (1108), duplicate record (3000), chain mismatch (1111/2001).

## Live agent against preprod
    SELLO_NIF=Bxxxxxxxx SELLO_NOMBRE='NOMBRE CENSAL' \
    SELLO_CERT_P12=... SELLO_CERT_PASS=... SELLO_AEAT=pruebas \
    go run ./cmd/sello
    # then: POST /v1/records with the CSV template and watch the ledger
