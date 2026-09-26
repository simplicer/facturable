// Package sello wires ingest, the SellarAlta use case, the SQLite ledger
// and the AEAT Remisor: it is the local agent engine (the SIF).
package sello

import (
	"context"
	"fmt"
	"io"
	"time"

	"facturable/internal/sello/ingest"
	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/domain"
)

// Service orquesta el ciclo completo del agente.
type Service struct {
	Store   *persistence.Store
	Remisor application.Remisor
	Flujo   *application.ControlFlujo

	EmisorNIF, EmisorNombre string
	Sistema                 application.SistemaLocal
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
		estado, err := s.chainState()
		if err != nil {
			return res, err
		}
		sellado, err := application.SellarAlta(s.EmisorNIF, s.EmisorNombre,
			application.InvoiceDraft{
				Series: b.Series, Date: b.Date, Tipo: b.Tipo,
				Description: b.Description, ClientNIF: b.ClientNIF,
				ClientName: b.ClientName,
				Desgloses:  toBreakdowns(b.Breakdowns),
			}, s.Sistema, estado, time.Now())
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
		estado, err := s.chainState()
		if err != nil {
			return res, err
		}
		desgloses := make([]application.BreakdownDraft, 0, len(d.Breakdowns))
		for _, bd := range d.Breakdowns {
			desgloses = append(desgloses, application.BreakdownDraft{
				TaxRate: bd.TaxRate, Base: bd.Base, Cuota: bd.Fee})
		}
		sellado, err := application.SellarAlta(s.EmisorNIF, s.EmisorNombre,
			application.InvoiceDraft{
				Series: d.Series, Date: d.Date, Tipo: d.Tipo,
				Description: d.Description, ClientNIF: d.ClientNIF,
				ClientName: d.ClientName, Desgloses: desgloses,
			}, s.Sistema, estado, time.Now())
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

// Cancel genera y guarda el chainRow de anulación de una factura.
func (s *Service) Cancel(series, fecha string) (int64, error) {
	estado, err := s.chainState()
	if err != nil {
		return 0, err
	}
	res, err := application.SellarAnulacion(s.EmisorNIF, series, fecha, s.Sistema, estado, time.Now())
	if err != nil {
		return 0, err
	}
	id, err := s.Store.Append(persistence.StoredRecord{
		NIF: res.NIF, Serie: res.Serie, Date: res.Date, Tipo: res.Tipo,
		Generado: res.Generado, Cuota: "0.00", Importe: "0.00",
		Huella: res.Huella, XML: res.XML,
		EnlaceSerie: res.EnlaceSerie, EnlaceFecha: res.EnlaceFecha, EnlaceHuella: res.EnlaceHuella,
	})
	return id, err
}

// SubmitPending remite los registros pendientes respetando el control
// de flujo (espera 60 s o acumulado de lote) y registra cada respuesta.
func (s *Service) SubmitPending(ctx context.Context) (int, error) {
	pendientes, err := s.Store.Pending(s.EmisorNIF, 1000)
	if err != nil {
		return 0, err
	}
	if len(pendientes) == 0 {
		return 0, nil
	}
	// El control de flujo de la AEAT obliga a esperar TiempoEsperaEnvio
	// (60 s inicial) entre envíos: el agente envía un sobre por ciclo y su
	// planificador (60 s) vuelve a llamar. El acumulado a lote llegará con
	// el envío agrupado (véase PLAN-DESARROLLO T-2.5 mejora).
	if !s.Flujo.PuedeEnviar(len(pendientes)) {
		return 0, nil
	}
	p := pendientes[0]
	lote, err := s.Remisor.Remitir(ctx, p.XML)
	if err != nil {
		return 0, fmt.Errorf("envío de %s: %w", p.Serie, err)
	}
	s.Flujo.RegistrarEnvio(lote)
	if err := s.recordResponse(p, lote); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Service) recordResponse(p persistence.PendingRecord, lote application.RespuestaLote) error {
	linea := lineFor(p, lote)
	if linea == nil {
		return s.Store.RecordSubmission(persistence.StoredSubmission{
			RegistroID: p.ID, Estado: "rechazado",
			Detalle: "la AEAT no devolvió el veredicto de este chainRow"})
	}
	estado := map[string]string{
		"Correcto": "correcto", "AceptadoConErrores": "aceptado_errores", "Incorrecto": "rechazado",
	}[linea.Estado]
	if estado == "" {
		estado = "rechazado"
	}
	detalle := linea.DescripcionRaw
	if linea.CodigoError != "" && linea.Estado != "Correcto" {
		info := domain.ClasificarError(linea.CodigoError)
		detalle = fmt.Sprintf("%s — %s", info.Description, info.Accion)
	}
	return s.Store.RecordSubmission(persistence.StoredSubmission{
		RegistroID: p.ID, Estado: estado, CSV: lote.CSV,
		Codigo: linea.CodigoError, Detalle: detalle})
}

func lineFor(p persistence.PendingRecord, lote application.RespuestaLote) *application.RespuestaLinea {
	for i := range lote.Lineas {
		l := &lote.Lineas[i]
		if l.NumSerie == p.Serie && l.Date == p.Date {
			return l
		}
	}
	return nil
}

func (s *Service) chainState() (application.EstadoCadena, error) {
	series, fecha, huella, enlS, enlF, enlH, ok, err := s.Store.Last(s.EmisorNIF)
	if err != nil {
		return application.EstadoCadena{}, err
	}
	if !ok {
		return application.EstadoCadena{}, nil
	}
	estado := application.EstadoCadena{
		Last: &application.EnlaceTriple{Serie: series, Date: fecha, Huella: huella},
	}
	if enlS != "" {
		estado.DeclaradoPorUltimo = &application.EnlaceTriple{Serie: enlS, Date: enlF, Huella: enlH}
	}
	aS, aF, aH, aOk, err := s.Store.Previous(s.EmisorNIF)
	if err != nil {
		return estado, err
	}
	if aOk {
		estado.Previous = &application.EnlaceTriple{Serie: aS, Date: aF, Huella: aH}
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

// realNow es el reloj de producción del agente.
func realNow() time.Time { return time.Now() }
