package sello

import (
	"context"
	"fmt"

	"facturable/internal/sello/persistence"
	"facturable/internal/verifactu/application"
	"facturable/internal/verifactu/domain"
	"facturable/internal/verifactu/domain/xmlalta"
)

// SubmitPending submits pending records in BATCHES (up to 1000 records per
// envelope, one taxpayer per batch: AEAT spec §6.4.4.1), honoring the flow
// control between submissions and recording the per-record response.
func (s *Service) SubmitPending(ctx context.Context) (int, error) {
	if !s.Flujo.PuedeEnviar(1000) {
		return 0, nil
	}
	pendientes, err := s.Store.Pending(s.EmisorNIF, 1000)
	if err != nil {
		return 0, err
	}
	if len(pendientes) == 0 {
		return 0, nil
	}

	blocks := make([]string, 0, len(pendientes))
	for _, p := range pendientes {
		bloques := xmlalta.SplitRecordBlocks(p.XML)
		if len(bloques) == 0 {
			return 0, fmt.Errorf("registro %s ilegible: sin bloque RegistroFactura", p.Serie)
		}
		blocks = append(blocks, bloques...)
	}
	sobre, err := xmlalta.BatchFromBlocks(s.EmisorNombre, s.EmisorNIF, blocks)
	if err != nil {
		return 0, err
	}
	lote, err := s.Submitter.Submit(ctx, sobre)
	if err != nil {
		return 0, fmt.Errorf("envío de lote: %w", err)
	}
	s.Flujo.RegistrarEnvio(lote)
	enviados := 0
	for _, p := range pendientes {
		if err := s.recordResponse(p, lote); err != nil {
			return enviados, err
		}
		enviados++
	}
	return enviados, nil
}

// recordResponse persists the AEAT verdict of one pending record, with the
// human explanation from the error dictionary when it is not clean.
func (s *Service) recordResponse(p persistence.PendingRecord, lote application.RespuestaLote) error {
	linea := lineFor(p, lote)
	if linea == nil {
		return s.Store.RecordSubmission(persistence.StoredSubmission{
			RegistroID: p.ID, Estado: "rechazado",
			Detalle: "la AEAT no devolvió el veredicto de este registro"})
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
