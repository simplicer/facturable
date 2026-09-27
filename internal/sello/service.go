// Package sello wires ingest, the SealInvoice use case, the SQLite ledger
// and the AEAT Submitter: it is the local agent engine (the SIF).
package sello

import (
	"fmt"
	"io"
	"time"

	"facturable/internal/sello/ingest"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
)

// Service orquesta el ciclo completo del agente.
type Service struct {
	Store     *persistence.Store
	Submitter application.Submitter
	Flujo     *application.ControlFlujo

	EmisorNIF, EmisorNombre string
	Sistema                 application.SystemInfo
	// Now is injected so tests control time; nil means UTC wall clock.
	Now func() time.Time
}

// clock returns the injected clock or UTC wall time.
func (s *Service) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// IngestResult resume una ingesta de CSV.
type IngestResult struct {
	Sellados int               `json:"sellados"`
	Errores  []ingest.RowError `json:"errores,omitempty"`
}

// IngestCSV valida, sella y guarda un CSV de facturas.
func (s *Service) IngestCSV(r io.Reader) (*IngestResult, error) {
	borradores, errores := ingest.ReadCSV(r)
	res := &IngestResult{Errores: errores}

	for _, b := range borradores {
		if dup, err := s.Store.HasSeries(s.EmisorNIF, b.Series); err == nil && dup {
			res.Errores = append(res.Errores, ingest.RowError{
				Fila: b.Fila, Campo: "numero_serie",
				Detalle: fmt.Sprintf("la factura %s ya consta en el libro (reenvío ignorado)", b.Series)})
			continue
		}
		estado, err := s.chainState()
		if err != nil {
			return res, err
		}
		sellado, err := application.SealInvoice(s.EmisorNIF, s.EmisorNombre,
			application.InvoiceDraft{
				Series: b.Series, Date: b.Date, Tipo: b.Tipo,
				Description: b.Description, ClientNIF: b.ClientNIF,
				ClientName: b.ClientName,
				Desgloses:  toBreakdowns(b.Breakdowns),
			}, s.Sistema, estado, s.clock())
		if err != nil {
			res.Errores = append(res.Errores, ingest.RowError{
				Fila: b.Fila, Campo: "factura", Detalle: err.Error()})
			continue
		}
		if _, err := s.Store.Append(persistence.StoredRecord{
			NIF: sellado.NIF, Serie: sellado.Serie, Date: sellado.Date,
			Tipo: sellado.Tipo, Generado: sellado.Generado,
			Cuota: sellado.Cuota, Importe: sellado.Importe,
			Huella: sellado.Huella, XML: sellado.XML,
			EnlaceSerie: sellado.EnlaceSerie, EnlaceFecha: sellado.EnlaceFecha,
			EnlaceHuella: sellado.EnlaceHuella,
		}); err != nil {
			return res, fmt.Errorf("no se pudo storeRow %s: %w", sellado.Serie, err)
		}
		res.Sellados++
	}
	return res, nil
}

// IngestDrafts stamps and stores validated JSON drafts (relay/API path).
func (s *Service) IngestDrafts(drafts []ingest.Draft) (*IngestResult, error) {
	res := &IngestResult{}
	for i, d := range drafts {
		// idempotencia relay/API: serie ya sellada → aviso, no doble sello
		if dup, err := s.Store.HasSeries(s.EmisorNIF, d.Series); err == nil && dup {
			res.Errores = append(res.Errores, ingest.RowError{
				Fila: i + 1, Campo: "numero_serie",
				Detalle: fmt.Sprintf("la factura %s ya consta en el libro (webhook repetido ignorado)", d.Series)})
			continue
		}
		estado, err := s.chainState()
		if err != nil {
			return res, err
		}
		desgloses := make([]application.BreakdownDraft, 0, len(d.Breakdowns))
		for _, bd := range d.Breakdowns {
			desgloses = append(desgloses, application.BreakdownDraft{
				TaxRate: bd.TaxRate, Base: bd.Base, Cuota: bd.Fee})
		}
		sellado, err := application.SealInvoice(s.EmisorNIF, s.EmisorNombre,
			application.InvoiceDraft{
				Series: d.Series, Date: d.Date, Tipo: d.Tipo,
				Description: d.Description, ClientNIF: d.ClientNIF,
				ClientName: d.ClientName, Desgloses: desgloses,
			}, s.Sistema, estado, s.clock())
		if err != nil {
			res.Errores = append(res.Errores, ingest.RowError{
				Fila: i + 1, Campo: "factura", Detalle: err.Error()})
			continue
		}
		if _, err := s.Store.Append(persistence.StoredRecord{
			NIF: sellado.NIF, Serie: sellado.Serie, Date: sellado.Date, Tipo: sellado.Tipo,
			Generado: sellado.Generado, Cuota: sellado.Cuota, Importe: sellado.Importe,
			Huella: sellado.Huella, XML: sellado.XML,
			EnlaceSerie: sellado.EnlaceSerie, EnlaceFecha: sellado.EnlaceFecha,
			EnlaceHuella: sellado.EnlaceHuella,
		}); err != nil {
			return res, fmt.Errorf("could not store %s: %w", sellado.Serie, err)
		}
		res.Sellados++
	}
	return res, nil
}

func (s *Service) chainState() (application.ChainState, error) {
	series, fecha, huella, enlS, enlF, enlH, ok, err := s.Store.Last(s.EmisorNIF)
	if err != nil {
		return application.ChainState{}, err
	}
	if !ok {
		return application.ChainState{}, nil
	}
	estado := application.ChainState{
		Last: &application.ChainLink{Serie: series, Date: fecha, Huella: huella},
	}
	if enlS != "" {
		estado.DeclaradoPorUltimo = &application.ChainLink{Serie: enlS, Date: enlF, Huella: enlH}
	}
	aS, aF, aH, aOk, err := s.Store.Previous(s.EmisorNIF)
	if err != nil {
		return estado, err
	}
	if aOk {
		estado.Previous = &application.ChainLink{Serie: aS, Date: aF, Huella: aH}
	}
	return estado, nil
}

func toBreakdowns(ds []ingest.BreakdownDraft) []application.BreakdownDraft {
	out := make([]application.BreakdownDraft, 0, len(ds))
	for _, d := range ds {
		out = append(out, application.BreakdownDraft{
			TaxRate: d.TaxRate, Base: d.Base, Cuota: d.Fee})
	}
	return out
}
