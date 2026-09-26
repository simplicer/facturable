# ADR-005: MultiOT decision (audit finding #8)

Decision: v1.0 ships ONE instance per taxpayer NIF (TipoUsoPosibleMultiOT=N,
IndicadorMultiplesOT=N), matching the code (chains, submissions and the
license NIF list are per-instance). The gestoría plan is served by one
deployment per client NIF orchestrated by the advisor, NOT one multi-OT
instance — this keeps chains, certificates and licenses isolated and simple.
Revisit only if a gestoría explicitly demands in-process multi-NIF.
