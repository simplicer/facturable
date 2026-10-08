# SPECS — business specification (living doc)

## Mission
Compliance Veri*Factu for micro-SMEs without migrating their invoicing flow; data and certificate stay on their machine.

## Hard regulatory facts (verified, see docs/NORMATIVA.md)
- Mandatory: 2028-10-01 for all taxpayers (postponed from 2026-01-01/2026-07-01 by RDL 15/2025 and RD 254/2025).
- Record chain: SHA-256 uppercase hex 64, per (SIF, taxpayer), altas+anulaciones same chain, verify n-1/n-2 before n.
- Hash inputs (alta): IDEmisorFactura + NumSerieFactura + FechaExpedicionFactura(DD-MM-AAAA) + TipoFactura + CuotaTotal + ImporteTotal + FechaHoraHusoGenRegistro(ISO8601+TZ) + previous Huella.
- Batches: <=1000 records, one taxpayer per batch, wait TiempoEsperaEnvio seconds (initial 60) or until 1000 queued.
- AEAT returns CSV (16 chars) per accepted batch: MUST be persisted at once; not retrievable later.
- QR on every invoice: URL https://www2.agenciatributaria.gob.es/wlpl/TIKE-CONT/ValidarQR?nif=&numserie=&fecha=DD-MM-AAAA&importe= ; VERIFACTU legend when remitting.
- Remission mode (our choice): records not XAdES-signed; SOAP with qualified cert (person www1 / seal www10; prewww1/prewww10 for testing).
- Producer obligations: declaración responsable per version, visible in system; sanctions 150k EUR/yr/type, 1k per unit sold uncertified.

## Use cases (canonical names used in code)
- SellInvoice(draft) -> RegistroAlta remitted + CSV + stamped PDF.
- CancelInvoice(id) -> RegistroAnulacion chained.
- RectifyInvoice(id) -> R1 record with FacturasRectificadas.
- SubmitBatch(batch) -> flow control (60s/1000), retry until response.
- VerifyChain(range) -> integrity check n-1 vs n-2.
- ExportLedger(period) -> gestoría CSV/ZIP (always available, even unlicensed).

## Non-goals
TPV, stock, accounting, cloud data storage, foral (TicketBAI/Navarra), SII, NO-VERIFACTU mode.
